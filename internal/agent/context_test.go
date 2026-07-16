package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelContextWindow(t *testing.T) {
	tests := []struct {
		model string
		min   int // must be at least this
	}{
		{"claude-sonnet-4-20250514", 100_000},
		{"glm-5.2", 100_000},
		{"unknown-model", 100_000},
	}
	for _, tt := range tests {
		if w := modelContextWindow(tt.model); w < tt.min {
			t.Errorf("window(%q) = %d, want >= %d", tt.model, w, tt.min)
		}
	}
}

func TestModelContextWindowEnvOverride(t *testing.T) {
	// BOLTCODE_CONTEXT_WINDOW takes precedence over the built-in table, so a
	// user with an unlisted/custom model isn't stuck with the 128k default.
	t.Setenv("BOLTCODE_CONTEXT_WINDOW", "999000")
	if w := modelContextWindow("some-custom-model"); w != 999000 {
		t.Errorf("env override = %d, want 999000", w)
	}
	// Override applies even to known models.
	if w := modelContextWindow("claude-sonnet-4-20250514"); w != 999000 {
		t.Errorf("env override for known model = %d, want 999000", w)
	}
	// A garbage value falls through to the table (not 0, which would break
	// compaction math).
	t.Setenv("BOLTCODE_CONTEXT_WINDOW", "not-a-number")
	if w := modelContextWindow("claude-sonnet-4-20250514"); w < 100_000 {
		t.Errorf("garbage env should fall through to table, got %d", w)
	}
}

func TestConfiguredContextWindowOverridesModelInference(t *testing.T) {
	a := &Agent{Config: Config{Model: "unknown-custom-model", ContextWindow: 512_000}}
	if got := a.ContextWindow(); got != 512_000 {
		t.Fatalf("context window = %d, want 512000", got)
	}
}

func TestParseContextWindow(t *testing.T) {
	tests := map[string]int{
		"128k":    128_000,
		"200000":  200_000,
		"1m":      1_000_000,
		"256_000": 256_000,
		"auto":    0,
		"-":       0,
	}
	for input, want := range tests {
		got, err := ParseContextWindow(input)
		if err != nil || got != want {
			t.Errorf("ParseContextWindow(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"abc", "2k", "20m"} {
		if _, err := ParseContextWindow(input); err == nil {
			t.Errorf("ParseContextWindow(%q) should fail", input)
		}
	}
}

func TestShouldCompact(t *testing.T) {
	window := 100_000
	if shouldCompact(50_000, window) {
		t.Error("50% should not compact")
	}
	if !shouldCompact(85_000, window) {
		t.Error("85% should compact (>=80%)")
	}
	if !shouldCompact(100_000, window) {
		t.Error("100% should compact")
	}
}

func TestContextBar(t *testing.T) {
	bar := contextBar(50_000, 100_000)
	// Should contain a percentage and token counts.
	if !strings.Contains(bar, "50%") {
		t.Errorf("bar missing 50%%: %s", bar)
	}
	if !strings.Contains(bar, "50k") {
		t.Errorf("bar missing 50k: %s", bar)
	}
	if !strings.Contains(bar, "100k") {
		t.Errorf("bar missing 100k: %s", bar)
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi there friend"},
	}
	// 11 + 15 = 26 chars / 3 ≈ 8 tokens.
	if n := estimateTokens(msgs); n <= 0 {
		t.Errorf("estimateTokens = %d, want > 0", n)
	}
}

func TestTodoWrite(t *testing.T) {
	a := &Agent{}
	in := `{"todos":[{"content":"step one","status":"completed"},{"content":"step two","status":"in_progress"},{"content":"step three","status":"pending"}]}`
	res := a.toolTodoWrite("tu_1", []byte(in))
	if res.IsError {
		t.Fatalf("todo_write failed: %s", res.Content)
	}
	// The tool_use_id must be echoed back so the API can match the result.
	if res.ToolUseID != "tu_1" {
		t.Errorf("tool_use_id = %q, want %q", res.ToolUseID, "tu_1")
	}
	if len(a.TodoList) != 3 {
		t.Fatalf("todo list len = %d", len(a.TodoList))
	}
	if a.TodoList[0].Status != "completed" {
		t.Errorf("first item status = %q", a.TodoList[0].Status)
	}
	// progress line reports 1/3 done.
	line := a.todoProgressLine()
	if !strings.Contains(line, "1/3") {
		t.Errorf("progress line: %s", line)
	}
}

func TestTodoWriteBadStatusDefaults(t *testing.T) {
	a := &Agent{}
	in := `{"todos":[{"content":"x","status":"bogus"}]}`
	a.toolTodoWrite("tu_2", []byte(in))
	if a.TodoList[0].Status != "pending" {
		t.Errorf("bad status should default to pending, got %q", a.TodoList[0].Status)
	}
}

func TestMultiEditAppliesAll(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Workdir: dir}
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("foo\nbar\nbaz\n"), 0o644)

	in := `{"path":"f.txt","edits":[{"old_string":"foo","new_string":"FOO"},{"old_string":"baz","new_string":"BAZ"}]}`
	res := a.toolMultiEdit("me_1", []byte(in))
	if res.IsError {
		t.Fatalf("multi_edit failed: %s", res.Content)
	}
	if res.ToolUseID != "me_1" {
		t.Errorf("tool_use_id = %q, want %q", res.ToolUseID, "me_1")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "FOO\nbar\nBAZ\n" {
		t.Errorf("result = %q", string(data))
	}
}

func TestMultiEditAbortsOnBadEdit(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Workdir: dir}
	path := filepath.Join(dir, "f.txt")
	original := "foo\nbar\n"
	os.WriteFile(path, []byte(original), 0o644)

	// Second edit's old_string doesn't exist — whole op must abort, file untouched.
	in := `{"path":"f.txt","edits":[{"old_string":"foo","new_string":"FOO"},{"old_string":"nope","new_string":"X"}]}`
	res := a.toolMultiEdit("me_2", []byte(in))
	if !res.IsError {
		t.Fatal("expected abort error")
	}
	if res.ToolUseID != "me_2" {
		t.Errorf("tool_use_id = %q, want %q", res.ToolUseID, "me_2")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("file modified despite abort: %q", string(data))
	}
}

func TestMultiEditRejectsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Workdir: dir}
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("dup\ndup\n"), 0o644)

	in := `{"path":"f.txt","edits":[{"old_string":"dup","new_string":"X"}]}`
	res := a.toolMultiEdit("me_3", []byte(in))
	if !res.IsError {
		t.Fatal("expected ambiguous-match error")
	}
	if res.ToolUseID != "me_3" {
		t.Errorf("tool_use_id = %q, want %q", res.ToolUseID, "me_3")
	}
}
