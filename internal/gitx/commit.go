package gitx

import "fmt"

// HasChanges reports whether the given directory (typically a task worktree)
// has any uncommitted modifications or untracked files.
func HasChanges(dir string) bool {
	out, err := runIn(dir, "status", "--porcelain")
	if err != nil {
		return false
	}
	return out != ""
}

// CommitAll stages all changes in dir and creates a commit with the given
// message. This is how agent work gets persisted so `submit` can find new
// commits on the task branch.
func CommitAll(dir, message string) error {
	if _, err := runIn(dir, "add", "-A"); err != nil {
		return fmt.Errorf("staging changes: %w", err)
	}
	if _, err := runIn(dir, "commit", "-m", message); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	return nil
}

// ChangeSummary returns a short stat line for uncommitted changes in dir.
func ChangeSummary(dir string) string {
	out, _ := runIn(dir, "diff", "--stat", "HEAD")
	if out == "" {
		out, _ = runIn(dir, "status", "--short")
	}
	return out
}
