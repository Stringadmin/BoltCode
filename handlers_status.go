package main

import (
	"fmt"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runStatus handles `boltcode status` — the team dashboard.
func runStatus(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("status"); err != nil {
		return err
	}
	r, err := app.Status()
	if err != nil {
		return err
	}
	fmt.Print(command.FormatStatus(r))
	return nil
}

// runSync handles `boltcode sync` — share metadata with the team via git.
func runSync(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("sync"); err != nil {
		return err
	}
	msg, _ := popFlag(args, "--message", "")
	fmt.Println("syncing team metadata...")
	if err := app.Sync(msg); err != nil {
		return err
	}
	fmt.Println("sync complete")
	return nil
}
