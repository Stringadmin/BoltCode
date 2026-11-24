package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

var (
	planCompletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMutedText)).Strikethrough(true)
	planCurrentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorWarning)).Bold(true)
	planPendingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSubtleText))
)

func (m *ChatModel) setPlanItems(items []agent.TodoItem) {
	m.planItems = append(m.planItems[:0], items...)
	m.planDirty = true
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

func (m *ChatModel) clearCompletedPlanForNewTurn() bool {
	if m.planTotal > 0 && m.planDone == m.planTotal {
		// Display state only: the agent retires its own TodoList at the start
		// of the input loop, so the UI must not write into it from here.
		m.setPlanItems(nil)
		return true
	}
	return false
}

func planItemsCompleted(items []agent.TodoItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Status != "completed" {
			return false
		}
	}
	return true
}

// applyPlanEvent folds the plan payload of an agent event into the panel
// state. It lives next to the other plan-panel logic instead of the tool
// timeline, which merely forwards events here.
func (m *ChatModel) applyPlanEvent(event agent.Event) {
	if event.PlanItems != nil || event.ToolName == "todo_write" {
		// Ready and tool events are consumed by independent Bubble Tea commands.
		// A completed todo_write event can therefore arrive after agentReadyMsg.
		// Never let that late event resurrect a plan already retired at ready.
		if !m.busy && planItemsCompleted(event.PlanItems) {
			m.setPlanItems(nil)
		} else {
			m.setPlanItems(event.PlanItems)
		}
	} else if event.PlanTotal > 0 {
		// Compatibility with callers/tests that only populate the compact plan
		// counters rather than the structured snapshot.
		m.planDone = event.PlanDone
		m.planTotal = event.PlanTotal
		m.planCurrent = event.PlanCurrent
	}
}

func (m *ChatModel) renderPlanPanel() string {
	if len(m.planItems) == 0 {
		return ""
	}
	progress := 0
	if m.planTotal > 0 {
		progress = m.planDone * 100 / m.planTotal
	}
	lines := []string{chatDimStyle.Render(fmt.Sprintf("  ╭─ task plan · %d/%d ", m.planDone, m.planTotal)) + progressBar(progress, 10, chatGreenStyle)}
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
