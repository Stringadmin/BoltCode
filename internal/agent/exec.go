package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// commandTimeout caps how long a single run_command tool call may run. A hung
// build or an interactive prompt would otherwise block the agent session
// forever (there was no timeout at all before). 10 minutes is generous for
// typical build/test commands while still bounding the worst case.
const commandTimeout = 10 * time.Minute

// gitCommandTimeout bounds built-in git tools separately from arbitrary shell
// commands. More importantly, runGit ties them to the agent session context so
// leaving the TUI cannot leave a git child process (and the agent goroutine)
// behind.
const gitCommandTimeout = 2 * time.Minute

// secretEnvVars are env-var names known to carry LLM/BoltCode credentials.
// They are stripped from the environment of every run_command subprocess so an
// AI-spawned command (or a compromised dependency) can't exfiltrate the user's
// API keys via `env`, `printenv`, or reading /proc/self/environ.
var secretEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"CLAUDE_API_KEY",
	"BOLTCODE_API_KEY",
	"VOLC_API_KEY",
	"ARK_API_KEY",
}

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

// shellName returns the shell program used to run command strings. On Unix it
// prefers bash but falls back to sh if bash is not available, so the tool
// works inside minimal containers (Alpine, distroless) that only ship sh.
func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}

// shellArgs returns the argument list (after the program name) for the host
// shell so a command string is executed as a single unit.
func shellArgs(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/c", command}
	}
	return []string{"-c", command}
}

// ExecTool executes a tool call and returns the result string.
func (a *Agent) ExecTool(call ToolCall) ToolResult {
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
		result, isErr = a.toolRunCommand(input["command"])
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

func (a *Agent) toolReadFile(path, offsetStr, limitStr string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err), true
	}
	lines := strings.Split(string(data), "\n")

	// Parse offset (1-based line number to start from).
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n > 0 {
			offset = n - 1 // convert to 0-based
		}
	}
	if offset >= len(lines) {
		offset = 0
	}

	// Parse limit (max number of lines to read).
	limit := 0
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	// Build output with line numbers so the AI knows which lines it's reading.
	var b strings.Builder
	for i := offset; i < end; i++ {
		fmt.Fprintf(&b, "%5d: %s\n", i+1, lines[i])
	}
	totalLines := len(lines)
	if end < totalLines {
		fmt.Fprintf(&b, "... (%d more lines, use offset=%d to continue)", totalLines-end, end+1)
	}
	return b.String(), false
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

func (a *Agent) toolDeleteFile(path string) (string, bool) {
	full, err := safeJoin(a.Workdir, path)
	if err != nil {
		return err.Error(), true
	}
	fi, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("path not found: %s", path), true
		}
		return fmt.Sprintf("error accessing %s: %v", path, err), true
	}
	if fi.IsDir() {
		// Only allow deleting empty directories.
		entries, err := os.ReadDir(full)
		if err != nil {
			return fmt.Sprintf("error reading directory %s: %v", path, err), true
		}
		if len(entries) > 0 {
			return fmt.Sprintf("cannot delete non-empty directory %s — remove files inside first or use run_command", path), true
		}
		if err := os.Remove(full); err != nil {
			return fmt.Sprintf("error removing directory %s: %v", path, err), true
		}
		return fmt.Sprintf("deleted empty directory %s", path), false
	}
	if err := os.Remove(full); err != nil {
		return fmt.Sprintf("error deleting %s: %v", path, err), true
	}
	return fmt.Sprintf("deleted %s", path), false
}

func (a *Agent) toolRenameFile(from, to string) (string, bool) {
	fullFrom, err := safeJoin(a.Workdir, from)
	if err != nil {
		return err.Error(), true
	}
	fullTo, err := safeJoin(a.Workdir, to)
	if err != nil {
		return err.Error(), true
	}
	// Check source exists.
	if _, err := os.Stat(fullFrom); err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("source not found: %s", from), true
		}
		return fmt.Sprintf("error accessing source %s: %v", from, err), true
	}
	// Create parent directories of destination.
	if err := os.MkdirAll(filepath.Dir(fullTo), 0o755); err != nil {
		return fmt.Sprintf("error creating destination directory: %v", err), true
	}
	if err := os.Rename(fullFrom, fullTo); err != nil {
		// os.Rename can fail across filesystem boundaries on Unix.
		// Fall back to copy + delete.
		return a.copyThenDelete(fullFrom, fullTo, from, to)
	}
	return fmt.Sprintf("renamed %s → %s", from, to), false
}

