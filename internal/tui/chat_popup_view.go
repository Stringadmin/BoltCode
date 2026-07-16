package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *ChatModel) renderPopupOverlay(base string) string {
	popup := m.renderPopupPanel()
	return overlayCentered(base, popup, m.width, m.height)
}

func (m *ChatModel) renderPopupPanel() string {
	p := m.popup
	popupWidth := m.width - 12
	if popupWidth > 100 {
		popupWidth = 100
	}
	if popupWidth < 32 {
		popupWidth = max(20, m.width-4)
	}
	// Style.Width includes horizontal padding in its wrapping calculation.
	// Rows must use the actual inner width or a full selected row wraps by the
	// four padding cells, which was especially visible with CJK session titles.
	itemWidth := max(12, popupWidth-popupStyle.GetHorizontalPadding())
	var items []string
	items = append(items, chatTitleStyle.Render(p.title)+chatDimStyle.Render("  esc"))
	if p.subtitle != "" {
		items = append(items, chatDimStyle.Render(p.subtitle))
	}

	if p.kind == "confirm" {
		items = append(items, "")
		items = append(items, chatPromptStyle.Render("  "+p.prompt))
		items = append(items, "")
		for i, item := range p.items {
			items = append(items, renderPopupItem(item, i == p.selected, itemWidth))
		}
		items = append(items, "")
		items = append(items, chatDimStyle.Render("↑↓ navigate · Enter select"))
		return popupStyle.Width(popupWidth).Render(strings.Join(items, "\n"))
	}

	search := chatDimStyle.Render("Search")
	if p.filter != "" {
		search = popupNormalStyle.Render(p.filter)
	}
	items = append(items, chatPromptStyle.Render("› ")+search, "")
	if len(p.filtered) == 0 {
		items = append(items, chatDimStyle.Render("  (no matches)"))
	}
	maxRows := min(12, max(3, m.height-10))
	start := 0
	if p.selected >= maxRows {
		start = p.selected - maxRows + 1
	}
	end := min(start+maxRows, len(p.filtered))
	for visibleIndex := start; visibleIndex < end; visibleIndex++ {
		item := p.items[p.filtered[visibleIndex]]
		items = append(items, renderPopupItem(item, visibleIndex == p.selected, itemWidth))
	}
	if len(p.filtered) > maxRows {
		items = append(items, chatDimStyle.Render(fmt.Sprintf("  … %d/%d", p.selected+1, len(p.filtered))))
	}
	items = append(items, "")
	footer := "type to search · ↑↓ navigate · Enter select · Esc cancel"
	if p.kind == "sessions" {
		footer = "type to search · ↑↓ navigate · Enter switch · Esc cancel"
	}
	items = append(items, chatDimStyle.Render(footer))
	return popupStyle.Width(popupWidth).Render(strings.Join(items, "\n"))
}

func renderPopupItem(item popupItem, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = "▶ "
	}
	labelWidth := max(8, width/2)
	label := ansi.Truncate(item.label, labelWidth, "…")
	line := marker + label
	if item.desc != "" {
		remaining := width - ansi.StringWidth(line) - 2
		if remaining > 4 {
			line += "  " + ansi.Truncate(item.desc, remaining, "…")
		}
	}
	line = ansi.Truncate(line, width, "…")
	if selected {
		return popupSelStyle.Width(width).Render(line)
	}
	return popupNormalStyle.Render(line)
}

// overlayCentered composites a modal over the existing chat instead of using
// lipgloss.Place, whose blank padding replaces the entire alternate screen.
// ANSI-aware cuts preserve colors and wide CJK characters on both sides of
// the popup, giving the terminal equivalent of a floating window.
func overlayCentered(base, popup string, width, height int) string {
	if width <= 0 || height <= 0 {
		return popup
	}
	popupLines := strings.Split(popup, "\n")
	popupWidth := min(width, ansiBlockWidth(popupLines))
	popupHeight := min(height, len(popupLines))
	startX := max(0, (width-popupWidth)/2)
	startY := max(0, (height-popupHeight)/2)

	baseLines := strings.Split(base, "\n")
	output := make([]string, height)
	for row := 0; row < height; row++ {
		baseLine := ""
		if row < len(baseLines) {
			baseLine = baseLines[row]
		}
		baseLine = fitOverlayLine(baseLine, width)
		popupRow := row - startY
		if popupRow < 0 || popupRow >= popupHeight || popupRow >= len(popupLines) {
			output[row] = baseLine
			continue
		}

		overlayLine := fitOverlayLine(popupLines[popupRow], popupWidth)
		left := fitOverlayLine(ansi.Cut(baseLine, 0, startX), startX)
		rightWidth := max(0, width-startX-popupWidth)
		right := fitOverlayLine(ansi.Cut(baseLine, startX+popupWidth, width), rightWidth)
		output[row] = fitOverlayLine(left+overlayLine+right, width)
	}
	return strings.Join(output, "\n")
}

func ansiBlockWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	return width
}

func fitOverlayLine(line string, width int) string {
	line = ansi.Truncate(line, width, "")
	if padding := width - ansi.StringWidth(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}
