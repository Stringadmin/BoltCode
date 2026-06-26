package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// runAgent launches the built-in AI agent.
//
// In solo mode (default): `run` with no task launches the agent directly in the
// repo root, no worktree needed — the simplest path to "open a coding session".
// Extra args after `run` are taken as a custom instruction.
//
// In team mode: `run <task-id>` launches the agent in the task's isolated
// worktree, transitioning the task to in_progress (after dependency checks).
func (s *Session) runAgent(args []string) {
	if !s.ensureApp() {
		return
	}

	teamMode := s.App.IsTeamMode()

	// Team mode requires a task-id. Solo mode runs in the repo root.
	if teamMode {
		if len(args) < 1 {
			fmt.Println(errStyle.Render("  usage: run <task-id> [custom instruction]"))
			return
		}
		s.runAgentTask(args)
		return
	}

	// Solo mode: no task-id. Optional instruction as args.
	intent := strings.Join(args, " ")
	s.launchAgent(s.App.Repo.Root, intent, nil)
}

// runAgentTask is the team-mode path: resolve the task, check dependencies,
// transition to in_progress, then launch in the task's worktree.
func (s *Session) runAgentTask(args []string) {
	taskID := args[0]
	t, err := s.App.Store.GetTask(taskID)
	if err != nil {
		fmt.Println(errStyle.Render("  task not found: " + taskID))
		return
	}

	// Transition task to in_progress so conflict detection sees it as active
	if t.Status == model.TaskOpen {
		// Check dependencies before starting
		if blocking := s.App.CheckDependencies(t); len(blocking) > 0 {
			fmt.Println(errStyle.Render("  blocked by unfinished dependencies:"))
			for _, b := range blocking {
				fmt.Println(errStyle.Render("    → " + b))
			}
			fmt.Println(infoStyle.Render("  finish those tasks first, then retry"))
			return
		}
		t.Status = model.TaskInProgress
		t.UpdatedAt = time.Now()
		if saveErr := s.App.Store.SaveTask(t); saveErr != nil {
			fmt.Println(errStyle.Render("  warning: could not update task status: " + saveErr.Error()))
		}
	}

	intent := t.Intent
	if len(args) > 1 {
		intent = strings.Join(args[1:], " ")
	}
	s.launchAgent(t.Worktree, intent, t)
}

// launchAgent is the shared agent startup + commit flow for both modes.
// workdir is where the agent edits; intent is its goal; task is non-nil in team
// mode (drives the post-session commit message and the "ready for review" hint).
func (s *Session) launchAgent(workdir, intent string, task *model.Task) {
	// Load agent config (API key, model, etc.)
	cfg, err := s.App.LoadAgentConfig()
	if err != nil {
		fmt.Println(errStyle.Render("  " + err.Error()))
		fmt.Println(infoStyle.Render("  run 'config' to set your API key first"))
		return
	}

	fmt.Println()
	agent.AnimateSessionStart(intent, cfg.Model, workdir)

	a := &agent.Agent{
		Config:   *cfg,
		Workdir:  workdir,
		RepoRoot: s.App.Repo.Root,
		Intent:   intent,
	}

	// Command confirmation: ask user before running shell commands
	a.ConfirmFn = func(cmd string) bool {
		fmt.Printf("\n  \033[38;5;220m⚠ run:\033[0m %s\n", cmd)
		s.rl.SetPrompt("  [y/n/a] > ")
		answer := strings.TrimSpace(s.readLine())
		s.rl.SetPrompt("  ❯ ")
		switch strings.ToLower(answer) {
		case "a", "allow":
			a.AutoAllow = true
			return true
		case "n", "no":
			return false
		default:
			return true
		}
	}

	// Switch readline prompt for agent session
	s.rl.SetPrompt("  ❯ ")

	// Swap in slash-command completion for the duration of the agent session.
	// readline reads i.Config.AutoComplete on each Readline, so a live swap
	// works without recreating the instance.
	prevCompleter := s.rl.Config.AutoComplete
	s.rl.Config.AutoComplete = newSlashCompleter()
	defer func() { s.rl.Config.AutoComplete = prevCompleter }()

	// Pass the session's input function so the agent can be conversational.
	// A lone "/" prints the command list as a preview (a poor man's popup
	// completion, since readline can't show live hints while typing).
	inputFn := func() string {
		line := s.readLine()
		if line == "/" {
			printSlashCommandPreview()
			return ""
		}
		return line
	}
	// PromptFn prints a (possibly multi-line) prompt via readline's own stdout
	// writer and reads one line. This keeps the readline line buffer in sync —
	// plain fmt.Print before a Readline desyncs readline and makes it return
	// empty immediately (the /profile menu bug).
	a.PromptFn = func(prompt string) string {
		// Write the menu/prompt through readline's stdout so it triggers a
		// refresh instead of fighting the line buffer.
		fmt.Fprint(s.rl.Stdout(), prompt)
		return s.readLine()
	}

	if err := a.Run(inputFn); err != nil {
		fmt.Println(errStyle.Render("  agent error: " + err.Error()))
	}

	// After agent finishes: check for uncommitted changes and offer to commit
	if gitx.HasChanges(workdir) {
		fmt.Println()
		fmt.Println(infoStyle.Render("  uncommitted changes detected:"))
		if summary := gitx.ChangeSummary(workdir); summary != "" {
			for _, line := range strings.Split(summary, "\n") {
				if line != "" {
					fmt.Println("    " + line)
				}
			}
		}
		fmt.Println()
		fmt.Print("  commit changes? [Y/n/custom message] > ")
		answer := strings.TrimSpace(s.readLine())

		switch strings.ToLower(answer) {
		case "n", "no":
			if task != nil {
				fmt.Println(infoStyle.Render("  skipped — remember to commit before submit"))
			} else {
				fmt.Println(infoStyle.Render("  skipped — commit manually when ready"))
			}
		default:
			msg := "boltcode: session work"
			if intent != "" {
				msg = fmt.Sprintf("boltcode: %s", intent)
			}
			if answer != "" && strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
				msg = answer
			}
			if err := gitx.CommitAll(workdir, msg); err != nil {
				fmt.Println(errStyle.Render("  commit failed: " + err.Error()))
			} else {
				fmt.Println(okStyle.Render("  committed"))
			}
		}
	}

	// Restore main prompt
	s.rl.SetPrompt(mainPrompt)

	fmt.Println()
	if task != nil {
		fmt.Println(okStyle.Render(fmt.Sprintf("  task %s ready for review", task.ID)))
		fmt.Println(infoStyle.Render("  next: submit " + task.ID + " \"<summary>\""))
	} else {
		fmt.Println(okStyle.Render("  session complete"))
	}
}
