package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeInitialUserMessageUsesRealRequest(t *testing.T) {
	got := composeInitialUserMessage([]string{"## Current Project\nGo CLI"}, "修复输入问题")
	if !strings.Contains(got, "## User request\n修复输入问题") {
		t.Fatalf("initial message missing user request: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "please greet") {
		t.Fatalf("initial TUI message still asks for a generated greeting: %q", got)
	}
	if got := composeInitialUserMessage(nil, "hello"); got != "hello" {
		t.Fatalf("message without context = %q", got)
	}
}

func TestNewTUISessionWaitsForRealInputBeforeAPI(t *testing.T) {
	a := &Agent{Workdir: t.TempDir(), Config: Config{Model: "test-model"}, UIMode: true, NoSpinner: true}
	calls := 0
	if err := a.Run(func() string {
		calls++
		return "exit"
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("input calls = %d, want 1", calls)
	}
	if a.Stats.Rounds != 0 {
		t.Fatalf("API rounds = %d, want 0 before first real request", a.Stats.Rounds)
	}
	if len(a.Messages) != 0 {
		t.Fatalf("generated greeting leaked into messages: %+v", a.Messages)
	}
}

func TestWaitForUserInputSkipsEmptyAndStopsOnExit(t *testing.T) {
	a := &Agent{}
	inputs := []string{"", "  ", "actual request"}
	i := 0
	got, ok := a.waitForUserInput(func() string {
		value := inputs[i]
		i++
		return value
	})
	if !ok || got != "actual request" {
		t.Fatalf("input = %q, %v", got, ok)
	}

	if got, ok := a.waitForUserInput(func() string { return "exit" }); ok || got != "" {
		t.Fatalf("exit input = %q, %v", got, ok)
	}
}

func TestWaitForRecoveryInputRetriesWithoutNewMessage(t *testing.T) {
	a := &Agent{}
	input, retry, ok := a.waitForRecoveryInput(func() string { return "/retry" })
	if !ok || !retry || input != "" {
		t.Fatalf("recovery = input %q retry=%v ok=%v", input, retry, ok)
	}

	input, retry, ok = a.waitForRecoveryInput(func() string { return "change direction" })
	if !ok || retry || input != "change direction" {
		t.Fatalf("recovery steering = input %q retry=%v ok=%v", input, retry, ok)
	}
}

func TestWaitForUserInputClearsCompletedPlan(t *testing.T) {
	var event Event
	a := &Agent{
		TodoList: []TodoItem{{Content: "finished", Status: "completed"}},
		EventFn:  func(received Event) { event = received },
	}
	input, ok := a.waitForUserInput(func() string { return "next task" })
	if !ok || input != "next task" {
		t.Fatalf("waitForUserInput = %q, %v", input, ok)
	}
	if len(a.TodoList) != 0 {
		t.Fatalf("completed plan survived: %+v", a.TodoList)
	}
	if event.Kind != EventPlanUpdated || event.PlanItems == nil || len(event.PlanItems) != 0 {
		t.Fatalf("plan clear event = %+v", event)
	}
}

func TestWaitForRecoveryRetryKeepsPlan(t *testing.T) {
	a := &Agent{TodoList: []TodoItem{{Content: "retry task", Status: "completed"}}}
	_, retry, ok := a.waitForRecoveryInput(func() string { return "/retry" })
	if !ok || !retry {
		t.Fatalf("retry response = retry %v, ok %v", retry, ok)
	}
	if len(a.TodoList) != 1 {
		t.Fatal("/retry cleared the existing plan")
	}
}

func TestDisplayToolPathMakesWorkspacePathRelative(t *testing.T) {
	workdir := `C:\workspace\BoltCode`
	if filepath.Separator == '/' {
		workdir = "/workspace/BoltCode"
	}
	a := &Agent{Workdir: workdir}
	got := a.displayToolPath(filepath.Join(workdir, "internal", "agent", "exec.go"))
	if got != "internal/agent/exec.go" {
		t.Fatalf("display path = %q", got)
	}
}

func TestWrapToolErrorKeepsDetailsReadable(t *testing.T) {
	message := "path F:/BoltCode/internal/agent/a-very-long-file-name.go escapes the working directory and cannot be read"
	lines := wrapToolError(message, 36, 4)
	if len(lines) < 2 || len(lines) > 4 {
		t.Fatalf("wrapped lines = %#v", lines)
	}
	if !strings.Contains(strings.Join(lines, " "), "working directory") {
		t.Fatalf("wrapped error lost useful detail: %#v", lines)
	}
}
