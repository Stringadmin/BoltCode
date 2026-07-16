// Package tui implements the interactive review interface using Bubble Tea.
// `boltcode tui` enters this mode: browse proposals, view diffs, approve/reject
// with keyboard shortcuts — no need to type commands repeatedly.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

// view tracks which screen the user is on.
type view int

const (
	viewList view = iota
	viewDiff
)

// Model is the Bubble Tea model for the review TUI.
type Model struct {
	app             *command.App
	proposals       []*model.Proposal
	cursor          int
	current         view
	diff            string
	width           int
	height          int
	scroll          int
	message         string
	quitting        bool
	author          string // git user.name, cached at startup for review attribution
	confirmingMerge bool   // true after first "m" — second "m" confirms the (irreversible) merge
}

// tickMsg is emitted periodically by tickCmd so the TUI can reload pending
// proposals without the user restarting — a teammate's submission appears
// within a few seconds.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// New creates the TUI model, pre-loading pending proposals.
func New(app *command.App) (Model, error) {
	pending, err := app.ListPending()
	if err != nil {
		return Model{}, err
	}
	// Cache the reviewer identity once (WhoAmI shells out to git) so each
	// approve/reject keypress doesn't spawn a git process.
	return Model{app: app, proposals: pending, width: 80, height: 24, author: app.WhoAmI()}, nil
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorOrange))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorText)).
			Background(lipgloss.Color(theme.ColorSelection))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorNormalText))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorMutedText))

	statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(theme.ColorSuccess))
)

func (m Model) Init() tea.Cmd {
	// Start the periodic-refresh loop so new proposals from teammates appear
	// without restarting the TUI.
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		// Reload pending proposals. Skip while a merge confirmation is pending
		// so the cursor doesn't jump mid-confirm. Approve/reject already
		// mutated the store, so reloaded state is consistent.
		if !m.confirmingMerge {
			if pending, err := m.app.ListPending(); err == nil {
				m.proposals = pending
			}
		}
		return m, tickCmd() // schedule the next refresh

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.message != "" && m.current == viewList {
			m.message = ""
		}
		switch m.current {
		case viewList:
			return m.updateList(msg)
		case viewDiff:
			return m.updateDiff(msg)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	// A pending merge confirmation is cancelled by any key other than a second
	// "m" — this prevents an accidental single keypress from merging.
	if m.confirmingMerge && key != "m" {
		m.confirmingMerge = false
		m.message = ""
	}
	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.proposals)-1 {
			m.cursor++
		}
	case "enter", "d":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			diff, err := m.app.ProposalDiff(p)
			if err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.diff = diff
				m.scroll = 0
				m.current = viewDiff
			}
		}
	case "a":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			if _, err := m.app.Approve(p.ID, m.author); err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.message = fmt.Sprintf("✓ approved %s", p.ID)
				m.removeCurrent()
			}
		}
	case "m":
		if len(m.proposals) > 0 {
			if m.confirmingMerge {
				// Second press — confirm the irreversible merge.
				m.confirmingMerge = false
				p := m.proposals[m.cursor]
				if _, err := m.app.ApproveAndMerge(p.ID, m.author); err != nil {
					m.message = "error: " + err.Error()
				} else {
					m.message = fmt.Sprintf("✓ merged %s", p.ID)
					m.removeCurrent()
				}
			} else {
				// First press — ask for confirmation.
				m.confirmingMerge = true
				m.message = fmt.Sprintf("press m again to merge %s (irreversible), or any other key to cancel", m.proposals[m.cursor].ID)
			}
		}
	case "r":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			if err := m.app.Reject(p.ID, m.author, "rejected via TUI"); err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.message = fmt.Sprintf("✗ rejected %s", p.ID)
				m.removeCurrent()
			}
		}
	}
	return m, nil
}

// removeCurrent drops the proposal at the cursor and clamps the cursor so it
// never points past the end of the (now shorter) list. Shared by approve,
// merge, and reject.
func (m *Model) removeCurrent() {
	m.proposals = removeAt(m.proposals, m.cursor)
	if m.cursor >= len(m.proposals) && m.cursor > 0 {
		m.cursor--
	}
}

func (m Model) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "backspace":
		m.current = viewList
	case "up", "k":
		if m.scroll > 0 {
			m.scroll--
		}
	case "down", "j":
		m.scroll++
	case "a", "m", "r":
		// The diff view advertises a/m/r in its help line, so they must work
		// here too: switch back to the list and delegate, reusing the exact
		// approve/merge/reject logic (including the two-press merge confirm).
		m.current = viewList
		return m.updateList(msg)
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func removeAt(s []*model.Proposal, i int) []*model.Proposal {
	return append(s[:i], s[i+1:]...)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	switch m.current {
	case viewDiff:
		return m.viewDiff()
	default:
		return m.viewList()
	}
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  BOLTCODE — Review Dashboard") + "\n\n")

	if len(m.proposals) == 0 {
		b.WriteString(normalStyle.Render("  nothing to review — all caught up") + "\n")
	} else {
		for i, p := range m.proposals {
			line := fmt.Sprintf("  %-10s [%-12s] %s", p.ID, p.Agent, p.Summary)
			if i == m.cursor {
				b.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				b.WriteString(normalStyle.Render(line) + "\n")
			}
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(statusStyle.Render("  "+m.message) + "\n\n")
	}
	help := "  ↑/↓ navigate  enter/d diff  a approve  m merge (press 2×)  r reject  q quit"
	if m.confirmingMerge {
		help = "  press m again to confirm merge — any other key cancels"
	}
	b.WriteString(helpStyle.Render(help))
	return b.String()
}

func (m Model) viewDiff() string {
	var b strings.Builder
	// Defensive: if the list emptied while we were viewing a diff (e.g. a
	// teammate approved the proposal and a refresh removed it), fall back to
	// the list view instead of indexing an empty slice.
	if len(m.proposals) == 0 || m.cursor >= len(m.proposals) {
		m.current = viewList
		return m.viewList()
	}
	p := m.proposals[m.cursor]
	b.WriteString(titleStyle.Render(fmt.Sprintf("  DIFF — %s (%s)", p.ID, p.Summary)) + "\n\n")

	lines := strings.Split(m.diff, "\n")
	maxScroll := len(lines) - (m.height - 5)
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.scroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	visible := m.height - 5
	end := scroll + visible
	if end > len(lines) {
		end = len(lines)
	}

	for _, line := range lines[scroll:end] {
		if strings.HasPrefix(line, "+") {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSuccess)).Render(line) + "\n")
		} else if strings.HasPrefix(line, "-") {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorDanger)).Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + helpStyle.Render("  ↑/↓ scroll  q/esc back  a approve  m merge (press 2×)  r reject"))
	return b.String()
}

// Run starts the interactive TUI program.
func Run(app *command.App) error {
	m, err := New(app)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
