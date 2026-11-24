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
	"github.com/charmbracelet/x/ansi"

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
	listOffset      int // first visible proposal when the list exceeds the screen
	current         view
	diff            string
	diffLines       []string // diff pre-wrapped to the current width
	diffAdds        int
	diffDels        int
	diffFiles       int
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

	// Diff line styles, hoisted out of the per-line render loop.
	diffAddStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSuccess))
	diffDelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorDanger))
	diffHeadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMutedText))
	diffHunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorAccent))
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
				if m.cursor >= len(m.proposals) {
					m.cursor = max(0, len(m.proposals)-1)
				}
				m.clampListWindow()
			}
		}
		return m, tickCmd() // schedule the next refresh

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.diff != "" {
			m.diffLines = wrapDiff(m.diff, m.width)
		}
		m.clampListWindow()
		return m, nil

	case tea.MouseMsg:
		event := tea.MouseEvent(msg)
		if !event.IsWheel() {
			return m, nil
		}
		delta := 1
		if event.Button == tea.MouseButtonWheelUp {
			delta = -1
		}
		if m.current == viewDiff {
			m.scroll = max(0, min(m.maxDiffScroll(), m.scroll+3*delta))
		} else {
			m.cursor = max(0, min(len(m.proposals)-1, m.cursor+delta))
			m.clampListWindow()
		}
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
	case "pgup":
		m.cursor = max(0, m.cursor-m.listVisibleRows())
	case "pgdown":
		m.cursor = min(max(0, len(m.proposals)-1), m.cursor+m.listVisibleRows())
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(m.proposals)-1)
	case "enter", "d":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			diff, err := m.app.ProposalDiff(p)
			if err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.diff = diff
				m.diffLines = wrapDiff(diff, m.width)
				m.diffAdds, m.diffDels, m.diffFiles = diffStats(diff)
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
	m.clampListWindow()
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

// listVisibleRows is how many proposals fit between the title and the footer.
func (m Model) listVisibleRows() int {
	return max(3, m.height-6)
}

// clampListWindow keeps the scroll window over the cursor after movement,
// removals, reloads, and resizes.
func (m *Model) clampListWindow() {
	visible := m.listVisibleRows()
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+visible {
		m.listOffset = m.cursor - visible + 1
	}
	m.listOffset = max(0, min(m.listOffset, max(0, len(m.proposals)-visible)))
}

// diffVisibleRows is the diff content height: screen minus the two-line
// header, the blank separator, and the two-line footer.
func (m Model) diffVisibleRows() int {
	return max(1, m.height-6)
}

func (m Model) maxDiffScroll() int {
	return max(0, len(m.diffLines)-m.diffVisibleRows())
}

func (m Model) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.diffVisibleRows()
	switch msg.String() {
	case "q", "esc", "backspace":
		m.current = viewList
	case "up", "k":
		if m.scroll > 0 {
			m.scroll--
		}
	case "down", "j":
		m.scroll = min(m.maxDiffScroll(), m.scroll+1)
	case "pgup":
		m.scroll = max(0, m.scroll-page)
	case "pgdown":
		m.scroll = min(m.maxDiffScroll(), m.scroll+page)
	case "home", "g":
		m.scroll = 0
	case "end", "G":
		m.scroll = m.maxDiffScroll()
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
	title := titleStyle.Render(" ⚡ BOLTCODE") + helpStyle.Render(" · Review Dashboard")
	b.WriteString(chatHeaderStyle.Width(m.width).Render(ansi.Truncate(title, m.width, "…")) + "\n\n")

	if len(m.proposals) == 0 {
		b.WriteString(helpStyle.Render("  ✨ nothing to review — all caught up") + "\n")
	} else {
		end := min(len(m.proposals), m.listOffset+m.listVisibleRows())
		for i := m.listOffset; i < end; i++ {
			p := m.proposals[i]
			line := fmt.Sprintf("  %-10s", p.ID) + " " +
				fmt.Sprintf("%-12s", p.Agent) + " " + p.Summary
			if i == m.cursor {
				b.WriteString(selectedStyle.Width(m.width).Render(line) + "\n")
			} else {
				b.WriteString(helpStyle.Render(fmt.Sprintf("  %-10s", p.ID)) + " " +
					chatAccentStyle.Render(fmt.Sprintf("%-12s", p.Agent)) + " " +
					normalStyle.Render(p.Summary) + "\n")
			}
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(statusStyle.Render(ansi.Truncate("  "+m.message, m.width, "…")) + "\n\n")
	}
	help := "  ↑/↓ navigate · PgUp/PgDn page · enter/d diff · a approve · m merge (press 2×) · r reject · q quit"
	if len(m.proposals) > 0 {
		help = fmt.Sprintf("  %d/%d", m.cursor+1, len(m.proposals)) + help
	}
	if m.confirmingMerge {
		help = "  press m again to confirm merge — any other key cancels"
	}
	b.WriteString(helpStyle.Render(ansi.Truncate(help, m.width, "…")))
	return b.String()
}

// wrapDiff pre-renders a diff into display lines wrapped at the given width,
// so long lines neither overflow the screen nor break scroll math.
func wrapDiff(diff string, width int) []string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		out = append(out, strings.Split(wrapChatLine(line, width), "\n")...)
	}
	return out
}

// diffStats summarizes a unified diff: added/removed content lines (file
// headers excluded) and how many files are touched.
func diffStats(diff string) (adds, dels, files int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			files++
		case strings.HasPrefix(line, "+++"):
		case strings.HasPrefix(line, "+"):
			adds++
		case strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "-"):
			dels++
		}
	}
	return adds, dels, files
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
	meta := helpStyle.Render("  ╭─ "+p.ID) +
		chatAccentStyle.Render(" · "+p.Agent) +
		helpStyle.Render(" · "+p.Summary)
	b.WriteString(ansi.Truncate(meta, m.width, "…") + "\n")
	fileWord := "files"
	if m.diffFiles == 1 {
		fileWord = "file"
	}
	stats := diffAddStyle.Render(fmt.Sprintf("+%d", m.diffAdds)) + " " +
		diffDelStyle.Render(fmt.Sprintf("−%d", m.diffDels)) +
		helpStyle.Render(fmt.Sprintf(" · %d %s", m.diffFiles, fileWord))
	b.WriteString(helpStyle.Render("  ╰─ ") + stats + "\n\n")

	maxScroll := m.maxDiffScroll()
	scroll := min(m.scroll, maxScroll)
	visible := m.diffVisibleRows()
	end := min(scroll+visible, len(m.diffLines))

	for _, line := range m.diffLines[scroll:end] {
		switch {
		// +++/--- are file headers, not added/removed content.
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			b.WriteString(diffHeadStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffDelStyle.Render(line) + "\n")
		case strings.HasPrefix(line, "@@"):
			b.WriteString(diffHunkStyle.Render(line) + "\n")
		default:
			b.WriteString(line + "\n")
		}
	}

	scrollPct := 100
	if maxScroll > 0 {
		scrollPct = scroll * 100 / maxScroll
	}
	// The percentage leads the footer so it survives truncation on narrow
	// terminals.
	footer := fmt.Sprintf(
		"  %d%% · ↑/↓ scroll · PgUp/PgDn page · Home/End ends · q/esc back · a approve · m merge (press 2×) · r reject", scrollPct)
	b.WriteString("\n" + helpStyle.Render(ansi.Truncate(footer, m.width, "…")))
	return b.String()
}

// Run starts the interactive TUI program.
func Run(app *command.App) error {
	m, err := New(app)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
