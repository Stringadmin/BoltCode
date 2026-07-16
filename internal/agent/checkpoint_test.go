package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUndoRestoresWriteAndEditToolsInOrder(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Workdir: dir}
	write := a.ExecTool(ToolCall{ID: "write", Name: "write_file", Input: json.RawMessage(`{"path":"note.txt","content":"one"}`)})
	if write.IsError {
		t.Fatal(write.Content)
	}
	edit := a.ExecTool(ToolCall{ID: "edit", Name: "edit_file", Input: json.RawMessage(`{"path":"note.txt","old_string":"one","new_string":"two"}`)})
	if edit.IsError {
		t.Fatal(edit.Content)
	}
	if len(a.undoStack) != 2 {
		t.Fatalf("undo stack = %d, want 2", len(a.undoStack))
	}
	if _, err := a.undoLastCheckpoint(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil || string(data) != "one" {
		t.Fatalf("first undo content = %q, err=%v", data, err)
	}
	if _, err := a.undoLastCheckpoint(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("second undo should remove newly written file: %v", err)
	}
}

func TestUndoRenameRestoresBothPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "from.txt"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "to.txt"), []byte("destination"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{Workdir: dir}
	result := a.ExecTool(ToolCall{ID: "rename", Name: "rename_file", Input: json.RawMessage(`{"from":"from.txt","to":"to.txt"}`)})
	if result.IsError {
		t.Fatal(result.Content)
	}
	if _, err := a.undoLastCheckpoint(); err != nil {
		t.Fatal(err)
	}
	from, fromErr := os.ReadFile(filepath.Join(dir, "from.txt"))
	to, toErr := os.ReadFile(filepath.Join(dir, "to.txt"))
	if fromErr != nil || toErr != nil || string(from) != "source" || string(to) != "destination" {
		t.Fatalf("rename undo: from=%q (%v), to=%q (%v)", from, fromErr, to, toErr)
	}
}

func TestFailedToolDoesNotCreateCheckpointAndReadonlyModeCannotUndo(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{Workdir: dir}
	failed := a.ExecTool(ToolCall{ID: "edit", Name: "edit_file", Input: json.RawMessage(`{"path":"missing.txt","old_string":"x","new_string":"y"}`)})
	if !failed.IsError || len(a.undoStack) != 0 {
		t.Fatalf("failed tool checkpointed: result=%+v stack=%d", failed, len(a.undoStack))
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := a.ExecTool(ToolCall{ID: "edit2", Name: "edit_file", Input: json.RawMessage(`{"path":"file.txt","old_string":"before","new_string":"after"}`)})
	if result.IsError {
		t.Fatal(result.Content)
	}
	a.SetMode(ModePlan)
	if _, err := a.undoLastCheckpoint(); err == nil {
		t.Fatal("plan mode should not undo project files")
	}
}
