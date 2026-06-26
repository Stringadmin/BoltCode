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
	res := a.toolTodoWrite([]byte(in))
	if res.IsError {
		t.Fatalf("todo_write failed: %s", res.Content)
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
	a.toolTodoWrite([]byte(in))
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
	res := a.toolMultiEdit([]byte(in))
	if res.IsError {
		t.Fatalf("multi_edit failed: %s", res.Content)
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
	res := a.toolMultiEdit([]byte(in))
	if !res.IsError {
		t.Fatal("expected abort error")
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
	res := a.toolMultiEdit([]byte(in))
	if !res.IsError {
		t.Fatal("expected ambiguous-match error")
	}
}
