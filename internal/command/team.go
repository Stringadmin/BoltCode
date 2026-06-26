package command

import (
	"fmt"
)

// TeamInit upgrades a repo from solo (default) to team collaboration mode:
// it sets Mode="team" in the config and optionally records a team name. The
// worktree isolation, task/proposal/review flow, and team sync become available.
//
// It requires the repo to already be `boltcode init`-ed (so a config exists to
// upgrade). Downgrading back to solo is a manual config edit for now.
func TeamInit(wd, teamName string) error {
	app, err := Load(wd)
	if err != nil {
		return err
	}
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
