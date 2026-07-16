package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

var (
	planCompletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Strikethrough(true)
	planCurrentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	planPendingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func (m *ChatModel) setPlanItems(items []agent.TodoItem) {
	m.planItems = append(m.planItems[:0], items...)
	m.planDone = 0
	m.planTotal = len(m.planItems)
	m.planCurrent = ""
	for _, item := range m.planItems {
		switch item.Status {
		case "completed":
			m.planDone++
		case "in_progress":
			if m.planCurrent == "" {
				m.planCurrent = item.Content
			}
		}
	}
}

func (m *ChatModel) renderPlanPanel() string {
	if len(m.planItems) == 0 {
		return ""
	}
	lines := []string{chatDimStyle.Render(fmt.Sprintf("  ╭─ task plan · %d/%d", m.planDone, m.planTotal))}
	itemWidth := max(12, m.width-12)
	for _, item := range m.planItems {
		content := strings.Join(strings.Fields(item.Content), " ")
		if content == "" {
			content = "(untitled task)"
		}
		content = ansi.Truncate(content, itemWidth, "…")
		switch item.Status {
		case "completed":
			lines = append(lines, "  │  "+chatGreenStyle.Render("✓")+" "+planCompletedStyle.Render(content))
		case "in_progress":
			lines = append(lines, "  │  "+chatPromptStyle.Render("◆")+" "+planCurrentStyle.Render(content))
		default:
			lines = append(lines, "  │  "+chatDimStyle.Render("○")+" "+planPendingStyle.Render(content))
		}
	}
	lines = append(lines, chatDimStyle.Render("  ╰─"))
	return strings.Join(lines, "\n")
}
