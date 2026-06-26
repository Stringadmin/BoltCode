package gitx

import (
	"fmt"
	"os"
	"path/filepath"
)

// AddWorktree creates a new branch off base and checks it out into a fresh
// worktree under WorktreeBase/<name>. This is how each task gets isolated:
// multiple AI agents can edit their own worktree concurrently with no clashes.
//
// It returns the absolute worktree path. If the branch already exists it is
// reused (checked out) rather than recreated.
func (r *Repo) AddWorktree(name, base string) (string, error) {
	if err := os.MkdirAll(r.WorktreeBase(), 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(r.WorktreeBase(), name)
	if !dirEmpty(path) {
		return "", fmt.Errorf("worktree path already in use: %s", path)
	}

	args := []string{"worktree", "add"}
	if r.BranchExists(name) {
		args = append(args, path, name)
	} else {
		// -b creates the branch; base is the start point it forks from.
		args = append(args, "-b", name, path, base)
	}
	if _, err := r.run(args...); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveWorktree detaches a task's worktree. The branch is kept so its history
// survives; pass force to discard uncommitted changes in the worktree.
func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	_, err := r.run(args...)
	return err
}
