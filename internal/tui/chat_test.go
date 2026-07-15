package tui

import (
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
