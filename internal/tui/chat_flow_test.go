package tui

// Tests for the behavior changes from the TUI optimization pass: /clear
// forwarding, confirm-channel safety, incremental wrapping, scroll position
// retention, prompt history, popup paging, and tick-chain gating.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

func TestClearForwardsToAgentAndResetsPanels(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.toolTimeline = []toolTimelineEntry{{id: "t1", name: "read_file", running: true}}
	m.setPlanItems([]agent.TodoItem{{Content: "pending task", Status: "in_progress"}})
	m.textarea.SetValue("/clear")

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if got := <-m.agentIn; got != "/clear" {
		t.Fatalf("/clear was not forwarded to the agent, channel has %q", got)
	}
	if len(m.lines) != 0 {
		t.Fatalf("local log survived /clear: %d lines", len(m.lines))
	}
	if len(m.toolTimeline) != 0 {
		t.Fatalf("tool timeline survived /clear: %+v", m.toolTimeline)
	}
	if m.planTotal != 0 || len(m.planItems) != 0 {
		t.Fatalf("plan panel survived /clear: %+v", m.planItems)
	}
}

func TestClearIsRejectedWhileBusy(t *testing.T) {
	m := newChatTestModel() // starts busy
	before := len(m.lines)
	m.textarea.SetValue("/clear")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.agentIn) != 0 {
		t.Fatal("/clear reached the agent mid-turn")
	}
	if len(m.lines) != before {
		t.Fatal("log was cleared while the agent was busy")
	}
	if !strings.Contains(m.statusHint, "wait for the current reply") {
		t.Fatalf("busy hint = %q", m.statusHint)
	}
}

func TestConfirmPopupAnswerReachesWaitingAgent(t *testing.T) {
	m := newChatTestModel()
	m.confirmPopup("rm -rf build/")
	got := make(chan bool, 1)
	go func() { got <- <-m.confirmCh }()
	m.handlePopupKey(tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case answer := <-got:
		if answer {
			t.Fatal("esc should answer false")
		}
	case <-time.After(time.Second):
		t.Fatal("confirm answer blocked the update loop")
	}
}

func TestConfirmAllowSetsAutoAllowBeforeAnswering(t *testing.T) {
	m := newChatTestModel()
	m.confirmPopup("make test")
	m.popup.selected = 1 // "Allow all"
	got := make(chan bool, 1)
	go func() { got <- <-m.confirmCh }()
	m.handlePopupKey(tea.KeyMsg{Type: tea.KeyEnter})
	select {
	case answer := <-got:
		if !answer {
			t.Fatal("allow should answer true")
		}
	case <-time.After(time.Second):
		t.Fatal("confirm answer blocked the update loop")
	}
	if !m.agent.AutoAllow {
		t.Fatal("allow-all did not set AutoAllow")
	}
}

func TestIncrementalWrapMatchesFullWrap(t *testing.T) {
	m := newChatTestModel()
	m.width = 60
	m.height = 30
	m.layout()
	long := strings.Repeat("word ", 40) + "end"
	for _, line := range []string{"short", long, "", "  indented " + long, "中文宽字符" + long} {
		m.Update(agentLineMsg{text: line})
	}
	want := wrapChatContent(strings.Join(m.lines, "\n"), m.width-3)
	if got := strings.Join(m.wrappedLines, "\n"); got != want {
		t.Fatalf("incremental wrap diverged from full wrap:\n got: %q\nwant: %q", got, want)
	}

	// A resize forces a full re-wrap; the invariant must still hold.
	m.width = 40
	m.layout()
	want = wrapChatContent(strings.Join(m.lines, "\n"), m.width-3)
	if got := strings.Join(m.wrappedLines, "\n"); got != want {
		t.Fatalf("re-wrap after resize diverged:\n got: %q\nwant: %q", got, want)
	}
}

