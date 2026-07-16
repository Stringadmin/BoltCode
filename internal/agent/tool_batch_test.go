package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParallelReadBatchClassificationIsConservative(t *testing.T) {
	readBatch := []ToolCall{{Name: "read_file"}, {Name: "search_code"}, {Name: "glob"}, {Name: "list_files"}}
	if !canParallelizeToolCalls(readBatch) {
		t.Fatal("pure local read batch should run in parallel")
	}
	for _, unsafe := range []string{"write_file", "run_command", "web_fetch", "git_status", "todo_write"} {
		calls := []ToolCall{{Name: "read_file"}, {Name: unsafe}}
		if canParallelizeToolCalls(calls) {
			t.Errorf("batch containing %q must remain serial", unsafe)
		}
	}
	if canParallelizeToolCalls([]ToolCall{{Name: "read_file"}}) {
		t.Fatal("one read call does not need parallel orchestration")
	}
}

func TestParallelReadBatchPreservesProviderToolOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{Workdir: dir, Stats: NewStats(), UIMode: true, EventFn: func(Event) {}}
	calls := []ToolCall{
		{ID: "call_second", Name: "read_file", Input: json.RawMessage(`{"path":"second.txt"}`)},
		{ID: "call_first", Name: "read_file", Input: json.RawMessage(`{"path":"first.txt"}`)},
	}
	executions := a.executeReadToolsParallel(calls)
	if len(executions) != 2 {
		t.Fatalf("executions = %d", len(executions))
	}
	if executions[0].result.ToolUseID != "call_second" || !strings.Contains(executions[0].result.Content, "second-content") {
		t.Fatalf("first ordered result = %+v", executions[0].result)
	}
	if executions[1].result.ToolUseID != "call_first" || !strings.Contains(executions[1].result.Content, "first-content") {
		t.Fatalf("second ordered result = %+v", executions[1].result)
	}
	_, _, _, tools, _, _ := a.Stats.Snapshot()
	if tools != 2 {
		t.Fatalf("parallel tool count = %d, want 2", tools)
	}
}

func TestParallelReadBatchPreservesErrorPairing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "exists.txt"), []byte("available"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{Workdir: dir, Stats: NewStats(), UIMode: true, EventFn: func(Event) {}}
	calls := []ToolCall{
		{ID: "call_missing", Name: "read_file", Input: json.RawMessage(`{"path":"missing.txt"}`)},
		{ID: "call_exists", Name: "read_file", Input: json.RawMessage(`{"path":"exists.txt"}`)},
	}

	executions := a.executeReadToolsParallel(calls)
	if len(executions) != len(calls) {
		t.Fatalf("executions = %d, want %d", len(executions), len(calls))
	}
	first, firstError := a.finishToolExecution(executions[0])
	second, secondError := a.finishToolExecution(executions[1])
	if !firstError || first["tool_use_id"] != "call_missing" {
		t.Fatalf("missing-file result lost its provider pairing: %#v", first)
	}
	if secondError || second["tool_use_id"] != "call_exists" {
		t.Fatalf("successful result lost its provider pairing: %#v", second)
	}
	if !strings.Contains(second["content"].(string), "available") {
		t.Fatalf("successful result content = %#v", second["content"])
	}
}
