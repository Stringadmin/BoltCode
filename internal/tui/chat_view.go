package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/theme"
)

// layout sizes the viewport and textarea to the current terminal dimensions.
func (m *ChatModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	inputH := chatInputRows + 2
	headerH := 1
	statusH := 2
	vpH := m.height - inputH - statusH - headerH - chatConversationGapRows
	if vpH < 3 {
		vpH = 3
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, vpH)
		m.viewport.MouseWheelEnabled = true
		m.viewport.MouseWheelDelta = 4
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpH
	}
	m.textarea.SetWidth(m.width - 2)
	m.refreshViewport()
}

// refreshViewport re-renders the message log into the viewport, word-wrapping
// to the current width.
func (m *ChatModel) refreshViewport() {
	if !m.ready {
		return
	}
	wrapWidth := m.width - 3
	if m.logDirty || m.renderWidth != wrapWidth {
		m.renderedLog = wrapChatContent(strings.Join(m.lines, "\n"), wrapWidth)
		m.renderWidth = wrapWidth
		m.logDirty = false
	}
	content := m.renderedLog
	if timeline := m.renderToolTimeline(); timeline != "" {
		if content != "" {
			content += "\n\n"
		}
		content += timeline
	}
	if plan := m.renderPlanPanel(); plan != "" {
		if content != "" {
			content += "\n\n"
		}
		content += plan
	}
	if m.busy && !m.finished {
		if content != "" {
			content += "\n\n"
		}
		content += m.renderActivityLine()
	}
	m.viewport.SetContent(content)
}

func (m *ChatModel) renderActivityLine() string {
	activity := m.activity
	if activity == "" {
		activity = "Thinking"
	}
	elapsed := time.Since(m.busySince).Round(100 * time.Millisecond)
	activityStyle := chatPromptStyle
	if m.lastToolError {
		activityStyle = chatErrorStyle
	}
	line := chatDimStyle.Render("  ╰─ ") + m.spinner.View() + " " + activityStyle.Render(activity)
	if m.activityDetail != "" {
		line += chatDimStyle.Render(" · " + m.activityDetail)
	}
	return line + chatDimStyle.Render(" · "+elapsed.String())
}

func wrapChatContent(content string, width int) string {
	if width < 20 {
		return content
	}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		bodyWidth := width - indent
		if bodyWidth < 1 {
			bodyWidth = width
		}
		wrapped := strings.Split(ansi.WrapWc(strings.TrimLeft(line, " "), bodyWidth, "/\\"), "\n")
		for _, part := range wrapped {
			out = append(out, strings.Repeat(" ", indent)+strings.TrimLeft(part, " "))
		}
	}
	return strings.Join(out, "\n")
}

func activityFromAgentLine(line string) string {
	plain := strings.TrimSpace(ansi.Strip(line))
	switch {
	case strings.Contains(plain, "✦ assistant"):
		return "Writing response"
	case strings.Contains(plain, "📖") && strings.Contains(plain, "Read"):
		return "Reading files"
	case strings.Contains(plain, "📁") && strings.Contains(plain, "List"):
		return "Exploring files"
	case strings.Contains(plain, "🔍") && strings.Contains(plain, "Search"):
		return "Searching code"
	case strings.Contains(plain, "⚙") && strings.Contains(plain, "Run"):
		return "Running command"
	case strings.Contains(plain, "✏") && (strings.Contains(plain, "Edit") || strings.Contains(plain, "MultiEdit")):
		return "Editing files"
	case strings.Contains(plain, "📝") && strings.Contains(plain, "Write"):
		return "Writing files"
	case strings.HasPrefix(plain, "╰─"):
		return "Analyzing tool results"
	default:
		return ""
	}
}

func (m *ChatModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Starting BoltCode..."
	}

	base := lipgloss.JoinVertical(lipgloss.Left,
		m.renderSessionHeader(),
		m.renderConversationViewport(),
		strings.Repeat(" ", max(0, m.width)),
		m.renderStatusBar(),
		m.renderInputBox(),
	)
	if m.popup != nil {
		return m.renderPopupOverlay(base)
	}
	return base
}

func (m *ChatModel) renderSessionHeader() string {
	state := "ready"
	if m.failed {
		state = "failed"
	} else if m.finished {
		state = "stopped"
	} else if m.recovering {
		state = "recovery"
	} else if m.busy {
		state = "working"
	}
	workspace := filepath.Base(filepath.Clean(m.agent.Workdir))
	if workspace == "" || workspace == "." {
		workspace = "workspace"
	}
	scroll := 100
	if m.ready && !m.viewport.AtBottom() {
		scroll = int(m.viewport.ScrollPercent() * 100)
	}
	content := fmt.Sprintf(" ⚡ BoltCode  ·  %s  ·  %s  ·  %s  ·  %s  ·  ↕ %d%% ", workspace, m.model, m.mode, state, scroll)
	if ansi.StringWidth(content) > m.width {
		content = ansi.Truncate(content, m.width, "…")
	}
	return chatHeaderStyle.Width(m.width).Render(content)
}

func (m *ChatModel) renderInputBox() string {
	style := inputBoxStyle
	switch {
	case m.failed || m.recovering:
		style = style.BorderForeground(lipgloss.Color(theme.ColorDanger))
	case m.busy:
		style = style.BorderForeground(lipgloss.Color(theme.ColorPrimary))
	default:
		style = style.BorderForeground(lipgloss.Color(theme.ColorSuccess))
	}
	return style.Render(m.textarea.View())
}
