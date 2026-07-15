package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runTask handles `task new <intent>` and `task ls`.
func runTask(wd string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boltcode task <new|ls> ...")
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("task"); err != nil {
		return err
	}

	switch args[0] {
	case "new":
		agent, rest := popFlag(args[1:], "--agent", "builtin")
		base, rest := popFlag(rest, "--base", "")
		depends, rest := popFlag(rest, "--depends", "")
		priorityStr, rest := popFlag(rest, "--priority", "0")
		tagsStr, rest := popFlag(rest, "--tags", "")
		intent := strings.TrimSpace(strings.Join(rest, " "))
		var deps []string
		if depends != "" {
			deps = strings.Split(depends, ",")
		}
		t, err := app.TaskNewWithDeps(intent, agent, base, deps)
		if err != nil {
			return err
		}
		// Apply optional priority and tags
		changed := false
		if p, _ := strconv.Atoi(priorityStr); p > 0 {
			t.Priority = p
			changed = true
		}
		if tagsStr != "" {
			t.Tags = strings.Split(tagsStr, ",")
			changed = true
		}
		if changed {
			if err := app.Store.SaveTask(t); err != nil {
				return err
			}
		}
		fmt.Printf("created task %s (branch %s)\n", t.ID, t.Branch)
		fmt.Printf("  worktree: %s\n", t.Worktree)
		fmt.Printf("  launch the built-in agent — start a session (run 'boltcode'), then:\n")
		fmt.Printf("    run %s\n", t.ID)
		fmt.Printf("  when done: boltcode submit %s \"<summary>\"\n", t.ID)
		return nil

	case "ls":
		tasks, err := app.ListTasks()
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Println("no tasks yet — create one with: boltcode task new \"<intent>\"")
			return nil
		}
		for _, t := range tasks {
			// Format: ID [status] intent @assignee (by creator)
			meta := ""
			if t.Assignee != "" {
				meta += " \033[38;5;75m@" + t.Assignee + "\033[0m"
			}
			if t.CreatedBy != "" {
				meta += " \033[38;5;241m(by " + t.CreatedBy + ")\033[0m"
			}
			if t.Priority > 0 {
				meta += fmt.Sprintf(" \033[38;5;220mP%d\033[0m", t.Priority)
			}
			if len(t.Tags) > 0 {
				meta += " \033[38;5;245m[" + strings.Join(t.Tags, ",") + "]\033[0m"
			}
			fmt.Printf("  %-12s \033[38;5;78m%-11s\033[0m %s%s\n", t.ID, t.Status, t.Intent, meta)
		}
		return nil

	case "claim":
		if len(args) < 2 {
			return fmt.Errorf("usage: boltcode task claim <task-id> [--as <name>]")
		}
		who, _ := popFlag(args[2:], "--as", "")
		if who == "" {
			who = app.WhoAmI()
		}
		t, err := app.TaskClaim(args[1], who)
		if err != nil {
			return err
		}
		fmt.Printf("task %s claimed by %s\n", t.ID, t.Assignee)
		return nil

	case "abandon":
		if len(args) < 2 {
			return fmt.Errorf("usage: boltcode task abandon <task-id>")
		}
		t, err := app.TaskAbandon(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("task %s abandoned\n", t.ID)
		return nil

	default:
		return fmt.Errorf("unknown task subcommand %q — try: new, ls, claim, abandon", args[0])
	}
}

// runSubmit handles `submit <task> <summary...>`.
func runSubmit(wd string, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: boltcode submit <task> <summary>")
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if err := app.RequireTeam("submit"); err != nil {
		return err
	}
	summary := strings.TrimSpace(strings.Join(args[1:], " "))
	p, err := app.Submit(args[0], summary, "")
	if err != nil {
		return err
	}
	fmt.Printf("submitted proposal %s for task %s — review with: boltcode review\n", p.ID, args[0])
	return nil
}
