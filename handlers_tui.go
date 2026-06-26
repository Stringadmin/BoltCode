package main

import (
	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/tui"
)

// runTUI launches the interactive review interface.
func runTUI(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	return tui.Run(app)
}
