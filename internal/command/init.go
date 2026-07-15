package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/gitx"
)

// Config holds repo-level BoltCode settings, stored at .boltcode/config.json.
type Config struct {
	DefaultBase string `json:"default_base"` // branch tasks fork from / merge into
	User        string `json:"user"`         // current developer identity
	Team        string `json:"team,omitempty"`
	Mode        string `json:"mode,omitempty"` // "solo" (default) or "team"
}

func configPath(repo *gitx.Repo) string {
	return filepath.Join(dataDir(repo), "config.json")
}

// Init sets up BoltCode in the repo containing wd: creates .boltcode/, records a
// default base branch and user identity, and adds .boltcode/local to
// .git/info/exclude so local state never shows up as untracked noise.
func Init(wd, user string) error {
	repo, err := gitx.Open(wd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}
	if _, err := os.Stat(configPath(repo)); err == nil {
		return fmt.Errorf("boltcode already initialized in %s", repo.Root)
	}
	// Ensure the metadata directory layout exists. We create the dirs directly
	// rather than via NewJSONStore, because NewJSONStore opens a cross-process
	// lock file whose handle would leak here (Init discards the store). Load
	// opens the real store later.
	for _, sub := range []string{"tasks", "proposals", "reviews", "local"} {
		if err := os.MkdirAll(filepath.Join(dataDir(repo), sub), 0o755); err != nil {
			return err
		}
	}

	base, err := repo.CurrentBranch()
	if err != nil {
		// Empty repo (no commits yet) — read default branch from HEAD ref
		base, err = repo.DefaultBranch()
		if err != nil {
			base = "main"
		}
	}
	// Seed identity from git config when the caller didn't supply one, so a
	// non-interactive `boltcode init` reuses what git already knows.
	if user == "" {
		user = repo.ConfigUser()
	}
	cfg := Config{DefaultBase: base, User: user, Mode: "solo"}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := fio.WriteFileAtomic(configPath(repo), data, 0o644); err != nil {
		return err
	}
	if err := excludeWorktrees(repo); err != nil {
		// Non-fatal: the worktrees still function, they'll just show as untracked.
		fmt.Printf("warning: could not update git exclude file: %v\n", err)
	}

	fmt.Printf("Initialized boltcode in %s (base branch: %s)\n", repo.Root, base)
	return nil
}

// excludeWorktrees appends BoltCode's internal dirs to .git/info/exclude so the
// per-task worktrees and local state never appear as untracked files. It is
// best-effort: a failure here doesn't fail init, since the worktrees still work.
func excludeWorktrees(repo *gitx.Repo) error {
	excludePath, err := repo.GitPath("info/exclude")
	if err != nil {
		return err
	}
	existing, _ := os.ReadFile(excludePath)
	const marker = "/.boltcode/local"
	if strings.Contains(string(existing), marker) {
		return nil // already excluded
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n# added by boltcode — local state (per-machine, never synced)\n/.boltcode/local/\n")
	return err
}
