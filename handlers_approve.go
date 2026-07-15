package main

import (
	"fmt"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runApprove handles both `approve <proposal>` (mark approved only) and
// `merge <proposal>` (approve + merge into base). The merge bool picks which.
func runApprove(wd string, args []string, merge bool) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: boltcode %s <proposal>",
			map[bool]string{true: "merge", false: "approve"}[merge])
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam(map[bool]string{true: "merge", false: "approve"}[merge]); err != nil {
		return err
	}
	author := app.WhoAmI() // derive reviewer identity from git config, not a hardcoded "human"

	if !merge {
		p, err := app.Approve(args[0], author)
		if err != nil {
			return err
		}
		fmt.Printf("approved %s — integrate with: boltcode merge %s\n", p.ID, p.ID)
		return nil
	}

	t, err := app.ApproveAndMerge(args[0], author)
	if err != nil {
		return err
	}
	fmt.Printf("merged proposal into %s (task %s done)\n", t.Base, t.ID)
	fmt.Printf("  push when ready:  git push origin %s\n", t.Base)
	return nil
}

// runReject sends a proposal back to the AI with a review note.
func runReject(wd string, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: boltcode reject <proposal> <note>")
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("reject"); err != nil {
		return err
	}
	note := strings.TrimSpace(strings.Join(args[1:], " "))
	if err := app.Reject(args[0], app.WhoAmI(), note); err != nil {
		return err
	}
	fmt.Printf("rejected %s — task reopened for the AI to revise\n", args[0])
	return nil
}
