package agent

// Tests for the edit-diff preview pipeline and dimmed thinking display.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestExecEditFileProducesDiff(t *testing.T) {
	a := newAgentInTemp(t)
	writeFile(t, filepath.Join(a.Workdir, "a.go"), "package x\n\nfunc old() {}\n")

	input, _ := json.Marshal(map[string]string{"path": "a.go", "old_string": "func old() {}", "new_string": "func new() {}"})
	result := a.ExecTool(ToolCall{ID: "e1", Name: "edit_file", Input: input})
	if result.IsError {
		t.Fatalf("edit failed: %s", result.Content)
	}
	if !strings.Contains(result.Diff, "- func old() {}") || !strings.Contains(result.Diff, "+ func new() {}") {
		t.Fatalf("diff missing before/after lines:\n%s", result.Diff)
	}
}

func TestMultiEditProducesCombinedDiff(t *testing.T) {
	a := newAgentInTemp(t)
	writeFile(t, filepath.Join(a.Workdir, "m.go"), "alpha\nbeta\ngamma\n")

	input := json.RawMessage(`{"path":"m.go","edits":[{"old_string":"alpha","new_string":"ALPHA"},{"old_string":"gamma","new_string":"GAMMA"}]}`)
	result := a.ExecTool(ToolCall{ID: "m1", Name: "multi_edit", Input: input})
	if result.IsError {
		t.Fatalf("multi_edit failed: %s", result.Content)
	}
	for _, want := range []string{"- alpha", "+ ALPHA", "- gamma", "+ GAMMA"} {
		if !strings.Contains(result.Diff, want) {
			t.Fatalf("combined diff missing %q:\n%s", want, result.Diff)
		}
	}
}

func TestWriteFileProducesAddedPreview(t *testing.T) {
	a := newAgentInTemp(t)
	content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	input, _ := json.Marshal(map[string]string{"path": "main.go", "content": content})
	result := a.ExecTool(ToolCall{ID: "w1", Name: "write_file", Input: input})
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}
	if !strings.Contains(result.Diff, "+ package main") {
		t.Fatalf("write preview missing added lines:\n%s", result.Diff)
	}
}

func TestAddedLinesPreviewCapsAndSkipsEmpty(t *testing.T) {
	if got := addedLinesPreview("", 6); got != "" {
		t.Fatalf("empty content preview = %q, want empty", got)
	}
	long := strings.Repeat("line\n", 20)
	got := addedLinesPreview(long, 3)
	if strings.Count(got, "+ ") != 3 || !strings.Contains(got, "…") {
		t.Fatalf("preview not capped at 3 lines:\n%s", got)
	}
}

func TestAnthropicThinkingDeltaPrintsDimmed(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weigh the options"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"the answer"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		"",
	}, "\n")

	a := &Agent{}
	out := captureStdout(t, func() {
		if _, _, err := a.parseStream(strings.NewReader(stream)); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, ansiDarkGray+"weigh the options"+ansiReset) {
		t.Fatalf("thinking not printed dimmed:\n%q", out)
	}
	if !strings.Contains(out, "the answer") {
		t.Fatalf("assistant text missing from output:\n%q", out)
	}
}

func TestOpenAIReasoningContentPrintsDimmedBeforeText(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"first reason it through"}}]}`,
		`data: {"choices":[{"delta":{"content":"final answer"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	a := &Agent{}
	out := captureStdout(t, func() {
		if _, _, err := a.parseOpenAIStream(strings.NewReader(stream)); err != nil {
			t.Fatal(err)
		}
	})
	dim := strings.Index(out, ansiDarkGray+"first reason it through"+ansiReset)
	if dim < 0 {
		t.Fatalf("reasoning content not printed dimmed:\n%q", out)
	}
	if answer := strings.Index(out, "final answer"); answer < 0 || answer < dim {
		t.Fatalf("reasoning should print before the answer:\n%q", out)
	}
}

func TestFinishToolExecutionCarriesDiff(t *testing.T) {
	a := newAgentInTemp(t)
	writeFile(t, filepath.Join(a.Workdir, "a.go"), "one\n")
	input, _ := json.Marshal(map[string]string{"path": "a.go", "old_string": "one", "new_string": "two"})
	executions := []toolExecution{{
		call:    ToolCall{ID: "e1", Name: "edit_file", Input: input},
		started: a.toolEvent(ToolCall{ID: "e1", Name: "edit_file"}),
	}}
	executions[0].result = a.ExecTool(executions[0].call)

	var events []Event
	a.EventFn = func(event Event) { events = append(events, event) }
	_, _ = a.finishToolExecution(executions[0])

	if len(events) == 0 || events[len(events)-1].Diff == "" {
		t.Fatal("finished event did not carry the edit diff")
	}
}
