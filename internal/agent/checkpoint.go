package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxUndoCheckpoints = 20
	maxSnapshotBytes   = 4 << 20
)

type pathSnapshot struct {
	path    string
	existed bool
	isDir   bool
	mode    os.FileMode
	data    []byte
}

type changeCheckpoint struct {
	description string
	paths       []pathSnapshot
}

func (a *Agent) captureToolCheckpoint(call ToolCall) *changeCheckpoint {
	paths := checkpointPaths(call)
	if len(paths) == 0 {
		return nil
	}
	checkpoint := changeCheckpoint{description: checkpointDescription(call, paths)}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		full, err := safeJoin(a.Workdir, path)
		if err != nil || seen[full] {
			continue
		}
		seen[full] = true
		snapshot, err := capturePathSnapshot(full)
		if err != nil {
			// Checkpoints are a safety convenience, not a reason to reject an
			// otherwise valid tool. Large files and non-empty directories are
			// deliberately skipped rather than copied into memory.
			return nil
		}
		checkpoint.paths = append(checkpoint.paths, snapshot)
	}
	if len(checkpoint.paths) == 0 {
		return nil
	}
	return &checkpoint
}

func checkpointPaths(call ToolCall) []string {
	var input map[string]interface{}
	if json.Unmarshal(call.Input, &input) != nil {
		return nil
	}
	stringValue := func(key string) string {
		value, _ := input[key].(string)
		return strings.TrimSpace(value)
	}
	switch call.Name {
	case "write_file", "edit_file", "multi_edit", "delete_file":
		if path := stringValue("path"); path != "" {
			return []string{path}
		}
	case "rename_file":
		from, to := stringValue("from"), stringValue("to")
		if from != "" && to != "" {
			return []string{from, to}
		}
	}
	return nil
}

func checkpointDescription(call ToolCall, paths []string) string {
	if call.Name == "rename_file" && len(paths) == 2 {
		return fmt.Sprintf("rename %s → %s", paths[0], paths[1])
	}
	return fmt.Sprintf("%s %s", call.Name, strings.Join(paths, ", "))
}

func capturePathSnapshot(path string) (pathSnapshot, error) {
	snapshot := pathSnapshot{path: path}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	snapshot.existed = true
	snapshot.isDir = info.IsDir()
	snapshot.mode = info.Mode()
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return snapshot, err
		}
		if len(entries) != 0 {
			return snapshot, fmt.Errorf("non-empty directory checkpoints are not supported")
		}
		return snapshot, nil
	}
	if info.Size() > maxSnapshotBytes {
		return snapshot, fmt.Errorf("file exceeds checkpoint limit")
	}
	snapshot.data, err = os.ReadFile(path)
	return snapshot, err
}

func (a *Agent) pushCheckpoint(checkpoint changeCheckpoint) {
	a.undoStack = append(a.undoStack, checkpoint)
	if len(a.undoStack) > maxUndoCheckpoints {
		a.undoStack = append([]changeCheckpoint(nil), a.undoStack[len(a.undoStack)-maxUndoCheckpoints:]...)
	}
}

func (a *Agent) undoLastCheckpoint() (string, error) {
	if a.EffectiveMode() != ModeBuild {
		return "", fmt.Errorf("undo changes project files; switch to build mode first")
	}
	if len(a.undoStack) == 0 {
		return "", fmt.Errorf("no direct file changes available to undo")
	}
	checkpoint := a.undoStack[len(a.undoStack)-1]
	for index := len(checkpoint.paths) - 1; index >= 0; index-- {
		if err := restorePathSnapshot(checkpoint.paths[index]); err != nil {
			return "", fmt.Errorf("restore %s: %w", checkpoint.description, err)
		}
	}
	a.undoStack = a.undoStack[:len(a.undoStack)-1]
	return checkpoint.description, nil
}

func restorePathSnapshot(snapshot pathSnapshot) error {
	if err := os.RemoveAll(snapshot.path); err != nil {
		return err
	}
	if !snapshot.existed {
		return nil
	}
	if snapshot.isDir {
		return os.MkdirAll(snapshot.path, snapshot.mode.Perm())
	}
	if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(snapshot.path, snapshot.data, snapshot.mode.Perm())
}
