package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// layout sizes the viewport and textarea to the current terminal dimensions.
func (m *ChatModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	inputH := chatInputRows + 2
	headerH := 1
	statusH := 2
	activityH := 1 // reserved spinner/activity row, so busy transitions never resize the viewport
	vpH := m.height - inputH - statusH - headerH - activityH - chatConversationGapRows
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
	m.textarea.SetWidth(max(1, m.width-2))
	// Timeline and plan rendering depend on the width; rebuild their caches.
	m.timelineDirty = true
	m.planDirty = true
	m.refreshViewport()
}

// refreshViewport re-renders changed parts of the message log into the
// viewport, word-wrapping to the current width. Appended lines are wrapped
// incrementally; a resize or a cleared log triggers a full re-wrap.
func (m *ChatModel) refreshViewport() {
	if !m.ready {
		return
	}
	wrapWidth := m.width - 3
	if m.renderWidth != wrapWidth || len(m.wrappedLines) > len(m.lines) {
		m.wrappedLines = make([]string, len(m.lines))
		for i, line := range m.lines {
			m.wrappedLines[i] = wrapChatLine(line, wrapWidth)
		}
		m.renderWidth = wrapWidth
	} else {
		for i := len(m.wrappedLines); i < len(m.lines); i++ {
			m.wrappedLines = append(m.wrappedLines, wrapChatLine(m.lines[i], wrapWidth))
		}
	}
	content := strings.Join(m.wrappedLines, "\n")
	if m.timelineDirty {
		m.timelineCache = m.renderToolTimeline()
		m.timelineDirty = false
	}
	if m.planDirty {
		m.planCache = m.renderPlanPanel()
		m.planDirty = false
	}
	if m.timelineCache != "" {
		if content != "" {
			content += "\n\n"
		}
		content += m.timelineCache
	}
	if m.planCache != "" {
		if content != "" {
			content += "\n\n"
		}
		content += m.planCache
	}
	// SetContent re-splits and measures every line, so skip it when nothing
	// actually changed.
	if content != m.viewportContent {
		m.viewport.SetContent(content)
		m.viewportContent = content
	}
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

// wrapChatLine renders one log line at the given width, preserving leading
// indentation across wrapped segments. The result may contain newlines.
func wrapChatLine(line string, width int) string {
	if width < 20 || ansi.StringWidth(line) <= width {
		return line
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	bodyWidth := width - indent
	if bodyWidth < 1 {
		bodyWidth = width
	}
	wrapped := strings.Split(ansi.WrapWc(strings.TrimLeft(line, " "), bodyWidth, "/\\"), "\n")
	for i, part := range wrapped {
		wrapped[i] = strings.Repeat(" ", indent) + strings.TrimLeft(part, " ")
	}
	return strings.Join(wrapped, "\n")
}

// wrapChatContent wraps a whole multi-line block. Kept for tests and one-off
// rendering; the chat hot path wraps lines incrementally via wrapChatLine.
func wrapChatContent(content string, width int) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = wrapChatLine(line, width)
	}
	return strings.Join(lines, "\n")
}

// appendLogBlank starts a new visual block in the log unless the log already
// ends with a blank line. Shared by the popup selection side effects.
func (m *ChatModel) appendLogBlank() {
	if len(m.lines) > 0 && m.lines[len(m.lines)-1] != "" {
		m.lines = append(m.lines, "")
	}
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
		m.renderActivityRow(),
		strings.Repeat(" ", max(0, m.width)),
		m.renderStatusBar(),
		m.renderInputBox(),
	)
	if m.popup != nil {
		return m.renderPopupOverlay(base)
	}
	return base
}

// renderActivityRow is the single-row spinner/activity line between the
// conversation and the status bar. It lives outside the viewport so spinner
// ticks re-render one row instead of re-splitting the whole message log.
// The row is always occupied (blank while idle) so the layout never jumps.
func (m *ChatModel) renderActivityRow() string {
	if !m.busy || m.finished {
		return ""
	}
	return m.renderActivityLine()
}

func (m *ChatModel) renderSessionHeader() string {
	state := "ready"
	stateStyle := chatGreenStyle
	switch {
	case m.failed:
		state, stateStyle = "failed", chatErrorStyle
	case m.finished:
		state, stateStyle = "stopped", chatDimStyle
	case m.recovering:
		state, stateStyle = "recovery", chatErrorStyle
	case m.busy:
		state, stateStyle = "working", chatPromptStyle
	}
	scroll := 100
	if m.ready && !m.viewport.AtBottom() {
		scroll = int(m.viewport.ScrollPercent() * 100)
	}
	sep := chatDimStyle.Render(" · ")
	content := chatTitleStyle.Render(" ⚡ BoltCode") + sep +
		chatBrightStyle.Render(m.workspace) + sep +
		chatAccentStyle.Render(m.model) + sep +
		modeBadge(m.mode) + sep +
		stateStyle.Render(state) + sep +
		chatDimStyle.Render(fmt.Sprintf("↕ %d%%", scroll)) + " "
	if ansi.StringWidth(content) > m.width {
		content = ansi.Truncate(content, m.width, "…")
	}
	return chatHeaderStyle.Width(m.width).Render(content)
}

// modeBadge renders the agent mode as an uppercase, color-coded segment.
func modeBadge(mode agent.Mode) string {
	style := chatGreenStyle
	switch mode {
	case agent.ModePlan:
		style = chatWarnStyle
	case agent.ModeAsk:
		style = chatBlueStyle
	}
	return style.Render(strings.ToUpper(string(mode)))
}

func (m *ChatModel) renderInputBox() string {
	style := inputBoxReadyStyle
	switch {
	case m.failed || m.recovering:
		style = inputBoxErrorStyle
	case m.busy:
		style = inputBoxBusyStyle
	}
	return style.Render(m.textarea.View())
}
