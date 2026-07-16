package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/theme"
)

var chatSelectionStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(theme.ColorBrightText)).
	Background(lipgloss.Color(theme.ColorSelection))

type selectionPoint struct {
	row int
	col int
}

type chatSelection struct {
	anchor    selectionPoint
	focus     selectionPoint
	lines     []string
	selecting bool
	moved     bool
}

type clipboardResultMsg struct {
	characters int
	err        error
}

func (m *ChatModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(msg)
	if event.IsWheel() {
		m.clearSelection()
		if m.popup == nil {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if m.popup != nil {
		return m, nil
	}

	switch {
	case event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress:
		lines := m.visibleConversationLines()
		point, ok := m.conversationPoint(event.X, event.Y, lines, false)
		if !ok {
			m.clearSelection()
			return m, nil
		}
		m.selection = chatSelection{
			anchor:    point,
			focus:     point,
			lines:     lines,
			selecting: true,
		}
		m.statusHint = "selecting conversation text"
		return m, nil

	case event.Action == tea.MouseActionMotion && m.selection.selecting:
		point, ok := m.conversationPoint(event.X, event.Y, m.selection.lines, true)
		if ok {
			m.selection.focus = point
			m.selection.moved = point != m.selection.anchor
		}
		return m, nil

	case event.Action == tea.MouseActionRelease && m.selection.selecting:
		point, ok := m.conversationPoint(event.X, event.Y, m.selection.lines, true)
		if ok {
			m.selection.focus = point
			m.selection.moved = point != m.selection.anchor
		}
		m.selection.selecting = false
		if !m.selection.moved || m.selectedConversationText() == "" {
			m.clearSelection()
			return m, nil
		}
		m.statusHint = "selection ready · right-click to copy"
		return m, nil

	case event.Button == tea.MouseButtonRight && event.Action == tea.MouseActionPress:
		text := m.selectedConversationText()
		if text == "" {
			m.statusHint = "drag over conversation text, then right-click to copy"
			return m, nil
		}
		write := m.copyText
		if write == nil {
			write = clipboard.WriteAll
		}
		return m, func() tea.Msg {
			return clipboardResultMsg{
				characters: utf8.RuneCountInString(text),
				err:        write(text),
			}
		}
	}
	return m, nil
}

func (m *ChatModel) clearSelection() {
	m.selection = chatSelection{}
}

// conversationPoint translates a terminal coordinate into the visible
// conversation viewport. The fixed session header occupies row zero.
func (m *ChatModel) conversationPoint(x, y int, lines []string, clamp bool) (selectionPoint, bool) {
	if !m.ready || len(lines) == 0 {
		return selectionPoint{}, false
	}
	row := y - 1
	if !clamp && (row < 0 || row >= m.viewport.Height || row >= len(lines)) {
		return selectionPoint{}, false
	}
	row = max(0, min(row, min(m.viewport.Height, len(lines))-1))
	col := x
	if col < 0 {
		col = 0
	}
	width := ansi.StringWidth(lines[row])
	if col > width {
		col = width
	}
	return selectionPoint{row: row, col: col}, true
}

func (m *ChatModel) visibleConversationLines() []string {
	view := ansi.Strip(m.viewport.View())
	lines := strings.Split(view, "\n")
	if len(lines) > m.viewport.Height {
		lines = lines[:m.viewport.Height]
	}
	for len(lines) < m.viewport.Height {
		lines = append(lines, "")
	}
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return lines
}

func (m *ChatModel) selectedConversationText() string {
	selection := m.selection
	if !selection.moved || len(selection.lines) == 0 {
		return ""
	}
	start, end := orderedSelection(selection.anchor, selection.focus)
	segments := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row && row < len(selection.lines); row++ {
		line := selection.lines[row]
		left, right := selectedCellBounds(line, row, start, end)
		segments = append(segments, strings.TrimRight(ansi.Cut(line, left, right), " "))
	}
	return strings.Join(segments, "\n")
}

func (m *ChatModel) renderConversationViewport() string {
	view := m.viewport.View()
	if !m.selection.moved {
		return view
	}
	lines := strings.Split(view, "\n")
	start, end := orderedSelection(m.selection.anchor, m.selection.focus)
	for row := start.row; row <= end.row && row < len(lines); row++ {
		plain := ansi.Strip(lines[row])
		left, right := selectedCellBounds(plain, row, start, end)
		if right <= left {
			continue
		}
		width := ansi.StringWidth(lines[row])
		before := ansi.Cut(lines[row], 0, left)
		selected := ansi.Strip(ansi.Cut(lines[row], left, right))
		after := ansi.Cut(lines[row], right, width)
		lines[row] = before + chatSelectionStyle.Render(selected) + after
	}
	return strings.Join(lines, "\n")
}

func orderedSelection(first, second selectionPoint) (selectionPoint, selectionPoint) {
	if first.row < second.row || (first.row == second.row && first.col <= second.col) {
		return first, second
	}
	return second, first
}

func selectedCellBounds(line string, row int, start, end selectionPoint) (int, int) {
	width := ansi.StringWidth(line)
	left, right := 0, width
	if row == start.row {
		left, _ = graphemeCellBounds(line, start.col)
	}
	if row == end.row {
		_, right = graphemeCellBounds(line, end.col)
	}
	if right < left {
		right = left
	}
	return left, right
}

// graphemeCellBounds expands a cell coordinate to the complete grapheme it
// touches. This prevents selecting only half of Chinese characters or emoji.
func graphemeCellBounds(line string, col int) (int, int) {
	if col < 0 {
		col = 0
	}
	position := 0
	for remaining := line; remaining != ""; {
		cluster, width := ansi.FirstGraphemeCluster(remaining, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		if width <= 0 {
			width = 1
		}
		if col < position+width {
			return position, position + width
		}
		position += width
		remaining = remaining[len(cluster):]
	}
	return position, position
}
