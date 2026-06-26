// Package gitx wraps the git CLI for the operations BoltCode needs:
// discovering the repo, creating isolated worktrees per task, computing the
// diff of a proposal, and merging an approved proposal back to its base.
//
// We shell out to the real `git` binary rather than use a pure-Go library:
// worktree and merge semantics are subtle, and matching the user's installed
// git exactly avoids surprises.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a handle to a git repository rooted at Root (the main worktree).
type Repo struct {
	Root string
}

// run executes a git command inside the repo root and returns trimmed stdout.
// On failure it returns an error that includes stderr for diagnosis.
func (r *Repo) run(args ...string) (string, error) {
	return runIn(r.Root, args...)
}

// Run is the exported version of run, for use by command-layer code that needs
// to issue arbitrary git commands (e.g. remote add/remove).
func (r *Repo) Run(args ...string) (string, error) {
	return r.run(args...)
}

// ConfigUser returns the developer's name from `git config user.name`, or an
// empty string if it isn't set. Used to seed the BoltCode identity at init so
// the user doesn't have to retype what git already knows.
func (r *Repo) ConfigUser() string {
	name, err := r.run("config", "user.name")
	if err != nil {
		return ""
	}
	return name
}

func runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
