package shell

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/tui"
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
	// The worktree path isn't persisted (json:"-") to avoid leaking a
	// machine-local path into synced team metadata — re-derive it from the
	// branch, which is deterministic (WorktreeBase + "/" + branch).
	s.App.ResolveWorktree(t)

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
		// Command confirmation is handled by the TUI's confirm popup
		// (wired in tui.RunChat via a.ConfirmFn).
	}

	// Launch the chat TUI. The agent runs in a goroutine inside RunChat;
	// its stdout is captured to a pipe and displayed in the TUI's message
	// area. The input box stays visible and interactive at all times.
	if err := tui.RunChat(a); err != nil {
		fmt.Println(errStyle.Render("  tui error: " + err.Error()))
	}

	// After agent finishes: check for uncommitted changes and offer to commit.
	// Use raw stdin (not readline) since the TUI may have left terminal state
	// in a condition readline doesn't expect.
	reader := bufio.NewReader(os.Stdin)
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
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)

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
