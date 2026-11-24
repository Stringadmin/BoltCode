package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// commandTimeout caps how long a single run_command tool call may run. A hung
// build or an interactive prompt would otherwise block the agent session
// forever (there was no timeout at all before). 10 minutes is generous for
// typical build/test commands while still bounding the worst case.
const commandTimeout = 10 * time.Minute

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

// toolGetDiagnostics runs a diagnostic command (lint/type-check/vet) and returns
// the output. If a specific command is given it runs that; otherwise it detects
// the project type from files in the workdir and picks an appropriate command.
func (a *Agent) toolGetDiagnostics(command string) (string, bool) {
	cmd := command
	if cmd == "" {
		if a.WorkspaceMemory != "" {
			return "Supply an explicit diagnostic command for the current workspace configuration.", true
		}
		cmd = a.detectDiagnosticCmd()
	}
	if cmd == "" {
		return "no diagnostic command configured — set one with update_memory (key: test_cmd)", false
	}
	if a.structuredTurn && (a.ConfirmFn == nil || (a.needsCommandApproval(cmd) && !a.ConfirmFn(cmd))) {
		return "diagnostic command declined by user", true
	}
	ctx, cancel := context.WithTimeout(a.context(), 2*time.Minute)
	a.setCurrentCancel(cancel)
	defer func() {
		cancel()
		a.clearCurrentCancel()
	}()
	out, err := a.commandRunner().CombinedOutput(ctx, cmd, a.Workdir, scrubEnv(os.Environ(), a.Config.APIKey))
	result := strings.TrimSpace(string(out))
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
	_, err := a.fileSystem().Stat(p)
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

func (a *Agent) needsCommandApproval(command string) bool {
	return !a.AutoAllow || isDestructive(command)
}

func (a *Agent) toolRunCommand(command string) (string, bool) {
	// Safety: confirm before executing unless auto-allow is on. Destructive
	// commands (rm -rf /, force-push to main, …) are ALWAYS confirmed, even
	// under AutoAllow — a blanket "allow all" must not silently nuke the repo.
	if a.structuredTurn && a.ConfirmFn == nil {
		return "command confirmation is unavailable", true
	}
	if a.needsCommandApproval(command) && a.ConfirmFn != nil {
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
	// Scrub credentials from the child environment so an AI-run command can't
	// exfiltrate the user's API keys. We keep the rest of the env (PATH, HOME,
	// etc.) so legitimate build/test commands still work.
	out, err := a.commandRunner().CombinedOutput(ctx, command, a.Workdir, scrubEnv(os.Environ(), a.Config.APIKey))
	result := strings.TrimSpace(string(out))
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
