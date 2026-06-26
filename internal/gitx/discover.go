package gitx

import (
	"os"
	"path/filepath"
)

// Open discovers the git repository containing dir and returns a handle to its
// top-level worktree. It returns an error if dir is not inside a git repo.
func Open(dir string) (*Repo, error) {
	root, err := runIn(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	return &Repo{Root: filepath.Clean(root)}, nil
}

// CurrentBranch returns the checked-out branch name of the main worktree.
func (r *Repo) CurrentBranch() (string, error) {
	return r.run("rev-parse", "--abbrev-ref", "HEAD")
}

// DefaultBranch returns the default branch name for a repo that has no commits
// yet, by reading the symbolic ref of HEAD (e.g. "refs/heads/main" → "main").
func (r *Repo) DefaultBranch() (string, error) {
	out, err := r.run("symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "main", nil
	}
	return out, nil
}

// HasCommits reports whether the repo has at least one commit.
func (r *Repo) HasCommits() bool {
	_, err := r.run("rev-parse", "HEAD")
	return err == nil
}

// EnsureInitialCommit creates an empty initial commit if the repo has none.
// Git worktrees require at least one commit to function.
func (r *Repo) EnsureInitialCommit() error {
	if r.HasCommits() {
		return nil
	}
	// Ensure git identity exists (required for commit). USER is the convention
	// on Unix; USERNAME on Windows — check both so this works cross-platform.
	// (Previously only USERNAME was checked, which is empty on Linux/macOS and
	// left user.name unset, breaking the commit.)
	if name, _ := r.run("config", "user.name"); name == "" {
		user := currentUserName()
		if user == "" {
			user = "boltcode"
		}
		r.run("config", "user.name", user)
	}
	if email, _ := r.run("config", "user.email"); email == "" {
		user := currentUserName()
		if user == "" {
			user = "dev"
		}
		r.run("config", "user.email", user+"@boltcode.local")
	}
	if _, err := r.run("commit", "--allow-empty", "-m", "initial commit (boltcode)"); err != nil {
		return err
	}
	return nil
}

// currentUserName returns the OS login name, checking USER (Unix) first then
// USERNAME (Windows). Empty if neither is set.
func currentUserName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME")
}

// Head returns the full commit SHA that ref points to (default HEAD).
func (r *Repo) Head(ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	return r.run("rev-parse", ref)
}

// MergeBase returns the common ancestor commit of two refs — the point a
// proposal's diff should be computed against.
func (r *Repo) MergeBase(a, b string) (string, error) {
	return r.run("merge-base", a, b)
}

// BranchExists reports whether a local branch with the given name exists.
func (r *Repo) BranchExists(name string) bool {
	_, err := r.run("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// WorktreeBase is where BoltCode parks per-task worktrees: <repo>/.boltcode/local/worktrees.
// This is local-only state (not synced via git).
func (r *Repo) WorktreeBase() string {
	return filepath.Join(r.Root, ".boltcode", "local", "worktrees")
}

// GitPath resolves a path inside the git dir (e.g. "info/exclude"), honoring
// worktrees and custom git-dir layouts via `git rev-parse --git-path`.
func (r *Repo) GitPath(rel string) (string, error) {
	out, err := r.run("rev-parse", "--git-path", rel)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	return filepath.Join(r.Root, out), nil
}

// dirEmpty reports whether path does not exist or is an empty directory.
func dirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err != nil || len(entries) == 0
}
