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
		// The spinner and activity detail live on the activity row above; the
		// status bar only carries the state dot.
		left = chatPromptStyle.Render("● working")
	} else {
		left = chatGreenStyle.Render("● ready")
	}
	if m.statusHint != "" {
		left += chatDimStyle.Render(" · " + m.statusHint)
	}
	if m.newBelow {
		left += chatPromptStyle.Render(" · ↓ new messages")
	}
	permission := "confirm"
	if m.mode != agent.ModeBuild {
		permission = "read-only"
	} else if m.agent.AutoAllow {
		permission = "auto-allow"
	}
	// Model, mode and scroll position are shown once in the session header;
	// the status bar only adds what the header does not have.
	var rightParts []string
	if effort := agent.NormalizeReasoningEffort(m.agent.Config.ReasoningEffort); effort != agent.ReasoningAuto {
		rightParts = append(rightParts, "think:"+string(effort))
	}
	rightParts = append(rightParts, permission, "Shift+Tab mode", "Ctrl+P sessions")
	right := chatDimStyle.Render(strings.Join(rightParts, " · "))
	primary := fitStatusLine(left+"  │  "+right, m.width)

	window := m.agent.ContextWindow()
	contextPct := 0
	if window > 0 {
		contextPct = int(float64(m.contextTokens) / float64(window) * 100)
	}
	statusParts := []string{fmt.Sprintf("context %s %d%% · %s/%s", contextBar(contextPct, 10), contextPct, formatMetricTokens(m.contextTokens), formatMetricTokens(window))}
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

// progressBar renders a fixed-cell usage bar: filled cells in the given
// style, the rest dim.
func progressBar(pct, cells int, style lipgloss.Style) string {
	if cells < 1 {
		cells = 1
	}
	pct = max(0, min(100, pct))
	filled := pct * cells / 100
	return style.Render(strings.Repeat("█", filled)) + chatDimStyle.Render(strings.Repeat("░", cells-filled))
}

// contextBar colors the context-usage bar by pressure: green while healthy,
// yellow as the auto-compact threshold (80%) nears, red at 90%+.
func contextBar(pct, cells int) string {
	style := chatGreenStyle
	switch {
	case pct >= 90:
		style = chatErrorStyle
	case pct >= 70:
		style = chatWarnStyle
	}
	return progressBar(pct, cells, style)
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
