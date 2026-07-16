package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func (m *ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.popup != nil {
		return m.handlePopupKey(msg)
	}
	m.clearSelection()

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.textarea.Value() != "" {
			m.textarea.Reset()
			m.statusHint = "draft cleared"
			return m, nil
		}
		if m.busy && !m.finished {
			m.agent.CancelRequest()
			m.statusHint = "interrupt requested"
			m.activity = "Stopping current action"
			m.activityDetail = ""
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlD:
		m.agent.Cancel()
		m.quitting = true
		return m, tea.Quit

	case tea.KeyCtrlO:
		if len(m.toolTimeline) == 0 {
			m.statusHint = "no tool calls yet"
			return m, nil
		}
		m.toolsExpanded = !m.toolsExpanded
		if m.toolsExpanded {
			m.statusHint = "tool details expanded"
		} else {
			m.statusHint = "tool details collapsed"
		}
		m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case tea.KeyCtrlP:
		return m.showSessionPopup()

	case tea.KeyShiftTab:
		if m.busy && !m.finished {
			m.statusHint = "mode can be changed when the agent is ready"
			return m, nil
		}
		if m.finished {
			m.statusHint = "session ended"
			return m, nil
		}
		return m.setChatMode(agent.NextMode(m.mode))

	case tea.KeyEnter:
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		// Some Windows terminals emit Enter immediately after an IME commits
		// candidate text. Consume that Enter once so choosing “你好” does not
		// accidentally submit the whole chat message.
		if !m.imeCommitAt.IsZero() && time.Since(m.imeCommitAt) < 250*time.Millisecond {
			m.imeCommitAt = time.Time{}
			m.statusHint = "IME text committed — press Enter again to send"
			return m, nil
		}
		if m.finished {
			m.statusHint = "session ended"
			return m, nil
		}
		if m.busy {
			m.statusHint = "draft held — wait for the current reply"
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			return m.handleSlashCommand(text)
		}
		m.sendUserMessage(text, true)
		return m, nil

	case tea.KeyPgUp:
		m.viewport.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && !msg.Paste && containsNonASCII(msg.Runes) {
			m.imeCommitAt = time.Now()
		} else {
			m.imeCommitAt = time.Time{}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		val := m.textarea.Value()
		if val == "/" {
			return m.showCommandPopup()
		}
		if strings.HasSuffix(val, "@") {
			return m.showFilePopup()
		}
		return m, cmd
	}
}

func containsNonASCII(runes []rune) bool {
	for _, r := range runes {
		if r > 127 {
			return true
		}
	}
	return false
}

func (m *ChatModel) handleSlashCommand(text string) (tea.Model, tea.Cmd) {
	switch text {
	case "/":
		return m.showCommandPopup()
	case "/model":
		m.textarea.Reset()
		return m.showModelPopup()
	case "/mode":
		m.textarea.Reset()
		return m.showModePopup()
	case "/mode build", "/mode plan", "/mode ask":
		mode, _ := agent.ParseMode(strings.TrimPrefix(text, "/mode "))
		return m.setChatMode(mode)
	case "/sessions":
		return m.showSessionPopup()
	case "/exit", "/quit", "/q":
		m.quitting = true
		return m, tea.Quit
	case "/clear":
		m.lines = nil
		m.logDirty = true
		m.refreshViewport()
		m.textarea.Reset()
		return m, nil
	case "/retry":
		if !m.recovering {
			m.statusHint = "nothing to retry"
			m.textarea.Reset()
			return m, nil
		}
		return m.retryLastRequest()
	default:
		m.sendUserMessage(text, false)
		return m, nil
	}
}

func (m *ChatModel) retryLastRequest() (tea.Model, tea.Cmd) {
	select {
	case m.agentIn <- "/retry":
		m.recovering = false
		m.busy = true
		m.statusHint = "retrying last request"
		m.activity = "Retrying request"
		m.activityDetail = ""
		m.busySince = time.Now()
		m.textarea.Placeholder = chatBusyPlaceholder
		m.lines = append(m.lines, "", chatPromptStyle.Render("  ↻ retrying last request"))
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return m, nil
	default:
		m.statusHint = "retry channel is not ready"
		return m, nil
	}
}

// sendUserMessage atomically mirrors one accepted prompt in the viewport and
// in the one-slot agent channel. It is called only after the ready handshake,
// so a second Enter cannot create a visible-but-not-yet-consumed message.
func (m *ChatModel) sendUserMessage(text string, leadingBlank bool) bool {
	select {
	case m.agentIn <- text:
		if leadingBlank {
			m.lines = append(m.lines, "")
		}
		m.busy = true
		m.recovering = false
		m.lastError = ""
		m.statusHint = ""
		m.activity = "Thinking"
		m.activityDetail = ""
		m.lastToolError = false
		m.busySince = time.Now()
		m.textarea.Placeholder = chatBusyPlaceholder
		m.lines = append(m.lines, chatUserStyle.Render("  ◆ you"))
		for _, line := range strings.Split(text, "\n") {
			m.lines = append(m.lines, "    "+line)
		}
		m.logDirty = true
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return true
	default:
		m.busy = true
		m.statusHint = "draft held — input channel is not ready"
		return false
	}
}
