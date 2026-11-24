package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin resolves path against workdir and guarantees the result stays inside
// workdir — including after symlink resolution. Relative paths are preferred,
// but models sometimes echo the absolute working-directory path from context;
// an absolute path is accepted only when it still resolves inside workdir.
//
// Symlinks are evaluated so an attacker (or a careless AI) can't plant a
// symlink inside the worktree pointing at /etc and then read or write through
// it. If the workdir doesn't exist (e.g. unit tests), only the string-based
// traversal check applies.
func safeJoin(workdir, path string) (string, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("resolving working directory: %w", err)
	}
	root = filepath.Clean(root)

	full := ""
	if filepath.IsAbs(path) {
		full = filepath.Clean(path)
	} else {
		full = filepath.Join(root, filepath.Clean(path))
	}

	// String-based containment check catches both ".." traversal and absolute
	// paths that point outside the workspace.
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the working directory", path)
	}

	// Resolve the workdir's real path. If it doesn't exist (unit tests use a
	// synthetic path), the string check above is all we can do.
	realWorkdir, err := filepath.EvalSymlinks(root)
	if err != nil {
		return full, nil
	}

	// Resolve the target's real path. If it exists, EvalSymlinks gives the
	// real destination directly. If it doesn't (e.g. write_file creating a new
	// file), walk up to the nearest existing ancestor, resolve that, and
	// rejoin the non-existent trailing components — so a symlink in a parent
	// dir is still caught.
	realFull := full
	if r, err := filepath.EvalSymlinks(full); err == nil {
		realFull = r
	} else if os.IsNotExist(err) {
		ancestor := full
		var tail []string
		for {
			if r, err := filepath.EvalSymlinks(ancestor); err == nil {
				realFull = filepath.Join(append([]string{r}, tail...)...)
				break
			}
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("path %q escapes the working directory: %w", path, err)
			}
			tail = append([]string{filepath.Base(ancestor)}, tail...)
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				// Reached filesystem root without finding an existing dir;
				// treat as an escape.
				return "", fmt.Errorf("path %q escapes the working directory", path)
			}
			ancestor = parent
		}
	} else {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}

	// Re-check the resolved path is still inside the resolved workdir.
	realRel, err := filepath.Rel(realWorkdir, realFull)
	if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the working directory via symlink", path)
	}
	return full, nil
}

// ExecTool executes a tool call and returns the result string.
func (a *Agent) ExecTool(call ToolCall) ToolResult {
	mode := a.EffectiveMode()
	if !ModeAllowsTool(mode, call.Name) {
		return ToolResult{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("tool %q is not available in %s mode; switch to build mode to perform project changes", call.Name, mode),
			IsError:   true,
		}
	}
	if a.structuredTurn {
		if lesson := validateToolInput(call); lesson.Kind != "" {
			return ToolResult{ToolUseID: call.ID, Content: lesson.Guidance(mode), IsError: true, lesson: lesson}
		}
	}
	checkpoint := a.captureToolCheckpoint(call)
	result := a.execToolUnchecked(call)
	result.Content = a.truncateToolOutput(result.Content)
	if !result.IsError && checkpoint != nil {
		a.pushCheckpoint(*checkpoint)
	}
	return result
}

func (a *Agent) execToolUnchecked(call ToolCall) ToolResult {
	// Most tools take a flat string map; todo_write and multi_edit have nested
	// input (arrays/objects), so handle those before the string-only unmarshal.
	// They still need call.ID so the returned ToolResult carries the correct
	// tool_use_id — the Anthropic API requires each tool_result to reference
	// the matching tool_use id, and an empty id causes a 400.
	switch call.Name {
	case "todo_write":
		return a.toolTodoWrite(call.ID, call.Input)
	case "multi_edit":
		return a.toolMultiEdit(call.ID, call.Input)
	}

	var input map[string]string
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("malformed tool input: %v", err), IsError: true}
	}

	var result string
	var isErr bool

	switch call.Name {
	case "read_file":
		result, isErr = a.toolReadFile(input["path"], input["offset"], input["limit"])
	case "write_file":
		result, isErr = a.toolWriteFile(input["path"], input["content"])
		// Preview the written content so file creation is visible as well.
		if !isErr {
			if preview := addedLinesPreview(input["content"], 6); preview != "" {
				return ToolResult{ToolUseID: call.ID, Content: result, IsError: false, Diff: preview}
			}
		}
	case "edit_file":
		result, isErr = a.toolEditFile(input["path"], input["old_string"], input["new_string"])
		// Compute a compact diff preview for the user (not sent to AI).
		if !isErr {
			diff := simpleDiff(input["old_string"], input["new_string"], 3)
			return ToolResult{ToolUseID: call.ID, Content: result, IsError: false, Diff: diff}
		}
	case "search_code":
		result, isErr = a.toolSearchCode(input["pattern"], input["path"], input["glob"])
	case "run_command":
		command := input["command"]
		result, isErr = a.toolRunCommand(command)
		if a.structuredTurn && isErr {
			if lesson := commandFailureLesson(command, result); lesson.Kind != "" {
				return ToolResult{ToolUseID: call.ID, Content: result + "\n" + lesson.Guidance(a.EffectiveMode()), IsError: true, lesson: lesson}
			}
		}
	case "list_files":
		result, isErr = a.toolListFiles(input["path"])
	case "update_memory":
		result, isErr = a.toolUpdateMemory(input["key"], input["value"])
	case "delete_file":
		result, isErr = a.toolDeleteFile(input["path"])
	case "rename_file":
		result, isErr = a.toolRenameFile(input["from"], input["to"])
	case "web_fetch":
		result, isErr = a.toolWebFetch(input["url"])
	case "git_status":
		result, isErr = a.toolGitStatus()
	case "git_diff":
		result, isErr = a.toolGitDiff(input["path"])
	case "git_commit":
		result, isErr = a.toolGitCommit(input["message"])
	case "get_diagnostics":
		result, isErr = a.toolGetDiagnostics(input["command"])
	case "glob":
		result, isErr = a.toolGlob(input["pattern"], input["path"])
	default:
		result = "unknown tool: " + call.Name
		isErr = true
	}

	return ToolResult{ToolUseID: call.ID, Content: result, IsError: isErr}
}
