package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

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
	filtered  []int // indices into sessions matching filter; nil means "all" (no query yet)
	filter    string
	selected  int // index into the visible (filtered) list
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
	// Seed with the real terminal size so the first frame is already correct;
	// the first WindowSizeMsg only arrives after the initial render.
	width, height := 80, 24
	if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	model := &sessionPickerModel{sessions: sessions, width: width, height: height}
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	final, ok := result.(*sessionPickerModel)
	if !ok || final.cancelled || !final.chosen {
		return nil, nil
	}
	visible := final.visibleSessions()
	if len(visible) == 0 || final.selected >= len(visible) {
		return nil, nil
	}
	selected := final.sessions[visible[final.selected]]
	return &selected, nil
}

// visibleSessions returns the session indices matching the current filter.
// A nil filtered list means no query was typed yet — everything is visible.
func (m *sessionPickerModel) visibleSessions() []int {
	if m.filtered != nil {
		return m.filtered
	}
	indices := make([]int, len(m.sessions))
	for i := range m.sessions {
		indices[i] = i
	}
	return indices
}

func (m *sessionPickerModel) refilter() {
	m.filtered = m.filtered[:0]
	query := strings.ToLower(m.filter)
	for i, session := range m.sessions {
		if query == "" ||
			fuzzyMatch(strings.ToLower(sessionPreview(session)), query) ||
			fuzzyMatch(strings.ToLower(session.Model), query) ||
			fuzzyMatch(strings.ToLower(session.ID), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.selected = 0
}

func (m *sessionPickerModel) Init() tea.Cmd { return nil }

func (m *sessionPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case tea.KeyMsg:
		visibleCount := len(m.visibleSessions())
		switch message.Type {
		case tea.KeyUp, tea.KeyCtrlP:
			if m.selected > 0 {
				m.selected--
			}
		case tea.KeyDown, tea.KeyCtrlN:
			if m.selected < visibleCount-1 {
				m.selected++
			}
		case tea.KeyHome:
			m.selected = 0
		case tea.KeyEnd:
			m.selected = max(0, visibleCount-1)
		case tea.KeyEnter:
			if visibleCount > 0 {
				m.chosen = true
				return m, tea.Quit
			}
		case tea.KeyBackspace:
			if m.filter != "" {
				runes := []rune(m.filter)
				m.filter = string(runes[:len(runes)-1])
				m.refilter()
			}
		case tea.KeyEsc, tea.KeyCtrlC:
			// First Esc only clears the query; a second one cancels.
			if m.filter != "" {
				m.filter = ""
				m.refilter()
			} else {
				m.cancelled = true
				return m, tea.Quit
			}
		case tea.KeyRunes:
			// "q" only quits with an empty query; otherwise it is filter text.
			if message.String() == "q" && m.filter == "" {
				m.cancelled = true
				return m, tea.Quit
			}
			m.filter += message.String()
			m.refilter()
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
	visibleRows := height - 11
	if visibleRows < 3 {
		visibleRows = 3
	}
	visible := m.visibleSessions()
	start := m.selected - visibleRows/2
	if start < 0 {
		start = 0
	}
	end := start + visibleRows
	if end > len(visible) {
		end = len(visible)
		start = end - visibleRows
		if start < 0 {
			start = 0
		}
	}

	filterDisplay := chatDimStyle.Render("type to filter")
	if m.filter != "" {
		filterDisplay = sessionPickerNormal.Render(m.filter)
	}
	lines := []string{
		chatTitleStyle.Render("↻ Resume a BoltCode session"),
		chatDimStyle.Render(fmt.Sprintf("%d saved sessions · newest first", len(m.sessions))),
		chatPromptStyle.Render("› ") + filterDisplay,
		"",
	}
	if len(visible) == 0 {
		lines = append(lines, chatDimStyle.Render("  (no matches)"))
	}
	for row := start; row < end; row++ {
		session := m.sessions[visible[row]]
		marker := "  "
		if row == m.selected {
			marker = "▶ "
		}
		model := session.Model
		if model == "" {
			model = "unknown model"
		}
		prefix := fmt.Sprintf("%s%-8s  %s  %-18s  ", marker, session.ID, session.UpdatedAt.Format("01-02 15:04"), ansi.Truncate(model, 18, "…"))
		previewWidth := boxWidth - ansi.StringWidth(prefix) - 6
		if previewWidth < 8 {
			previewWidth = 8
		}
		rowText := prefix + ansi.Truncate(sessionPreview(session), previewWidth, "…")
		if row == m.selected {
			lines = append(lines, sessionPickerActive.Width(boxWidth-6).Render(rowText))
		} else {
			lines = append(lines, sessionPickerNormal.Render(rowText))
		}
	}
	lines = append(lines,
		"",
		chatDimStyle.Render(fmt.Sprintf("%d/%d · ↑↓ navigate · Enter resume · Esc/q cancel", m.selected+1, max(1, len(visible)))),
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
