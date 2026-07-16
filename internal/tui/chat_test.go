package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func newChatTestModel() *ChatModel {
	return NewChat(&agent.Agent{Config: agent.Config{Model: "test-model"}})
}

func TestChatStartsBusyAndHoldsDraft(t *testing.T) {
	m := newChatTestModel()
	if !m.busy {
		t.Fatal("chat should start busy while the initial agent turn is running")
	}
	initialLines := len(m.lines)

	m.textarea.SetValue("1")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.agentIn) != 0 {
		t.Fatal("busy chat queued a prompt")
	}
	if len(m.lines) != initialLines {
		t.Fatalf("busy chat rendered an unsent prompt: %q", m.lines)
	}
	if got := m.textarea.Value(); got != "1" {
		t.Fatalf("draft = %q, want it retained", got)
	}
	if !strings.Contains(m.statusHint, "draft held") {
		t.Fatalf("status hint = %q", m.statusHint)
	}
}

func TestInitialIntentIsVisibleInsideTUI(t *testing.T) {
	m := NewChat(&agent.Agent{
		Config: agent.Config{Model: "test-model"},
		Intent: "清理 TUI 架构\n补齐交互状态",
	})
	view := ansi.Strip(strings.Join(m.lines, "\n"))
	if !strings.Contains(view, "◆ you\n    清理 TUI 架构\n    补齐交互状态") {
		t.Fatalf("initial intent hierarchy missing:\n%s", view)
	}
}

func TestChatReadyAllowsExactlyOnePrompt(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	if m.busy {
		t.Fatal("ready signal did not enable input")
	}

	m.textarea.SetValue("first")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-m.agentIn; got != "first" {
		t.Fatalf("sent prompt = %q", got)
	}
	if !m.busy {
		t.Fatal("chat should become busy immediately after submit")
	}
	joined := ansi.Strip(strings.Join(m.lines, "\n"))
	if !strings.Contains(joined, "◆ you\n    first") {
		t.Fatalf("user message hierarchy missing: %q", joined)
	}
	lineCount := len(m.lines)

	m.textarea.SetValue("second")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.agentIn) != 0 {
		t.Fatal("second prompt was queued while busy")
	}
	if len(m.lines) != lineCount {
		t.Fatal("second prompt was rendered even though it was not sent")
	}
	if got := m.textarea.Value(); got != "second" {
		t.Fatalf("second draft = %q, want it retained", got)
	}
}

func TestBusyChatRendersAnimatedActivityLayer(t *testing.T) {
	m := newChatTestModel()
	m.width = 80
	m.height = 24
	m.layout()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Preparing project context") || !strings.Contains(view, "╰─") {
		t.Fatalf("busy activity layer missing from view:\n%s", view)
	}
	if m.spinner.View() == "" {
		t.Fatal("spinner has no visible frame")
	}
}

func TestWrapChatContentRespectsViewportWidth(t *testing.T) {
	input := "  这是一段很长的中文内容，用来确认聊天正文会根据视口宽度自动换行，而不是被终端直接裁切。"
	wrapped := wrapChatContent(input, 24)
	for _, line := range strings.Split(wrapped, "\n") {
		if width := ansi.StringWidth(line); width > 24 {
			t.Fatalf("wrapped line width = %d: %q", width, line)
		}
	}
}

func TestActivityFromToolLine(t *testing.T) {
	line := "    \033[38;5;241m╭─\033[0m 📖 \033[1mRead\033[0m"
	if got := activityFromAgentLine(line); got != "Reading files" {
		t.Fatalf("activity = %q", got)
	}
}

func TestStructuredToolEventShowsExactProgress(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 24
	m.layout()
	m.handleAgentEvent(agent.Event{
		Kind:     agent.EventToolStarted,
		Activity: "Reading file",
		Detail:   "internal/agent/provider.go",
	})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Reading file") || !strings.Contains(view, "internal/agent/provider.go") {
		t.Fatalf("structured activity missing from view:\n%s", view)
	}
}

