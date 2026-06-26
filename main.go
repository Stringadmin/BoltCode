// Command boltcode is a human-in-the-loop review layer over git, built for
// AI-assisted development: AI agents work in isolated worktrees and submit
// proposals; humans review, then approve and merge. AI never merges or pushes.
package main

import (
	"fmt"
	"os"

	"github.com/Stringadmin/BoltCode/internal/shell"
)

const Version = "0.1.0"

func main() {
	// `boltcode --resume <id>` (or `-r <id>`): jump straight back into a saved
	// agent conversation, Claude-Code style. No id → list recent sessions.
	if len(os.Args) >= 2 && (os.Args[1] == "--resume" || os.Args[1] == "-r") {
		if err := shell.Resume(Version, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) < 2 {
		// No args: enter interactive session (like Claude Code)
		if err := shell.Run(Version); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if err := dispatch(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `boltcode — a built-in AI coding agent with an optional team-collaboration layer

BoltCode runs in two modes:
  - SOLO (default): one person + the built-in AI agent. Open a coding session,
    edit, commit. Zero setup beyond ` + "`boltcode init`" + `.
  - TEAM: many people × many AI agents. Worktree isolation per task, proposals,
    human review, and team sync over git. Upgrade with ` + "`boltcode team init`" + `.

The recommended way to use BoltCode is the interactive session: just run
  boltcode
with no arguments. The commands below are also available as one-off CLI subcommands.

interactive session (run 'boltcode' with no args):
  init                     set up boltcode in the current repo
  run [instruction]        launch the built-in AI agent (solo: in repo root)
  run <task-id>            launch the agent in a task's worktree (team mode)
  config                   set your API key + model for the agent
  team init                upgrade solo → team collaboration mode
  setup                    connect a remote (GitHub/GitLab or self-hosted)
  spec [init]              show (or create) the project spec
  push / pull              push to / pull from the remote

resume a saved conversation (sessions auto-save when an agent session ends):
  boltcode --resume <id>   continue a specific past session
  boltcode --resume        list recent sessions and pick one

  team-mode only:
  task new <intent>        create a task + isolated worktree
  task ls / task claim <id>
  submit <task> <summary>  submit work as a proposal
  review / diff <proposal> / approve / merge / reject
  status                   team dashboard
  sync                     commit + push/pull metadata with team

  inside an agent session, slash commands:
  /help /tokens /time /model /diff /files /clear /compact
  /triage [worktree|repo]  review the codebase, get prioritized action items

one-off CLI subcommands (non-interactive):
  boltcode init
  boltcode team init [team-name]
  boltcode remote add <url>      url: git@github.com:user/repo.git |
                                        https://github.com/user/repo.git |
                                        user@host:path
  boltcode remote setup | show
  boltcode push / pull
  boltcode spec init | (show) | edit
  boltcode tui                   interactive review interface
  boltcode [team-only] task new|ls|claim | submit | review | diff |
                                 approve | merge | reject | status | sync

flags (for 'task new'):
  --base <branch>   base branch (default: current)
  --depends <ids>   comma-separated task ids this task depends on
  --priority N      task priority (higher = more urgent)
  --tags a,b        comma-separated tags

note: 'run', 'config', and the slash commands live only in the interactive
session — they are not one-off CLI subcommands.
`)
}
