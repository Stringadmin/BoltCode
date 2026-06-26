package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// safeJoin resolves path against workdir and guarantees the result stays inside
// workdir. It rejects absolute paths and any ".." traversal so a tool call can
// never read or write outside the task's isolated worktree.
func safeJoin(workdir, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be relative to the working directory: %q", path)
	}
	full := filepath.Join(workdir, filepath.Clean(path))
	rel, err := filepath.Rel(workdir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the working directory", path)
	}
	return full, nil
}

// shellCommand wraps a command string for the host OS shell: cmd /c on Windows,
// bash -c elsewhere. This is what makes run_command portable.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", command)
	}
	return exec.Command("bash", "-c", command)
}

// ExecTool executes a tool call and returns the result string.
func (a *Agent) ExecTool(call ToolCall) ToolResult {
	// Most tools take a flat string map; todo_write and multi_edit have nested
	// input (arrays/objects), so handle those before the string-only unmarshal.
	switch call.Name {
	case "todo_write":
		return a.toolTodoWrite(call.Input)
	case "multi_edit":
		return a.toolMultiEdit(call.Input)
	}

	var input map[string]string
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return ToolResult{ToolUseID: call.ID, Content: fmt.Sprintf("malformed tool input: %v", err), IsError: true}
	}

	var result string
	var isErr bool

	switch call.Name {
	case "read_file":
		result, isErr = a.toolReadFile(input["path"])
	case "write_file":
		result, isErr = a.toolWriteFile(input["path"], input["content"])
	case "edit_file":
		result, isErr = a.toolEditFile(input["path"], input["old_string"], input["new_string"])
	case "search_code":
		result, isErr = a.toolSearchCode(input["pattern"], input["path"], input["glob"])
	case "run_command":
		result, isErr = a.toolRunCommand(input["command"])
	case "list_files":
		result, isErr = a.toolListFiles(input["path"])
	case "update_memory":
		result, isErr = a.toolUpdateMemory(input["key"], input["value"])
	default:
		result = "unknown tool: " + call.Name
		isErr = true
	}

	return ToolResult{ToolUseID: call.ID, Content: result, IsError: isErr}
}

func (a *Agent) toolReadFile(path string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err), true
	}
	return string(data), false
}

func (a *Agent) toolWriteFile(path, content string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("error creating dir: %v", err), true
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", path, err), true
	}
	return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), false
}

func (a *Agent) toolEditFile(path, oldStr, newStr string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err), true
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	switch count {
	case 0:
		return fmt.Sprintf("old_string not found in %s", path), true
	case 1:
		// unambiguous — apply the edit
	default:
		// Multiple matches: silently replacing only the first would risk
		// editing the wrong location without the AI noticing. Surface it so
		// the agent can provide more surrounding context to disambiguate.
		return fmt.Sprintf("old_string matches %d locations in %s — include more surrounding context so it is unique", count, path), true
	}
	updated := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", path, err), true
	}
	return fmt.Sprintf("edited %s", path), false
}

func (a *Agent) toolSearchCode(pattern, path, glob string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("invalid search pattern: %v", err), true
	}

	var matches []string
	truncated := false
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".boltcode":
				return filepath.SkipDir
			}
			return nil
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(a.Workdir, p)
		if relErr != nil {
			rel = p
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
				if len(matches) >= 500 {
					truncated = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Sprintf("search failed: %v", walkErr), true
	}
	if len(matches) == 0 {
		return "no matches found", false
	}
	result := strings.Join(matches, "\n")
	if len(result) > 8000 {
		result = result[:8000] + "\n... (truncated)"
	} else if truncated {
		result += "\n... (truncated at 500 matches)"
	}
	return result, false
}

func (a *Agent) toolRunCommand(command string) (string, bool) {
	// Safety: confirm before executing unless auto-allow is on
	if !a.AutoAllow && a.ConfirmFn != nil {
		if !a.ConfirmFn(command) {
			return "user denied this command", true
		}
	}
	cmd := shellCommand(command)
	cmd.Dir = a.Workdir
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	if err != nil {
		return fmt.Sprintf("%s\nexit: %v", result, err), true
	}
	return result, false
}

