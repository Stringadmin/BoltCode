package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/theme"
)

func (m *ChatModel) renderStatusBar() string {
	left := ""
	if m.failed {
		left = chatErrorStyle.Render("● failed")
	} else if m.finished {
		left = chatDimStyle.Render("● stopped")
	} else if m.recovering {
		left = chatErrorStyle.Render("● recover")
	} else if m.busy {
		activity := m.activity
		if activity == "" {
			activity = "Thinking"
		}
		left = m.spinner.View() + " " + chatPromptStyle.Render(activity)
		if m.activityDetail != "" {
			left += chatDimStyle.Render(" · " + m.activityDetail)
		}
	} else {
		left = chatGreenStyle.Render("● ready")
	}
	if m.statusHint != "" {
		left += chatDimStyle.Render(" · " + m.statusHint)
	}
	permission := "confirm"
	if m.mode != agent.ModeBuild {
		permission = "read-only"
	} else if m.agent.AutoAllow {
		permission = "auto-allow"
	}
	scroll := 100
	if m.ready && !m.viewport.AtBottom() {
		scroll = int(m.viewport.ScrollPercent() * 100)
	}
	right := chatDimStyle.Render(fmt.Sprintf("%s · %s · %s · ↕ %d%% · Shift+Tab mode · Ctrl+P sessions", m.model, m.mode, permission, scroll))
	primary := fitStatusLine(left+"  │  "+right, m.width)

	window := m.agent.ContextWindow()
	contextPct := 0
	if window > 0 {
		contextPct = int(float64(m.contextTokens) / float64(window) * 100)
	}
	statusParts := []string{fmt.Sprintf("context %d%% · %s/%s", contextPct, formatMetricTokens(m.contextTokens), formatMetricTokens(window))}
	if m.planTotal > 0 {
		plan := fmt.Sprintf("tasks %d/%d", m.planDone, m.planTotal)
		if m.planCurrent != "" {
			plan += " · " + m.planCurrent
		}
		statusParts = append(statusParts, plan)
	}
	statusParts = append(statusParts,
		fmt.Sprintf("%d tools · %d rounds", m.tools, m.rounds),
		fmt.Sprintf("%s/%s in/out", formatMetricTokens(m.inputTokens), formatMetricTokens(m.outputTokens)),
	)
	toolAction := "expand"
	if m.toolsExpanded {
		toolAction = "collapse"
	}
	secondary := strings.Join(statusParts, "  │  ") + fmt.Sprintf("  │  Ctrl+O %s tool results · wheel/PgUp scroll", toolAction)
	secondary = fitStatusLine(chatDimStyle.Render(secondary), m.width)
	return lipgloss.JoinVertical(lipgloss.Left,
		statusBarStyle.Width(m.width).Render(primary),
		chatDimStyle.Background(lipgloss.Color(theme.ColorDeepSurface)).Width(m.width).Render(secondary),
	)
}

func formatMetricTokens(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	}
	if tokens >= 10_000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func fitStatusLine(content string, width int) string {
	if width > 0 && ansi.StringWidth(content) > width {
		content = ansi.Truncate(content, width, "…")
	}
	if pad := width - lipgloss.Width(content); pad > 0 {
		content += strings.Repeat(" ", pad)
	}
	return content
}
