package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/subprocess"
)

// gitCommandTimeout bounds built-in git tools separately from arbitrary shell
// commands. More importantly, runGit ties them to the agent session context so
// leaving the TUI cannot leave a git child process (and the agent goroutine)
// behind.
const gitCommandTimeout = 2 * time.Minute

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
	return result, false
}

func (a *Agent) toolGitCommit(message string) (string, bool) {
	if message == "" {
		return "commit message is required", true
	}
	if a.structuredTurn && (a.ConfirmFn == nil || !a.ConfirmFn("git commit: "+message)) {
		return "git commit declined by user", true
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
	if a.HideConsole {
		subprocess.HideConsole(cmd)
	}
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