func TestScrolledUpUserKeepsPositionAndGetsNewBelowBadge(t *testing.T) {
	m := newChatTestModel()
	m.width = 60
	m.height = 24
	m.layout()
	for i := 0; i < 60; i++ {
		m.Update(agentLineMsg{text: fmt.Sprintf("line %d", i)})
	}
	if !m.viewport.AtBottom() {
		t.Fatal("setup: expected the viewport pinned to the bottom")
	}
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.viewport.AtBottom() {
		t.Fatal("setup: wheel up did not scroll away from the bottom")
	}

	m.Update(agentLineMsg{text: "arrived while scrolled up"})
	if m.viewport.AtBottom() {
		t.Fatal("agent output yanked a scrolled-up user to the bottom")
	}
	if !m.newBelow {
		t.Fatal("new-below indicator was not set")
	}
	if bar := ansi.Strip(m.renderStatusBar()); !strings.Contains(bar, "new messages") {
		t.Fatalf("status bar missing the new-messages badge:\n%s", bar)
	}

	// Jumping to the bottom (empty input) clears the badge.
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.viewport.AtBottom() {
		t.Fatal("End did not jump to the bottom")
	}
	if m.newBelow {
		t.Fatal("new-below indicator survived reaching the bottom")
	}
}

func TestPromptHistoryRecallRestoresDraft(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	for _, prompt := range []string{"first task", "second task"} {
		m.textarea.SetValue(prompt)
		m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		<-m.agentIn
		m.Update(agentReadyMsg{})
	}
	m.textarea.SetValue("draft in progress")

	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "second task" {
		t.Fatalf("first recall = %q, want %q", got, "second task")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "first task" {
		t.Fatalf("second recall = %q, want %q", got, "first task")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp}) // clamps at the oldest entry
	if got := m.textarea.Value(); got != "first task" {
		t.Fatalf("oldest clamp = %q, want %q", got, "first task")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.textarea.Value(); got != "draft in progress" {
		t.Fatalf("draft restore = %q, want %q", got, "draft in progress")
	}
}

func TestPopupPgUpPgDnMovesOnePage(t *testing.T) {
	m := newChatTestModel()
	m.height = 24
	items := make([]popupItem, 0, 50)
	for i := 0; i < 50; i++ {
		items = append(items, popupItem{id: fmt.Sprintf("f%d", i), label: fmt.Sprintf("file%d", i)})
	}
	m.popup = &popupState{kind: "files", items: items, title: "Files"}
	m.popup.applyFilter("")

	page := popupVisibleRows(m.height)
	m.handlePopupKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.popup.selected != page {
		t.Fatalf("pgdown selected %d, want %d", m.popup.selected, page)
	}
	m.handlePopupKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.popup.selected != 0 {
		t.Fatalf("pgup selected %d, want 0", m.popup.selected)
	}
	for i := 0; i < 10; i++ {
		m.handlePopupKey(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if m.popup.selected != 49 {
		t.Fatalf("end clamp selected %d, want 49", m.popup.selected)
	}
}

func TestFailedSendDoesNotStickBusy(t *testing.T) {
	m := newChatTestModel()
	m.Update(agentReadyMsg{})
	m.agentIn <- "occupant" // fill the one-slot channel
	if ok := m.sendUserMessage("hello", true); ok {
		t.Fatal("send succeeded on a full channel")
	}
	if m.busy {
		t.Fatal("failed send left the UI in a busy state")
	}
}

func TestBusyTickChainsStopAndRestart(t *testing.T) {
	m := newChatTestModel() // starts busy with tick chains running
	if cmd := m.kickBusyTicks(); cmd != nil {
		t.Fatal("duplicate tick kick while chains were running")
	}
	m.Update(agentReadyMsg{})
	if cmd := m.kickBusyTicks(); cmd != nil {
		t.Fatal("idle kick restarted the tick chains")
	}
	m.sendUserMessage("go", true)
	<-m.agentIn
	if cmd := m.kickBusyTicks(); cmd == nil {
		t.Fatal("busy restart did not kick the tick chains")
	}
}

func TestStaleAsyncPopupResultIsIgnored(t *testing.T) {
	m := newChatTestModel()
	m.showFilePopup()
	m.popup = nil // user closed the popup before the scan finished
	m.Update(filesScannedMsg{seq: m.popupSeq, files: []string{"a.go"}})
	if m.popup != nil {
		t.Fatal("stale scan result reopened the popup")
	}

	m.showFilePopup()
	stale := m.popupSeq
	m.showFilePopup() // a newer scan supersedes the first
	m.Update(filesScannedMsg{seq: stale, files: []string{"old.go"}})
	if len(m.popup.items) != 0 {
		t.Fatalf("stale scan populated the new popup: %+v", m.popup.items)
	}
}

func TestContextBarThresholdColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	green := contextBar(50, 10)
	if !strings.Contains(green, chatGreenStyle.Render("█████")) {
		t.Fatal("50% bar should be green and half full")
	}
	yellow := contextBar(80, 10)
	if !strings.Contains(yellow, chatWarnStyle.Render("████████")) {
		t.Fatal("80% bar should be yellow")
	}
	red := contextBar(95, 10)
	if !strings.Contains(red, chatErrorStyle.Render("█████████")) {
		t.Fatal("95% bar should be red and nearly full")
	}
	if full := contextBar(100, 10); !strings.Contains(full, chatErrorStyle.Render("██████████")) {
		t.Fatal("100% bar should be red and full")
	}
	if got := ansi.StringWidth(contextBar(0, 10)); got != 10 {
		t.Fatalf("empty bar width = %d, want 10", got)
	}
	if got := ansi.StringWidth(contextBar(150, 10)); got != 10 {
		t.Fatalf("over-100 bar width = %d, want clamped to 10", got)
	}
}

func TestUserMessageLinesMultiLineAndCJK(t *testing.T) {
	lines := userMessageLines("第一行\nsecond line")
	if len(lines) != 4 {
		t.Fatalf("user block = %d lines, want 4 (header, 2 body, footer)", len(lines))
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"╭─ ◆ you", "│  第一行", "│  second line", "╰─"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("user block missing %q:\n%s", want, joined)
		}
	}
}

func TestModeBadgeColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	if got := modeBadge(agent.ModeBuild); !strings.Contains(got, chatGreenStyle.Render("BUILD")) {
		t.Fatalf("build badge = %q, want green BUILD", got)
	}
	if got := modeBadge(agent.ModePlan); !strings.Contains(got, chatWarnStyle.Render("PLAN")) {
		t.Fatalf("plan badge = %q, want yellow PLAN", got)
	}
	if got := modeBadge(agent.ModeAsk); !strings.Contains(got, chatBlueStyle.Render("ASK")) {
		t.Fatalf("ask badge = %q, want blue ASK", got)
	}
}