func (a *Agent) copyThenDelete(src, dst, from, to string) (string, bool) {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Sprintf("error reading %s for cross-device move: %v", from, err), true
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Sprintf("error writing %s during cross-device move: %v", to, err), true
	}
	if err := os.Remove(src); err != nil {
		return fmt.Sprintf("moved content but could not remove %s: %v", from, err), true
	}
	return fmt.Sprintf("renamed %s → %s (cross-device)", from, to), false
}

func (a *Agent) toolWebFetch(url string) (string, bool) {
	if url == "" {
		return "url is required", true
	}
	client := &http.Client{Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(a.context(), 15*time.Second)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Sprintf("invalid url: %v", err), true
	}
	req.Header.Set("User-Agent", "BoltCode/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("error fetching %s: %v", url, err), true
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Sprintf("HTTP %d fetching %s", resp.StatusCode, url), true
	}
	// Limit to 500KB to avoid blowing the context window.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500<<10))
	if err != nil {
		return fmt.Sprintf("error reading response: %v", err), true
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/html") {
		// Strip basic HTML for readability. A simple approach: remove tags.
		text := stripHTML(string(body))
		if len(text) > 8000 {
			text = text[:8000] + "\n... (truncated)"
		}
		return fmt.Sprintf("URL: %s\nContent-Type: %s\n\n%s", url, contentType, text), false
	}
	text := string(body)
	if len(text) > 8000 {
		text = text[:8000] + "\n... (truncated)"
	}
	return fmt.Sprintf("URL: %s\nContent-Type: %s\n\n%s", url, contentType, text), false
}

// stripHTML removes HTML tags for basic readability of fetched pages.
func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	// Collapse multiple whitespace runs.
	result := out.String()
	lines := strings.Split(result, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func (a *Agent) toolGitStatus() (string, bool) {
	// Use git directly in the workdir.
	out, err := a.runGit("status", "--short")
	if err != nil {
		return fmt.Sprintf("git status failed: %v\n%s", err, strings.TrimSpace(string(out))), true
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "working tree clean", false
	}
	return result, false
}

func (a *Agent) toolGitDiff(path string) (string, bool) {
	args := []string{"diff"}
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		args = append(args, "--", full)
	}
	out, err := a.runGit(args...)
	if err != nil {
		return fmt.Sprintf("git diff failed: %v\n%s", err, strings.TrimSpace(string(out))), true
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "no changes", false
	}
	if len(result) > 8000 {
		result = result[:8000] + "\n... (truncated)"
	}
	return result, false
}

func (a *Agent) toolGitCommit(message string) (string, bool) {
	if message == "" {
		return "commit message is required", true
	}
	// Stage all.
	if out, err := a.runGit("add", "-A"); err != nil {
		return fmt.Sprintf("git add failed: %v\n%s", err, strings.TrimSpace(string(out))), true
	}
	// Commit.
	out, err := a.runGit("commit", "-m", message)
	if err != nil {
		return fmt.Sprintf("git commit failed: %v\n%s", err, strings.TrimSpace(string(out))), true
	}
	return strings.TrimSpace(string(out)), false
}

func (a *Agent) runGit(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(a.context(), gitCommandTimeout)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = a.Workdir
	cmd.Env = gitEnv()
	return cmd.CombinedOutput()
}

// gitEnv returns a clean environment for git subprocesses with
// GIT_TERMINAL_PROMPT=0 so git fails fast instead of hanging.
func gitEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			out = append(out, kv)
		}
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	return out
}

// toolGetDiagnostics runs a diagnostic command (lint/type-check/vet) and returns
// the output. If a specific command is given it runs that; otherwise it detects
// the project type from files in the workdir and picks an appropriate command.
func (a *Agent) toolGetDiagnostics(command string) (string, bool) {
	cmd := command
	if cmd == "" {
		cmd = a.detectDiagnosticCmd()
	}
	if cmd == "" {
		return "no diagnostic command configured — set one with update_memory (key: test_cmd)", false
	}
	ctx, cancel := context.WithTimeout(a.context(), 2*time.Minute)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	prog := shellName()
	args := shellArgs(cmd)
	child := exec.CommandContext(ctx, prog, args...)
	child.Dir = a.Workdir
	child.Env = scrubEnv(os.Environ(), a.Config.APIKey)
	out, err := child.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result + "\nexit: diagnostic timed out after 2m", true
	}
	if err != nil {
		if result == "" {
			result = "(no output)"
		}
		return result + fmt.Sprintf("\nexit code: %v", err), true
	}
	if result == "" {
		return "diagnostics passed — no issues found", false
	}
	return result, false
}

