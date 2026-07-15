package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runReview lists proposals awaiting human review, or handles review subcommands.
func runReview(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("review"); err != nil {
		return err
	}

	// Subcommands
	if len(args) > 0 && args[0] == "comment" {
		return runReviewComment(app, args[1:])
	}

	pending, err := app.ListPending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Println("nothing to review — all caught up")
		return nil
	}
	fmt.Printf("%d proposal(s) awaiting review:\n\n", len(pending))
	for _, p := range pending {
		stat, _ := app.Repo.DiffStat(p.BaseCommit, p.Commit)
		fmt.Printf("%-10s  task %s  [%s]\n  %s\n", p.ID, p.TaskID, p.Agent, p.Summary)
		for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
			if line != "" {
				fmt.Printf("    %s\n", line)
			}
		}
		fmt.Printf("  view: boltcode diff %s   merge: boltcode merge %s\n\n", p.ID, p.ID)
	}
	return nil
}

// runDiff prints a proposal's full unified diff.
func runDiff(wd string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: boltcode diff <proposal>")
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("diff"); err != nil {
		return err
	}
	p, err := app.Store.GetProposal(args[0])
	if err != nil {
		return fmt.Errorf("proposal %s: %w", args[0], err)
	}
	diff, err := app.ProposalDiff(p)
	if err != nil {
		return err
	}
	if diff == "" {
		fmt.Println("(no changes)")
		return nil
	}
	fmt.Fprintln(os.Stdout, diff)
	return nil
}

// runReviewComment adds an inline review comment to a proposal.
// Usage: boltcode review comment <proposal> [--file x.go] [--line 42] <text>
func runReviewComment(app *command.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: boltcode review comment <proposal> [--file x.go] [--line N] <comment>")
	}
	proposalID := args[0]
	file, rest := popFlag(args[1:], "--file", "")
	lineStr, rest := popFlag(rest, "--line", "0")
	line, _ := strconv.Atoi(lineStr)
	body := strings.TrimSpace(strings.Join(rest, " "))
	if body == "" {
		return fmt.Errorf("comment body is required")
	}
	who := app.WhoAmI()
	if err := app.AddComment(proposalID, who, body, file, line); err != nil {
		return err
	}
	loc := ""
	if file != "" {
		loc = fmt.Sprintf(" [%s", file)
		if line > 0 {
			loc += fmt.Sprintf(":%d", line)
		}
		loc += "]"
	}
	fmt.Printf("comment added to %s%s\n", proposalID, loc)
	return nil
}