func (a *Agent) toolListFiles(path string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	const maxFiles = 500
	var files []string
	truncated := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".boltcode" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(a.Workdir, p)
		files = append(files, rel)
		return nil
	})
	result := strings.Join(files, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (listing truncated at %d files)", maxFiles)
	}
	return result, false
}

// toolUpdateMemory lets the AI persist project knowledge across sessions.
// key: one of "architecture", "build_cmd", "test_cmd", "language", "framework",
//      "file:<path>" (maps a file to its description), "preference", "decision"
// value: the information to remember
func (a *Agent) toolUpdateMemory(key, value string) (string, bool) {
	mem := a.LoadMemory()

	switch {
	case key == "architecture":
		mem.Architecture = value
	case key == "build_cmd":
		mem.BuildCmd = value
	case key == "test_cmd":
		mem.TestCmd = value
	case key == "language":
		mem.Language = value
	case key == "framework":
		mem.Framework = value
	case strings.HasPrefix(key, "file:"):
		file := strings.TrimPrefix(key, "file:")
		mem.FileMap[file] = value
	case key == "preference":
		mem.Preferences = append(mem.Preferences, value)
	case key == "decision":
		// value is expected as "topic: choice". Split on the first colon so
		// the decision is recorded with a distinct topic and choice rather
		// than duplicating the whole string into both fields.
		topic, choice := value, value
		if idx := strings.Index(value, ":"); idx >= 0 {
			topic = strings.TrimSpace(value[:idx])
			choice = strings.TrimSpace(value[idx+1:])
		}
		mem.Decisions = append(mem.Decisions, Decision{
			Topic: topic, Choice: choice, Date: mem.UpdatedAt.Format("2006-01-02"),
		})
	default:
		return "unknown memory key: " + key, true
	}

	a.SaveMemory(mem)
	return "memory updated: " + key, false
}

// toolTodoWrite replaces the session task plan. Each item has content + status
// (pending/in_progress/completed). An empty list clears it.
func (a *Agent) toolTodoWrite(raw json.RawMessage) ToolResult {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{ToolUseID: "", Content: fmt.Sprintf("malformed todo input: %v", err), IsError: true}
	}
	// Validate statuses.
	for i, t := range in.Todos {
		switch t.Status {
		case "pending", "in_progress", "completed":
		default:
			in.Todos[i].Status = "pending"
		}
	}
	a.TodoList = in.Todos
	if len(a.TodoList) == 0 {
		return ToolResult{Content: "todo list cleared", IsError: false}
	}
	done := 0
	for _, t := range a.TodoList {
		if t.Status == "completed" {
			done++
		}
	}
	return ToolResult{Content: fmt.Sprintf("todo list updated: %d/%d completed", done, len(a.TodoList)), IsError: false}
}

// toolMultiEdit applies several old→new string replacements to one file in a
// single call. Each old_string must be unique; the first non-matching edit
// aborts the whole operation (no partial writes) so the file stays consistent.
func (a *Agent) toolMultiEdit(raw json.RawMessage) ToolResult {
	var in struct {
		Path  string `json:"path"`
		Edits []struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("malformed multi_edit input: %v", err), IsError: true}
	}
	full, err := safeJoin(a.Workdir, in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error reading %s: %v", in.Path, err), IsError: true}
	}
	content := string(data)

	// Validate ALL edits before applying any, so a bad edit doesn't leave a
	// half-modified file.
	for i, e := range in.Edits {
		count := strings.Count(content, e.Old)
		if count == 0 {
			return ToolResult{Content: fmt.Sprintf("edit %d: old_string not found in %s", i+1, in.Path), IsError: true}
		}
		if count > 1 {
			return ToolResult{Content: fmt.Sprintf("edit %d: old_string matches %d locations in %s — include more context", i+1, count, in.Path), IsError: true}
		}
	}
	// Apply sequentially.
	for _, e := range in.Edits {
		content = strings.Replace(content, e.Old, e.New, 1)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return ToolResult{Content: fmt.Sprintf("error writing %s: %v", in.Path, err), IsError: true}
	}
	return ToolResult{Content: fmt.Sprintf("applied %d edits to %s", len(in.Edits), in.Path), IsError: false}
}