// detectDiagnosticCmd probes the workdir for common project types and returns an
// appropriate diagnostic command. It checks memory first, then falls back to
// file-based detection.
func (a *Agent) detectDiagnosticCmd() string {
	// Check memory for a user-configured test/build command.
	mem := a.LoadMemory()
	if mem.TestCmd != "" {
		return mem.TestCmd
	}
	if mem.BuildCmd != "" {
		return mem.BuildCmd
	}
	// File-based detection.
	if a.fileExistsAt(a.Workdir, "go.mod") {
		return "go vet ./..."
	}
	if a.fileExistsAt(a.Workdir, "package.json") {
		return "npm run check 2>&1 || tsc --noEmit 2>&1"
	}
	if a.fileExistsAt(a.Workdir, "Cargo.toml") {
		return "cargo check 2>&1"
	}
	if a.fileExistsAt(a.Workdir, "pyproject.toml") || a.fileExistsAt(a.Workdir, "requirements.txt") {
		return "python -m py_compile" // minimal syntax check
	}
	return ""
}

func (a *Agent) fileExistsAt(root, name string) bool {
	p := filepath.Join(root, name)
	_, err := os.Stat(p)
	return err == nil
}

// destructivePatterns are commands so dangerous that they require explicit
// confirmation EVEN when AutoAllow is on. A blanket "allow all" shouldn't let
// the AI run `rm -rf /` or force-push to main without a second look. The
// patterns are intentionally conservative (match the obvious catastrophic
// forms) — an adversarial AI could obfuscate, but the goal is to catch the
// common foot-gun, not to be a sandbox.
var destructivePatterns = []*regexp.Regexp{
	// rm with a recursive flag targeting an absolute root (/...), home (~/...),
	// or $HOME/$OLDPWD. A scoped `rm -rf build/` (relative) does NOT match —
	// only nukes aimed at root/home do. `sudo rm -rf /` matches too because the
	// pattern keys on "rm -rf <root|home>" anywhere in the string.
	regexp.MustCompile(`\brm\s+-\w*r\w*\s+(/|~|\$HOME|\$OLDPWD)`),
	// force-push to shared branches (main/master/trunk/develop)
	regexp.MustCompile(`\bgit\s+push\s+(-f|--force|--force-with-lease)\b.*\b(main|master|trunk|develop)\b`),
	// fork bomb
	regexp.MustCompile(`:\(\)\s*\{\s*:\|\s*:&\s*\}\s*;:`),
	// overwrite a whole disk / device
	regexp.MustCompile(`\bdd\b.*\bof=/dev/`),
	// format a filesystem
	regexp.MustCompile(`\bmkfs\b`),
	// recursive chmod 777 from root/home
	regexp.MustCompile(`\bchmod\s+-R\s+777\b.*\s(/|~|\$HOME)`),
}

// isDestructive reports whether command matches a known catastrophic pattern.
func isDestructive(command string) bool {
	for _, re := range destructivePatterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

func (a *Agent) toolRunCommand(command string) (string, bool) {
	// Safety: confirm before executing unless auto-allow is on. Destructive
	// commands (rm -rf /, force-push to main, …) are ALWAYS confirmed, even
	// under AutoAllow — a blanket "allow all" must not silently nuke the repo.
	needsConfirm := !a.AutoAllow || isDestructive(command)
	if needsConfirm && a.ConfirmFn != nil {
		if !a.ConfirmFn(command) {
			return "user denied this command", true
		}
	}
	ctx, cancel := context.WithTimeout(a.context(), commandTimeout)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	cmd := exec.CommandContext(ctx, shellName(), shellArgs(command)...)
	cmd.Dir = a.Workdir
	// Scrub credentials from the child environment so an AI-run command can't
	// exfiltrate the user's API keys. We keep the rest of the env (PATH, HOME,
	// etc.) so legitimate build/test commands still work.
	cmd.Env = scrubEnv(os.Environ(), a.Config.APIKey)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("%s\nexit: command timed out after %s", result, commandTimeout), true
	}
	if ctx.Err() == context.Canceled {
		return "command interrupted", true
	}
	if err != nil {
		return fmt.Sprintf("%s\nexit: %v", result, err), true
	}
	return result, false
}