func TestToolTimelineCollapsesAndExpandsDetails(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 30
	m.layout()
	m.handleAgentEvent(agent.Event{Kind: agent.EventToolStarted, ToolID: "call-1", ToolName: "read_file", Activity: "Reading file", Detail: "internal/tui/chat.go"})
	m.handleAgentEvent(agent.Event{Kind: agent.EventToolFinished, ToolID: "call-1", ToolName: "read_file", Detail: "internal/tui/chat.go", Result: "line one\nline two", Duration: 20 * time.Millisecond})

	collapsed := ansi.Strip(m.renderToolTimeline())
	if !strings.Contains(collapsed, "Read") || strings.Contains(collapsed, "line one") {
		t.Fatalf("collapsed timeline =\n%s", collapsed)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	expanded := ansi.Strip(m.renderToolTimeline())
	if !m.toolsExpanded || !strings.Contains(expanded, "line one") || !strings.Contains(expanded, "line two") {
		t.Fatalf("expanded timeline =\n%s", expanded)
	}
}

func TestRecoverableErrorOffersRetryWithoutEndingSession(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 30
	m.layout()
	m.handleAgentEvent(agent.Event{Kind: agent.EventError, Result: "API 429: rate limited", IsError: true})
	if !m.recovering || m.finished || m.busy {
		t.Fatalf("recovery state: recovering=%v finished=%v busy=%v", m.recovering, m.finished, m.busy)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Request failed") || !strings.Contains(view, "/retry") || !strings.Contains(view, "● recover") {
		t.Fatalf("recovery UI missing:\n%s", view)
	}
	m.handleSlashCommand("/retry")
	if got := <-m.agentIn; got != "/retry" {
		t.Fatalf("retry input = %q", got)
	}
	if !m.busy || m.recovering {
		t.Fatalf("retry did not resume busy state: busy=%v recovering=%v", m.busy, m.recovering)
	}
}

func TestStatusShowsContextCostAndPlan(t *testing.T) {
	a := &agent.Agent{
		Config: agent.Config{Model: "deepseek-chat"},
		TodoList: []agent.TodoItem{
			{Content: "inspect", Status: "completed"},
			{Content: "implement", Status: "in_progress"},
		},
		Stats: agent.NewStats(),
	}
	a.Stats.Add(a.Config.Model, 32000, 1000)
	m := NewChat(a)
	m.width = 220
	m.height = 30
	m.layout()
	m.Update(statusTickMsg{})
	status := ansi.Strip(m.renderStatusBar())
	for _, want := range []string{"context 50%", "32k/1.0k in/out", "$", "plan 1/2", "implement"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
}

func TestCtrlCInterruptsBusyActionBeforeQuitting(t *testing.T) {
	a := &agent.Agent{Config: agent.Config{Model: "test-model"}}
	m := NewChat(a)
	model, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil || model.(*ChatModel).quitting {
		t.Fatal("first Ctrl+C while busy should request interruption, not quit")
	}
	if !strings.Contains(m.statusHint, "interrupt requested") || m.activity != "Stopping current action" {
		t.Fatalf("interrupt state = hint %q activity %q", m.statusHint, m.activity)
	}
}

func TestNormalizeAgentLineKeepsSpacingButDropsControlArtifacts(t *testing.T) {
	if clean, drop := normalizeAgentLine(""); drop || clean != "" {
		t.Fatalf("explicit blank line = %q, drop=%v", clean, drop)
	}
	if _, drop := normalizeAgentLine("\r\033[K"); !drop {
		t.Fatal("control-only spinner artifact should be dropped")
	}
	if clean, drop := normalizeAgentLine("  content"); drop || clean != "  content" {
		t.Fatalf("content line = %q, drop=%v", clean, drop)
	}
}

func TestStatusBarStaysSingleWidth(t *testing.T) {
	m := newChatTestModel()
	m.width = 48
	m.activity = "Reading a very long workspace filename"
	m.model = "a-very-long-model-name-that-would-overflow"
	bar := m.renderStatusBar()
	for _, line := range strings.Split(bar, "\n") {
		if width := ansi.StringWidth(line); width > m.width {
			t.Fatalf("status width = %d, want <= %d: %q", width, m.width, ansi.Strip(line))
		}
	}
}

func TestChineseIMECommitBackspaceAndSubmit(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})

	// Windows IMEs deliver the committed phrase as one KeyRunes message.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好世界")})
	if got := m.textarea.Value(); got != "你好世界" {
		t.Fatalf("IME commit = %q", got)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.textarea.Value(); got != "你好世" {
		t.Fatalf("Chinese backspace = %q, want one rune removed", got)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-m.agentIn; got != "你好世" {
		t.Fatalf("submitted Chinese text = %q", got)
	}
}

func TestChineseBracketedPasteIsInsertedLiterally(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("查询整个项目文件"), Paste: true})
	if got := m.textarea.Value(); got != "查询整个项目文件" {
		t.Fatalf("Chinese paste = %q", got)
	}
}

