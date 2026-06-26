// Package tui implements the interactive review interface using Bubble Tea.
// `boltcode tui` enters this mode: browse proposals, view diffs, approve/reject
// with keyboard shortcuts — no need to type commands repeatedly.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// view tracks which screen the user is on.
type view int

const (
	viewList view = iota
	viewDiff
)

// Model is the Bubble Tea model for the review TUI.
type Model struct {
	app       *command.App
	proposals []*model.Proposal
	cursor    int
	current   view
	diff      string
	width     int
	height    int
	scroll    int
	message   string
	quitting  bool
}

// New creates the TUI model, pre-loading pending proposals.
func New(app *command.App) (Model, error) {
	pending, err := app.ListPending()
	if err != nil {
		return Model{}, err
	}
	return Model{app: app, proposals: pending, width: 80, height: 24}, nil
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("208"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("white")).
			Background(lipgloss.Color("62"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statusStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("34"))
)

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
	switch msg.String() {
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
			if _, err := m.app.Approve(p.ID, "human"); err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.message = fmt.Sprintf("✓ approved %s", p.ID)
				m.proposals = removeAt(m.proposals, m.cursor)
				if m.cursor >= len(m.proposals) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	case "m":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			if _, err := m.app.ApproveAndMerge(p.ID, "human"); err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.message = fmt.Sprintf("✓ merged %s", p.ID)
				m.proposals = removeAt(m.proposals, m.cursor)
				if m.cursor >= len(m.proposals) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	case "r":
		if len(m.proposals) > 0 {
			p := m.proposals[m.cursor]
			if err := m.app.Reject(p.ID, "human", "rejected via TUI"); err != nil {
				m.message = "error: " + err.Error()
			} else {
				m.message = fmt.Sprintf("✗ rejected %s", p.ID)
				m.proposals = removeAt(m.proposals, m.cursor)
				if m.cursor >= len(m.proposals) && m.cursor > 0 {
					m.cursor--
				}
			}
		}
	}
	return m, nil
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
	b.WriteString(helpStyle.Render("  ↑/↓ navigate  enter/d diff  a approve  m merge  r reject  q quit"))
	return b.String()
}

func (m Model) viewDiff() string {
	var b strings.Builder
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
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Render(line) + "\n")
		} else if strings.HasPrefix(line, "-") {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n" + helpStyle.Render("  ↑/↓ scroll  q/esc back  a approve  m merge  r reject"))
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