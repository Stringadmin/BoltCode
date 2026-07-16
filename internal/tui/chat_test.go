package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestNewUserMessageClearsCompletedPlan(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.setPlanItems([]agent.TodoItem{
		{Content: "inspect", Status: "completed"},
		{Content: "implement", Status: "completed"},
	})
	m.textarea.SetValue("start the next task")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if got := <-m.agentIn; got != "start the next task" {
		t.Fatalf("sent prompt = %q", got)
	}
	if m.planTotal != 0 || len(m.planItems) != 0 || m.renderPlanPanel() != "" {
		t.Fatalf("completed plan survived new user turn: %+v", m.planItems)
	}
	if strings.Contains(ansi.Strip(m.renderStatusBar()), "tasks 2/2") {
		t.Fatal("completed task count survived in status bar")
	}
}

func TestNewUserMessageKeepsInProgressPlan(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.setPlanItems([]agent.TodoItem{
		{Content: "inspect", Status: "completed"},
		{Content: "implement", Status: "in_progress"},
	})
	m.textarea.SetValue("use the second approach")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	<-m.agentIn

	if m.planTotal != 2 || len(m.planItems) != 2 {
		t.Fatalf("active plan was cleared by clarification: %+v", m.planItems)
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

func TestPlanPanelVisualizesCompletedCurrentAndPendingTasks(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.setPlanItems([]agent.TodoItem{
		{Content: "inspect architecture", Status: "completed"},
		{Content: "implement visual plan", Status: "in_progress"},
		{Content: "run regression tests", Status: "pending"},
	})
	panel := m.renderPlanPanel()
	plain := ansi.Strip(panel)
	for _, want := range []string{"task plan · 1/3", "✓ inspect architecture", "◆ implement visual plan", "○ run regression tests"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plan panel missing %q:\n%s", want, plain)
		}
	}
	if !planCompletedStyle.GetStrikethrough() {
		t.Fatal("completed task style must enable strikethrough")
	}
}

func TestTodoWriteEventReplacesAndClearsPlanPanel(t *testing.T) {
	m := newChatTestModel()
	m.recordToolEvent(agent.Event{
		Kind:     agent.EventToolFinished,
		ToolName: "todo_write",
		PlanItems: []agent.TodoItem{
			{Content: "first", Status: "completed"},
			{Content: "second", Status: "in_progress"},
		},
	})
	if m.planDone != 1 || m.planTotal != 2 || m.planCurrent != "second" || len(m.planItems) != 2 {
		t.Fatalf("updated plan state: done=%d total=%d current=%q items=%d", m.planDone, m.planTotal, m.planCurrent, len(m.planItems))
	}
	m.recordToolEvent(agent.Event{Kind: agent.EventToolFinished, ToolName: "todo_write"})
	if m.planTotal != 0 || len(m.planItems) != 0 || m.renderPlanPanel() != "" {
		t.Fatalf("cleared plan remained visible: total=%d items=%d", m.planTotal, len(m.planItems))
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

func TestStatusShowsContextPlanAndShortcutsWithoutCost(t *testing.T) {
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
	for _, want := range []string{"context 50%", "32k/1.0k in/out", "tasks 1/2", "implement", "Ctrl+P sessions", "Ctrl+O expand tool results"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "$") {
		t.Fatalf("status should not show estimated cost:\n%s", status)
	}
}

func TestModelPopupIncludesCurrentCustomModel(t *testing.T) {
	m := NewChat(&agent.Agent{Config: agent.Config{Provider: "deepseek", Model: "deepseek-v4-pro", BaseURL: "https://api.deepseek.com/v1"}})
	m.showModelPopup()
	if m.popup == nil || len(m.popup.items) == 0 {
		t.Fatal("model popup is empty")
	}
	if m.popup.items[0].id != "deepseek-v4-pro" || !strings.Contains(m.popup.items[0].desc, "current") {
		t.Fatalf("custom current model missing from popup: %+v", m.popup.items)
	}
}

func TestModePopupAndShiftTabCycleRespectReadyState(t *testing.T) {
	m := newChatTestModel()
	m.showModePopup()
	if m.popup == nil || m.popup.kind != "modes" || len(m.popup.items) != 3 {
		t.Fatalf("mode popup = %+v", m.popup)
	}
	m.popup = nil
	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.mode != agent.ModeBuild || !strings.Contains(m.statusHint, "when the agent is ready") {
		t.Fatalf("busy mode change was not blocked: mode=%s hint=%q", m.mode, m.statusHint)
	}
	m.Update(agentReadyMsg{})
	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.mode != agent.ModePlan || m.agent.EffectiveMode() != agent.ModePlan {
		t.Fatalf("first mode cycle = UI %s agent %s", m.mode, m.agent.EffectiveMode())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.mode != agent.ModeAsk {
		t.Fatalf("second mode cycle = %s, want ask", m.mode)
	}
	status := ansi.Strip(m.renderStatusBar())
	if !strings.Contains(status, "ask · read-only") {
		t.Fatalf("read-only mode missing from status:\n%s", status)
	}
}

func TestCtrlPSessionPopupFiltersWorkspaceAndPreservesDraft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "project")
	otherWorkspace := filepath.Join(t.TempDir(), "other")

	current := &agent.Agent{
		Config:   agent.Config{Model: "deepseek-chat"},
		Workdir:  workspace,
		Intent:   "fix the current project",
		Messages: []agent.Message{{Role: "user", Content: "fix the current project"}, {Role: "assistant", Content: "working on it"}},
	}
	currentID, err := agent.SaveSession(current)
	if err != nil {
		t.Fatal(err)
	}
	other := &agent.Agent{
		Config:   agent.Config{Model: "gpt-test"},
		Workdir:  otherWorkspace,
		Intent:   "unrelated conversation",
		Messages: []agent.Message{{Role: "user", Content: "unrelated conversation"}, {Role: "assistant", Content: "other"}},
	}
	if _, err := agent.SaveSession(other); err != nil {
		t.Fatal(err)
	}

	m := NewChat(&agent.Agent{Config: agent.Config{Model: "deepseek-chat"}, Workdir: workspace})
	m.textarea.SetValue("unfinished draft")
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.popup == nil || m.popup.kind != "sessions" {
		t.Fatal("Ctrl+P did not open the session popup")
	}
	if len(m.popup.items) != 1 || m.popup.items[0].id != currentID {
		t.Fatalf("workspace sessions = %+v, want only %s", m.popup.items, currentID)
	}
	m.handlePopupKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.textarea.Value(); got != "unfinished draft" {
		t.Fatalf("closing session popup changed draft to %q", got)
	}
}

func TestSessionPopupSelectionRequestsSafeSwitch(t *testing.T) {
	m := NewChat(&agent.Agent{Config: agent.Config{Model: "test-model"}, Workdir: t.TempDir()})
	model, cmd := m.applyPopupSelection("sessions", popupItem{id: "1234abcd"})
	result := model.(*ChatModel)
	if cmd == nil || !result.quitting || result.switchSessionID != "1234abcd" {
		t.Fatalf("session switch state: cmd=%v quitting=%v id=%q", cmd != nil, result.quitting, result.switchSessionID)
	}
}

func TestSessionPopupSearchesPreviewAndMetadata(t *testing.T) {
	p := &popupState{
		items: []popupItem{
			{id: "1", label: "修复输入框", desc: "deepseek-chat"},
			{id: "2", label: "Refactor storage", desc: "openai"},
		},
	}
	p.applyFilter("deep")
	if len(p.filtered) != 1 || p.items[p.filtered[0]].id != "1" {
		t.Fatalf("metadata search result = %+v", p.filtered)
	}
	p.applyFilter("storage")
	if len(p.filtered) != 1 || p.items[p.filtered[0]].id != "2" {
		t.Fatalf("preview search result = %+v", p.filtered)
	}
}

func TestSessionPopupChineseSelectionStaysOnOneRow(t *testing.T) {
	m := newChatTestModel()
	m.width = 86
	m.height = 24
	m.layout()
	m.popup = &popupState{
		kind:     "sessions",
		title:    "Sessions",
		subtitle: `F:\BoltCode`,
		items: []popupItem{{
			id:    "fa49c41d",
			label: "你好",
			desc:  "07-16 14:31 · deepseek-v4-pro · 6 messages · fa49c41d",
		}},
	}
	m.popup.applyFilter("")
	panel := m.renderPopupPanel()
	if got, want := lipgloss.Height(panel), 9; got != want {
		t.Fatalf("session popup height = %d, want %d (selected row wrapped):\n%s", got, want, ansi.Strip(panel))
	}
}

func TestPopupFloatsOverConversationBackground(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 26
	m.layout()
	m.popup = &popupState{
		kind:  "sessions",
		title: "Sessions",
		items: []popupItem{{
			id:    "1234abcd",
			label: "Earlier conversation",
			desc:  "07-16 12:00 · test-model · 4 messages · 1234abcd",
		}},
	}
	m.popup.applyFilter("")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⚡ BoltCode") || !strings.Contains(view, "Sessions") {
		t.Fatalf("floating popup did not preserve the chat behind it:\n%s", view)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("overlay height = %d, want %d", len(lines), m.height)
	}
	for index, line := range lines {
		if got := ansi.StringWidth(line); got != m.width {
			t.Fatalf("overlay line %d width = %d, want %d", index, got, m.width)
		}
	}
}

func TestPopupTopAndBottomBordersHaveSameCellWidth(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 26
	m.layout()
	m.popup = &popupState{
		kind:  "commands",
		title: "Slash Commands",
		items: []popupItem{{
			id:    "/sessions",
			label: "/sessions",
			desc:  "browse conversations in this workspace · Ctrl+P",
		}},
	}
	m.popup.applyFilter("")
	lines := strings.Split(m.renderPopupPanel(), "\n")
	if len(lines) < 2 {
		t.Fatalf("popup panel has %d lines", len(lines))
	}
	topWidth := ansi.StringWidth(lines[0])
	bottomWidth := ansi.StringWidth(lines[len(lines)-1])
	if topWidth != bottomWidth {
		t.Fatalf("popup borders differ: top=%d bottom=%d\n%s", topWidth, bottomWidth, ansi.Strip(strings.Join(lines, "\n")))
	}
}

func TestOverlayBorderDoesNotShiftAtWideBackgroundCharacter(t *testing.T) {
	const width = 40
	baseLines := make([]string, 7)
	for index := range baseLines {
		baseLines[index] = strings.Repeat(".", width)
	}
	// The popup starts at cell 10. Put a two-cell CJK rune across cells 9-10
	// on the row where the bottom border will be composed.
	baseLines[4] = strings.Repeat(".", 9) + "你" + strings.Repeat(".", 29)
	popup := strings.Join([]string{
		"╭" + strings.Repeat("─", 18) + "╮",
		"│" + strings.Repeat(" ", 18) + "│",
		"╰" + strings.Repeat("─", 18) + "╯",
	}, "\n")
	overlay := strings.Split(ansi.Strip(overlayCentered(strings.Join(baseLines, "\n"), popup, width, len(baseLines))), "\n")
	topLeft := strings.Index(overlay[2], "╭")
	bottomLeft := strings.Index(overlay[4], "╰")
	if topLeft != 10 || bottomLeft != topLeft {
		t.Fatalf("popup border shifted: top-left=%d bottom-left=%d\n%s", topLeft, bottomLeft, strings.Join(overlay, "\n"))
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
	wantViewportHeight := m.height - (chatInputRows + 2) - 2 - 1 - chatConversationGapRows
	if m.viewport.Height != wantViewportHeight {
		t.Fatalf("viewport height = %d, want %d", m.viewport.Height, wantViewportHeight)
	}
}

func TestConversationKeepsGapAboveBottomChrome(t *testing.T) {
	m := newChatTestModel()
	m.width = 80
	m.height = 24
	m.layout()
	m.busy = false
	view := strings.Split(ansi.Strip(m.View()), "\n")
	if len(view) != m.height {
		t.Fatalf("chat view height = %d, want %d", len(view), m.height)
	}
	gapRow := 1 + m.viewport.Height
	if gapRow >= len(view) {
		t.Fatalf("gap row %d outside view with %d lines", gapRow, len(view))
	}
	if strings.TrimSpace(view[gapRow]) != "" {
		t.Fatalf("conversation gap row is not blank: %q", view[gapRow])
	}
	if gapRow+1 >= len(view) || !strings.Contains(view[gapRow+1], "ready") {
		t.Fatalf("status bar does not follow conversation gap:\n%s", strings.Join(view, "\n"))
	}
}

func TestModelSwitchNoticeStartsANewMessageBlock(t *testing.T) {
	m := newChatTestModel()
	m.lines = []string{"previous reply"}
	m.applyPopupSelection("models", popupItem{id: "deepseek-v4-pro"})
	if len(m.lines) != 3 || m.lines[1] != "" || !strings.Contains(ansi.Strip(m.lines[2]), "switched to") {
		t.Fatalf("model switch block = %#v", m.lines)
	}
}

func TestMouseWheelScrollsConversationViewport(t *testing.T) {
	m := newChatTestModel()
	m.width = 80
	m.height = 20
	m.layout()
	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.viewport.YOffset >= before {
		t.Fatalf("wheel up did not scroll: before=%d after=%d", before, m.viewport.YOffset)
	}
	m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.viewport.YOffset != before {
		t.Fatalf("wheel down did not restore offset: before=%d after=%d", before, m.viewport.YOffset)
	}
}

func TestMouseSelectionCopiesWideCharactersWithRightClick(t *testing.T) {
	m := newChatTestModel()
	m.width = 40
	m.height = 20
	m.layout()
	m.viewport.SetContent("ab中文cd")
	m.viewport.GotoTop()

	copied := ""
	m.copyText = func(text string) error {
		copied = text
		return nil
	}
	m.Update(tea.MouseMsg{X: 2, Y: 1, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 5, Y: 1, Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: 5, Y: 1, Type: tea.MouseRelease, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})

	if got := m.selectedConversationText(); got != "中文" {
		t.Fatalf("selected text = %q, want %q", got, "中文")
	}

	_, cmd := m.Update(tea.MouseMsg{X: 5, Y: 1, Type: tea.MouseRight, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("right-click did not schedule a clipboard write")
	}
	m.Update(cmd())
	if copied != "中文" {
		t.Fatalf("clipboard text = %q, want %q", copied, "中文")
	}
	if !strings.Contains(m.statusHint, "copied 2 characters") {
		t.Fatalf("copy status = %q", m.statusHint)
	}
	if m.selectedConversationText() != "" {
		t.Fatal("selection was not cleared after a successful copy")
	}
}

func TestMouseSelectionCopiesAcrossConversationLines(t *testing.T) {
	m := newChatTestModel()
	m.width = 40
	m.height = 20
	m.layout()
	m.viewport.SetContent("alpha\nbeta")
	m.viewport.GotoTop()

	m.Update(tea.MouseMsg{X: 2, Y: 1, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 1, Y: 2, Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: 1, Y: 2, Type: tea.MouseRelease, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})

	if got := m.selectedConversationText(); got != "pha\nbe" {
		t.Fatalf("multiline selection = %q, want %q", got, "pha\nbe")
	}
}

func TestChatViewHasFixedHeaderAndStatefulInputBorder(t *testing.T) {
	m := newChatTestModel()
	m.width = 100
	m.height = 24
	m.layout()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⚡ BoltCode") || !strings.Contains(view, "working") || !strings.Contains(view, "↕ 100%") {
		t.Fatalf("session header missing:\n%s", view)
	}
	if got := ansi.StringWidth(m.renderSessionHeader()); got != m.width {
		t.Fatalf("header width = %d, want %d", got, m.width)
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
