package command

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitEnv is the environment for non-interactive git commands run by Sync.
// GIT_TERMINAL_PROMPT=0 makes git fail fast instead of hanging forever waiting
// for a username/password on a slow or unauthenticated remote — which would
// otherwise freeze the whole sync (and the session). It is cross-platform,
// unlike the SSH_ASKPASS shims.
func gitEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	seen := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			out = append(out, "GIT_TERMINAL_PROMPT=0")
			seen = true
			continue
		}
		out = append(out, kv)
	}
	if !seen {
		out = append(out, "GIT_TERMINAL_PROMPT=0")
	}
	return out
}

// Sync commits any local metadata changes under .boltcode/ (tasks, proposals,
// reviews, spec, config — but NOT local/) and pushes to the remote, then pulls
// remote changes. This is how teams share state without a server beyond git.
func (a *App) Sync(message string) error {
	metaDir := filepath.Join(a.Repo.Root, ".boltcode")

	// Ensure .boltcode/local is excluded (API keys, worktrees — per-machine state)
	gitignorePath := filepath.Join(metaDir, ".gitignore")
	ensureGitignore(gitignorePath, "local/")

	// Stage all shared metadata. Each path is added only if it exists — a
	// fresh project may not have reviews/ or remote.json yet, and `git add`
	// errors on missing paths. Real failures (permissions, etc.) still surface.
	sharedPaths := []string{
		filepath.Join(".boltcode", "tasks"),
		filepath.Join(".boltcode", "proposals"),
		filepath.Join(".boltcode", "reviews"),
		filepath.Join(".boltcode", "spec.md"),
		filepath.Join(".boltcode", "config.json"),
		filepath.Join(".boltcode", "remote.json"),
		filepath.Join(".boltcode", ".gitignore"),
	}
	for _, p := range sharedPaths {
		abs := filepath.Join(a.Repo.Root, p)
		if _, err := os.Stat(abs); err != nil {
			continue // path doesn't exist yet — nothing to stage
		}
		if _, err := a.Repo.Run("add", p); err != nil {
			return fmt.Errorf("staging %s: %w", p, err)
		}
	}

	// Check if there's anything staged
	status, _ := a.Repo.Run("diff", "--cached", "--stat")
	if strings.TrimSpace(status) == "" {
		fmt.Println("  no metadata changes to sync")
	} else {
		if message == "" {
			message = "boltcode: sync metadata"
		}
		if _, err := a.Repo.Run("commit", "-m", message); err != nil {
			return fmt.Errorf("commit metadata: %w", err)
		}
		fmt.Println("  committed metadata changes")
	}

	// Push + pull. A failed push means the team never received this metadata —
	// that must surface as an error so callers don't print "sync complete". A
	// failed pull may leave the repo mid-rebase, also an error. Previously both
	// printed a warning and returned nil, misleading the user into thinking
	// sync succeeded.
	rc, err := a.LoadRemoteConfig()
	if err != nil {
		fmt.Println("  no remote configured — local commit only")
		return nil
	}

	var syncErr error // accumulates push/pull failures

	fmt.Printf("  pushing to %s...\n", rc.Name)
	pushCmd := exec.Command("git", "push", rc.Name)
	pushCmd.Dir = a.Repo.Root
	pushCmd.Env = gitEnv()
	if out, err := pushCmd.CombinedOutput(); err != nil {
		// The local commit already happened above. A failed push means the
		// team hasn't received this metadata yet — make that unmistakable so
		// the user doesn't assume sync succeeded.
		msg := fmt.Sprintf("push failed (committed locally but NOT shared): %s — retry with: boltcode push",
			strings.TrimSpace(string(out)))
		fmt.Printf("  ⚠ %s\n", msg)
		syncErr = fmt.Errorf("%s", msg)
	} else {
		fmt.Println("  pushed")
	}

	fmt.Printf("  pulling from %s...\n", rc.Name)
	pullCmd := exec.Command("git", "pull", "--rebase", rc.Name)
	pullCmd.Dir = a.Repo.Root
	pullCmd.Env = gitEnv()
	if out, err := pullCmd.CombinedOutput(); err != nil {
		msg := fmt.Sprintf("pull failed: %s", strings.TrimSpace(string(out)))
		fmt.Printf("  ⚠ %s\n", msg)
		if syncErr != nil {
			syncErr = fmt.Errorf("%w; %s", syncErr, msg)
		} else {
			syncErr = fmt.Errorf("%s", msg)
		}
	} else {
		fmt.Printf("  %s\n", strings.TrimSpace(string(out)))
	}

	// Check for potential conflicts between active tasks (only meaningful if
	// the pull brought in new state — still run it best-effort).
	if warnings := a.DetectConflicts(); len(warnings) > 0 {
		fmt.Println()
		for _, w := range warnings {
			fmt.Println("  " + w)
		}
	}

	return syncErr
}

// ensureGitignore appends pattern to the gitignore file if not already present
// as its own line. Matching whole lines (not substrings) avoids false positives
// like "localfiles/" satisfying a check for "local/".
func ensureGitignore(path, pattern string) {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return // already ignored
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(pattern + "\n")
}
