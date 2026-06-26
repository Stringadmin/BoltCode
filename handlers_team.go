package main

import (
	"fmt"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runTeam handles `team init` — upgrade a repo to team collaboration mode.
func runTeam(wd string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boltcode team <init>")
	}
	switch args[0] {
	case "init":
		// Optional team name from the remaining args.
		teamName := strings.TrimSpace(strings.Join(args[1:], " "))
		if err := command.TeamInit(wd, teamName); err != nil {
			return err
		}
		fmt.Println("team mode enabled")
		fmt.Println("  next: boltcode remote add <url>   (connect GitHub / your server)")
		fmt.Println("        boltcode task new \"<intent>\"  (create a task + worktree)")
		fmt.Println("        boltcode sync                (share state with the team)")
		return nil
	default:
		return fmt.Errorf("unknown team subcommand %q", args[0])
	}
}