// scrubEnv returns a copy of env with known secret-bearing vars removed, plus
// any var whose value contains apiKey (so the agent's own key is never passed
// through regardless of which env var name it lives under).
func scrubEnv(env []string, apiKey string) []string {
	secretNames := make(map[string]struct{}, len(secretEnvVars))
	for _, n := range secretEnvVars {
		secretNames[n] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, bad := secretNames[name]; bad {
			continue
		}
		if apiKey != "" && strings.Contains(kv, apiKey) {
			continue
		}
		out = append(out, kv)
	}
	return out
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

// toolGlob finds files matching a glob pattern. Supports ** for recursive
// directory matching (e.g. "**/*.go", "src/**/*.ts"). Excludes .git,
// node_modules, and .boltcode directories from results.
func (a *Agent) toolGlob(pattern, path string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	regex, err := globToRegex(pattern)
	if err != nil {
		return fmt.Sprintf("invalid pattern %q: %v", pattern, err), true
	}
	const maxFiles = 500
	var matches []string
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
		if len(matches) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(a.Workdir, p)
		rel = filepath.ToSlash(rel)
		if regex.MatchString(rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if len(matches) == 0 {
		return "no files matched pattern: " + pattern, false
	}
	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (results truncated at %d files)", maxFiles)
	}
	return result, false
}

// globToRegex converts a glob pattern to a regular expression. Supports:
//   - ** : matches any sequence of characters including path separators
//   - *  : matches any sequence of non-separator characters
//   - ?  : matches any single non-separator character
//   - .  : literal dot (escaped)
func globToRegex(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(pattern)
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch {
		case i+1 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*':
			b.WriteString(".*")
			i += 2
			// Skip optional trailing slash after **
			if i < len(pattern) && pattern[i] == '/' {
				i++
			}
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		case pattern[i] == '.':
			b.WriteString("\\.")
			i++
		default:
			b.WriteByte(pattern[i])
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// toolUpdateMemory lets the AI persist project knowledge across sessions.
// key: one of "architecture", "build_cmd", "test_cmd", "language", "framework",
//
//	"file:<path>" (maps a file to its description), "preference", "decision"
//
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

	if err := a.SaveMemory(mem); err != nil {
		return fmt.Sprintf("error saving memory: %v", err), true
	}
	return "memory updated: " + key, false
}

// toolTodoWrite replaces the session task plan. Each item has content + status
// (pending/in_progress/completed). An empty list clears it.
func (a *Agent) toolTodoWrite(toolUseID string, raw json.RawMessage) ToolResult {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("malformed todo input: %v", err), IsError: true}
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
		return ToolResult{ToolUseID: toolUseID, Content: "todo list cleared", IsError: false}
	}
	done := 0
	for _, t := range a.TodoList {
		if t.Status == "completed" {
			done++
		}
	}
	return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("todo list updated: %d/%d completed", done, len(a.TodoList)), IsError: false}
}

// toolMultiEdit applies several old→new string replacements to one file in a
// single call. Each old_string must be unique; the first non-matching edit
// aborts the whole operation (no partial writes) so the file stays consistent.
func (a *Agent) toolMultiEdit(toolUseID string, raw json.RawMessage) ToolResult {
	var in struct {
		Path  string `json:"path"`
		Edits []struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("malformed multi_edit input: %v", err), IsError: true}
	}
	full, err := safeJoin(a.Workdir, in.Path)
	if err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: err.Error(), IsError: true}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("error reading %s: %v", in.Path, err), IsError: true}
	}
	content := string(data)

	// Validate ALL edits before applying any, so a bad edit doesn't leave a
	// half-modified file.
	for i, e := range in.Edits {
		count := strings.Count(content, e.Old)
		if count == 0 {
			return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("edit %d: old_string not found in %s", i+1, in.Path), IsError: true}
		}
		if count > 1 {
			return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("edit %d: old_string matches %d locations in %s — include more context", i+1, count, in.Path), IsError: true}
		}
	}
	// Apply sequentially.
	for _, e := range in.Edits {
		content = strings.Replace(content, e.Old, e.New, 1)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("error writing %s: %v", in.Path, err), IsError: true}
	}
	return ToolResult{ToolUseID: toolUseID, Content: fmt.Sprintf("applied %d edits to %s", len(in.Edits), in.Path), IsError: false}
}

// simpleDiff builds a compact before→after preview for display after an edit.
// Shows up to maxLines of old (removed) and new (added) lines, prefixed with
// ANSI - / + markers. Designed for human readability in the tool footer, not
// for patch application.
func simpleDiff(oldStr, newStr string, maxLines int) string {
	oldLines := strings.Split(strings.TrimRight(oldStr, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newStr, "\n"), "\n")

	var b strings.Builder
	oi, ni := 0, 0
	shown := 0
	for oi < len(oldLines) || ni < len(newLines) {
		if shown >= maxLines {
			b.WriteString(ansiDarkGray + "  │ …" + ansiReset + "\n")
			break
		}
		if oi < len(oldLines) {
			b.WriteString(ansiRed + "  - " + oldLines[oi] + ansiReset + "\n")
			oi++
			shown++
		}
		if ni < len(newLines) {
			b.WriteString(ansiGreen + "  + " + newLines[ni] + ansiReset + "\n")
			ni++
			shown++
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
