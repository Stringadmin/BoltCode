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
		m.timelineDirty = true
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
		if m.finished {
			m.imeCommitAt = time.Time{}
			m.statusHint = "session ended"
			return m, nil
		}
		if m.busy {
			m.imeCommitAt = time.Time{}
			m.statusHint = "draft held — wait for the current reply"
			return m, nil
		}
		// Some Windows terminals emit Enter immediately after an IME commits
		// candidate text. Consume that Enter once so choosing “你好” does not
		// accidentally submit the whole chat message. Checked after the
		// finished/busy guards so those hints win when they apply.
		if !m.imeCommitAt.IsZero() && time.Since(m.imeCommitAt) < 250*time.Millisecond {
			m.imeCommitAt = time.Time{}
			m.statusHint = "IME text committed — press Enter again to send"
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			return m.handleSlashCommand(text)
		}
		m.sendUserMessage(text, true)
		return m, m.kickBusyTicks()

	case tea.KeyPgUp:
		m.viewport.ViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.ViewDown()
		return m, nil

	case tea.KeyHome, tea.KeyEnd:
		if m.textarea.Value() != "" {
			// While typing, Home/End belong to the editor.
			return m, m.updateTextarea(msg)
		}
		if msg.Type == tea.KeyHome {
			m.viewport.GotoTop()
		} else {
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.KeyUp:
		if m.textarea.Line() == 0 && len(m.promptHistory) > 0 {
			m.recallPrompt(-1)
			return m, nil
		}
		return m, m.updateTextarea(msg)
	case tea.KeyDown:
		if m.historyIndex < len(m.promptHistory) {
			m.recallPrompt(1)
			return m, nil
		}
		return m, m.updateTextarea(msg)

	default:
		if msg.Type == tea.KeyRunes && !msg.Paste && containsNonASCII(msg.Runes) {
			m.imeCommitAt = time.Now()
		} else {
			m.imeCommitAt = time.Time{}
		}
		cmd := m.updateTextarea(msg)
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

// updateTextarea forwards an editing key to the textarea and returns its
// command. Shared by the editing branches so navigation keys can fall back to
// text editing without duplicating the call.
func (m *ChatModel) updateTextarea(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return cmd
}

// recallPrompt walks the prompt history (direction -1 = older, +1 = newer).
// The in-progress draft is saved on entry and restored past the newest entry.
func (m *ChatModel) recallPrompt(direction int) {
	if len(m.promptHistory) == 0 {
		return
	}
	if m.historyIndex >= len(m.promptHistory) {
		if direction > 0 {
			return // already at the newest entry
		}
		m.historyDraft = m.textarea.Value()
		m.historyIndex = len(m.promptHistory)
	}
	m.historyIndex += direction
	if m.historyIndex < 0 {
		m.historyIndex = 0
	}
	if m.historyIndex >= len(m.promptHistory) {
		m.historyIndex = len(m.promptHistory)
		m.textarea.SetValue(m.historyDraft)
		return
	}
	m.textarea.SetValue(m.promptHistory[m.historyIndex])
	m.textarea.CursorEnd()
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
		// Forward to the agent so its conversation history is cleared too —
		// clearing only the viewport left the model receiving (and billing)
		// the full history. The agent's confirmation line arrives afterwards
		// on the fresh screen.
		if m.finished {
			m.statusHint = "session ended"
			m.textarea.Reset()
			return m, nil
		}
		if m.busy {
			m.statusHint = "wait for the current reply, then /clear"
			m.textarea.Reset()
			return m, nil
		}
		if !m.sendUserMessage(text, false) {
			m.textarea.Reset()
			return m, nil
		}
		m.lines = nil
		m.toolTimeline = nil
		m.timelineDirty = true
		m.setPlanItems(nil)
		m.refreshViewport()
		return m, m.kickBusyTicks()
	case "/retry":
		if !m.recovering {
			m.statusHint = "nothing to retry"
			m.textarea.Reset()
			return m, nil
		}
		return m.retryLastRequest()
	default:
		m.sendUserMessage(text, false)
		return m, m.kickBusyTicks()
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
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return m, m.kickBusyTicks()
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
		m.clearCompletedPlanForNewTurn()
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
		m.lines = append(m.lines, userMessageLines(text)...)
		// Prompt history for ↑/↓ recall, capped so a long session does not
		// grow it without bound.
		if len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != text {
			m.promptHistory = append(m.promptHistory, text)
			if len(m.promptHistory) > 100 {
				m.promptHistory = append([]string(nil), m.promptHistory[len(m.promptHistory)-100:]...)
			}
		}
		m.historyIndex = len(m.promptHistory)
		m.historyDraft = ""
		m.refreshViewport()
		m.viewport.GotoBottom()
		m.textarea.Reset()
		return true
	default:
		// Nothing was sent, so the agent is not working on this message —
		// setting busy here would wedge the UI in a permanent working state.
		m.statusHint = "draft held — input channel is not ready"
		return false
	}
}
