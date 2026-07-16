package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

var (
	sessionPickerBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection)).Padding(1, 2)
	sessionPickerActive = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorText)).Background(lipgloss.Color(theme.ColorPanel)).Bold(true)
	sessionPickerNormal = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSoftText))
)

type sessionPickerModel struct {
	sessions  []agent.Session
	selected  int
	width     int
	height    int
	cancelled bool
	chosen    bool
}

// PickSession opens the same full-screen interaction style as chat for
// `boltcode --resume` without an id. It returns nil when the user cancels.
func PickSession(sessions []agent.Session) (*agent.Session, error) {
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no saved sessions")
	}
	model := &sessionPickerModel{sessions: sessions, width: 100, height: 30}
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	final, ok := result.(*sessionPickerModel)
	if !ok || final.cancelled || !final.chosen {
		return nil, nil
	}
	selected := final.sessions[final.selected]
	return &selected, nil
}

func (m *sessionPickerModel) Init() tea.Cmd { return nil }

func (m *sessionPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case tea.KeyMsg:
		switch message.Type {
		case tea.KeyUp, tea.KeyCtrlP:
			if m.selected > 0 {
				m.selected--
			}
		case tea.KeyDown, tea.KeyCtrlN:
			if m.selected < len(m.sessions)-1 {
				m.selected++
			}
		case tea.KeyHome:
			m.selected = 0
		case tea.KeyEnd:
			m.selected = len(m.sessions) - 1
		case tea.KeyEnter:
			m.chosen = true
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyRunes:
			if message.String() == "q" {
				m.cancelled = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *sessionPickerModel) View() string {
	width := m.width
	height := m.height
	if width < 50 {
		width = 50
	}
	if height < 12 {
		height = 12
	}
	boxWidth := width - 8
	if boxWidth > 120 {
		boxWidth = 120
	}
	visibleRows := height - 10
	if visibleRows < 3 {
		visibleRows = 3
	}
	start := m.selected - visibleRows/2
	if start < 0 {
		start = 0
	}
	end := start + visibleRows
	if end > len(m.sessions) {
		end = len(m.sessions)
		start = end - visibleRows
		if start < 0 {
			start = 0
		}
	}

	lines := []string{
		chatTitleStyle.Render("↻ Resume a BoltCode session"),
		chatDimStyle.Render(fmt.Sprintf("%d saved sessions · newest first", len(m.sessions))),
		"",
	}
	for index := start; index < end; index++ {
		session := m.sessions[index]
		marker := "  "
		if index == m.selected {
			marker = "▶ "
		}
		model := session.Model
		if model == "" {
			model = "unknown model"
		}
		prefix := fmt.Sprintf("%s%-8s  %s  %-18s  ", marker, session.ID, session.UpdatedAt.Format("01-02 15:04"), truncatePicker(model, 18))
		previewWidth := boxWidth - ansi.StringWidth(prefix) - 6
		if previewWidth < 8 {
			previewWidth = 8
		}
		row := prefix + ansi.Truncate(sessionPreview(session), previewWidth, "…")
		if index == m.selected {
			lines = append(lines, sessionPickerActive.Width(boxWidth-6).Render(row))
		} else {
			lines = append(lines, sessionPickerNormal.Render(row))
		}
	}
	lines = append(lines,
		"",
		chatDimStyle.Render(fmt.Sprintf("%d/%d · ↑↓ navigate · Enter resume · Esc/q cancel", m.selected+1, len(m.sessions))),
	)
	box := sessionPickerBorder.Width(boxWidth).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func sessionPreview(session agent.Session) string {
	preview := strings.TrimSpace(session.Intent)
	if preview == "" {
		for _, message := range session.Messages {
			if message.Role != "user" {
				continue
			}
			if value, ok := message.Content.(string); ok {
				preview = value
				if marker := strings.LastIndex(preview, "## User request\n"); marker >= 0 {
					preview = preview[marker+len("## User request\n"):]
				}
				break
			}
		}
	}
	preview = strings.Join(strings.Fields(preview), " ")
	if preview == "" {
		return "(no summary)"
	}
	return preview
}

func truncatePicker(value string, width int) string {
	return ansi.Truncate(value, width, "…")
}