func TestWelcomeCardIsBoxed(t *testing.T) {
	m := NewChat(&agent.Agent{Config: agent.Config{Model: "test-model"}, ExecEnv: agent.ExecEnv{Workdir: t.TempDir()}})
	joined := ansi.Strip(strings.Join(m.lines, "\n"))
	for _, want := range []string{"╭─", "⚡ BoltCode Agent", "test-model", "Describe what you want", "╰─"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("welcome card missing %q:\n%s", want, joined)
		}
	}
}

func TestTimelineShowsEditDiff(t *testing.T) {
	m := newChatTestModel()
	m.width = 80
	m.height = 24
	m.layout()
	m.recordToolEvent(agent.Event{Kind: agent.EventToolStarted, ToolID: "e1", ToolName: "edit_file", Detail: "auth.go"})
	m.recordToolEvent(agent.Event{
		Kind:     agent.EventToolFinished,
		ToolID:   "e1",
		ToolName: "edit_file",
		Detail:   "auth.go",
		Diff:     "\x1b[38;5;196m  - old line\x1b[0m\n\x1b[38;5;78m  + new line\x1b[0m",
	})
	panel := ansi.Strip(m.renderToolTimeline())
	if !strings.Contains(panel, "- old line") || !strings.Contains(panel, "+ new line") {
		t.Fatalf("edit diff missing from collapsed timeline:\n%s", panel)
	}
	// Diff colors survive (not wrapped in a dim style).
	raw := m.renderToolTimeline()
	if !strings.Contains(raw, "\x1b[38;5;78m  + new line") {
		t.Fatalf("diff colors were overridden:\n%q", raw)
	}
}