func TestIMECommitEnterDoesNotAccidentallySubmit(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.agentIn) != 0 || m.textarea.Value() != "你好" {
		t.Fatalf("IME confirmation submitted unexpectedly: channel=%d value=%q", len(m.agentIn), m.textarea.Value())
	}
	if !strings.Contains(m.statusHint, "IME text committed") {
		t.Fatalf("IME guard hint = %q", m.statusHint)
	}

	// A deliberate second Enter submits normally.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-m.agentIn; got != "你好" {
		t.Fatalf("second Enter submitted %q", got)
	}
}

func TestInputReservesSecondRowForWindowsIME(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 30
	m.layout()
	if lines := strings.Count(m.textarea.View(), "\n") + 1; lines < chatInputRows {
		t.Fatalf("textarea rows = %d, want at least %d", lines, chatInputRows)
	}
	wantViewportHeight := m.height - (chatInputRows + 2) - 2
	if m.viewport.Height != wantViewportHeight {
		t.Fatalf("viewport height = %d, want %d", m.viewport.Height, wantViewportHeight)
	}
}

func TestInputDrawsPromptOnlyOnFirstRow(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 30
	m.layout()
	view := ansi.Strip(m.textarea.View())
	if count := strings.Count(view, "❯"); count != 1 {
		t.Fatalf("prompt count = %d, want 1:\n%s", count, view)
	}
}

func TestResumeHistoryUsesCurrentTUIAndHidesInternalContext(t *testing.T) {
	a := &agent.Agent{
		Config:  agent.Config{Model: "deepseek-v4-pro"},
		Workdir: `F:\BoltCode`,
		Messages: []agent.Message{
			{Role: "user", Content: "## Current Project\nprivate scan data\n\n## User request\n检查项目架构"},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "我会检查整个项目。"},
				map[string]interface{}{"type": "tool_use", "id": "call_1", "name": "list_files", "input": map[string]interface{}{}},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_1", "content": "secret tool output"},
			}},
		},
	}
	m := NewChat(a)
	view := ansi.Strip(strings.Join(m.lines, "\n"))
	for _, want := range []string{"resumed", "检查项目架构", "assistant", "我会检查整个项目。"} {
		if !strings.Contains(view, want) {
			t.Fatalf("resume history missing %q:\n%s", want, view)
		}
	}
	for _, hidden := range []string{"private scan data", "secret tool output"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("resume history leaked internal content %q:\n%s", hidden, view)
		}
	}
}

func TestNextAgentInputSignalsReady(t *testing.T) {
	m := newChatTestModel()
	result := make(chan string, 1)
	go func() { result <- m.nextAgentInput() }()

	select {
	case <-m.readyCh:
	case <-time.After(time.Second):
		t.Fatal("agent input did not signal readiness")
	}

	m.agentIn <- "hello"
	select {
	case got := <-result:
		if got != "hello" {
			t.Fatalf("agent input = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("agent input did not receive submitted prompt")
	}
}

func TestAgentDoneRejectsFurtherInput(t *testing.T) {
	m := newChatTestModel()
	initialLines := len(m.lines)
	m.Update(agentResultMsg{})
	m.Update(agentDoneMsg{})
	m.textarea.SetValue("too late")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.agentIn) != 0 || len(m.lines) != initialLines {
		t.Fatal("finished chat accepted another prompt")
	}
	if got := m.textarea.Value(); got != "too late" {
		t.Fatalf("finished-session draft = %q, want retained", got)
	}
}

func TestAgentErrorIsVisibleAndNotReportedAsNormalEnd(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 24
	m.layout()
	m.Update(agentResultMsg{err: errors.New("API 429: rate limit exceeded")})
	m.Update(agentDoneMsg{})

	if !m.finished || !m.failed {
		t.Fatalf("finished=%v failed=%v", m.finished, m.failed)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Agent stopped") || !strings.Contains(view, "API 429: rate limit exceeded") {
		t.Fatalf("agent error missing from view:\n%s", view)
	}
	if strings.Contains(m.statusHint, "session ended") {
		t.Fatalf("error was mislabeled as normal end: %q", m.statusHint)
	}
}
