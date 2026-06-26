// Package command implements BoltCode's subcommands. Each command operates on
// an App, which bundles the discovered git repo with the metadata store so
// handlers don't re-discover them.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/store"
)

// App is the shared runtime context for every command.
type App struct {
	Repo  *gitx.Repo
	Store store.Store
}

// dataDir is where BoltCode keeps its metadata, under the repo root.
func dataDir(repo *gitx.Repo) string {
	return filepath.Join(repo.Root, ".boltcode")
}

// LoadConfig reads the project-level config from .boltcode/config.json.
func (a *App) LoadConfig() (*Config, error) {
	path := configPath(a.Repo)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes the project-level config, preserving fields other callers
// may have set. Reads the current file (if any), applies the given mutator, and
// writes it back atomically.
func (a *App) SaveConfig(cfg *Config) error {
	path := configPath(a.Repo)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// IsTeamMode reports whether this repo has been upgraded to team collaboration
// mode. Absent or "solo" config (including legacy configs with no Mode field)
// counts as solo, so existing projects keep working unchanged.
func (a *App) IsTeamMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.Mode == "team"
}

// RequireTeam returns an error if the repo is not in team mode. Used to gate
// collaboration-only commands (task/submit/review/sync/status) so solo users
// get a clear "upgrade with team init" hint instead of confusing behavior.
func (a *App) RequireTeam(cmd string) error {
	if a.IsTeamMode() {
		return nil
	}
	return fmt.Errorf("%q is a team-collaboration feature — run 'boltcode team init' to enable worktree isolation, review, and team sync", cmd)
}

// NewJSONStore opens (creating if needed) the JSON metadata store for a repo.
func NewJSONStore(repo *gitx.Repo) (store.Store, error) {
	return store.NewJSON(dataDir(repo))
}

// Load discovers the repo containing wd and opens its metadata store. It is the
// entry point for every command except `init`, which uses LoadForInit.
func Load(wd string) (*App, error) {
	repo, err := gitx.Open(wd)
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}
	st, err := store.NewJSON(dataDir(repo))
	if err != nil {
		return nil, err
	}
	return &App{Repo: repo, Store: st}, nil
}
