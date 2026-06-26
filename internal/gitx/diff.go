package gitx

import "fmt"

// Diff returns the unified diff between base and commit (base..commit), i.e.
// the changes a proposal introduces. Used to render the review view.
func (r *Repo) Diff(base, commit string) (string, error) {
	return r.run("diff", base+".."+commit)
}

// DiffStat returns a compact summary (files changed, insertions, deletions)
// for a proposal — handy for list views without rendering the full diff.
func (r *Repo) DiffStat(base, commit string) (string, error) {
	return r.run("diff", "--stat", base+".."+commit)
}

// ChangedFiles lists the paths touched between base and commit.
func (r *Repo) ChangedFiles(base, commit string) ([]string, error) {
	out, err := r.run("diff", "--name-only", base+".."+commit)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return splitLines(out), nil
}

// Merge integrates branch into base via a non-fast-forward merge, so every
// approved proposal shows up as a distinct merge commit in history. This runs
// in the main worktree and assumes base is checked out there.
//
// On conflict it aborts the merge and returns an error, leaving the repo
// clean — BoltCode surfaces the conflict to the human rather than guessing.
func (r *Repo) Merge(branch, message string) error {
	if _, err := r.run("merge", "--no-ff", "-m", message, branch); err != nil {
		// Best-effort cleanup so the working tree isn't left mid-merge.
		_, _ = r.run("merge", "--abort")
		return fmt.Errorf("merge failed (conflicts left for human resolution): %w", err)
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				line := s[start:i]
				// Strip a trailing CR so CRLF output (common on Windows git)
				// doesn't leave a '\r' glued to each path — that would corrupt
				// ChangedFiles comparisons and diffs downstream.
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		line := s[start:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		out = append(out, line)
	}
	return out
}
