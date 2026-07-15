package command

import (
	"fmt"

	"github.com/Stringadmin/BoltCode/internal/gitx"
)

// TeamInit upgrades a repo from solo (default) to team collaboration mode:
// it sets Mode="team" in the config and optionally records a team name. The
// worktree isolation, task/proposal/review flow, and team sync become available.
//
// It requires the repo to already be `boltcode init`-ed (so a config exists to
// upgrade). Downgrading back to solo is a manual config edit for now.
//
// TeamInit opens only the repo (not the metadata store): it only reads/writes
// .boltcode/config.json, which needs no store and no cross-process lock.
// Opening a store here would leak the lock file handle in tests (Windows can't
// delete an open file) and would hold the lock unnecessarily.
func TeamInit(wd, teamName string) error {
	repo, err := gitx.Open(wd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}
	app := &App{Repo: repo}
	cfg, err := app.LoadConfig()
	if err != nil {
		return fmt.Errorf("not initialized — run 'boltcode init' first: %w", err)
	}
	if cfg.Mode == "team" {
		return fmt.Errorf("already in team mode")
	}
	cfg.Mode = "team"
	if teamName != "" {
		cfg.Team = teamName
	}
	if err := app.SaveConfig(cfg); err != nil {
		return err
	}
	return nil
}
