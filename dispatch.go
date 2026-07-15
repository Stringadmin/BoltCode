package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/banner"
	"github.com/Stringadmin/BoltCode/internal/command"
)

// dispatch routes a subcommand to its handler. `init` is special — it bootstraps
// the repo, so it doesn't load an existing App first.
func dispatch(cmd string, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	switch cmd {
	case "init":
		return command.Init(wd, "")
	case "task":
		return runTask(wd, args)
	case "submit":
		return runSubmit(wd, args)
	case "review":
		return runReview(wd, args)
	case "diff":
		return runDiff(wd, args)
	case "approve":
		return runApprove(wd, args, false)
	case "merge":
		return runApprove(wd, args, true)
	case "reject":
		return runReject(wd, args)
	case "remote":
		return runRemote(wd, args)
	case "push":
		return runPush(wd, args)
	case "pull":
		return runPull(wd, args)
	case "spec":
		return runSpec(wd, args)
	case "status":
		return runStatus(wd, args)
	case "sync":
		return runSync(wd, args)
	case "completion":
		return runCompletion(args)
	case "team":
		return runTeam(wd, args)
	case "tui":
		return runTUI(wd, args)
	case "-h", "--help", "help":
		banner.Print(Version)
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// popFlag extracts "--name value" and "--name=value" pairs from args, returning
// the value (or def) and the remaining positional args. Minimal by design — we
// only have a few flags; for anything richer, switch to a real flag parser.
func popFlag(args []string, name, def string) (string, []string) {
	out := def
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			// --name value (space-separated)
			out = args[i+1]
			i++
			continue
		}
		if prefix := name + "="; strings.HasPrefix(args[i], prefix) {
			// --name=value (equals form)
			out = strings.TrimPrefix(args[i], prefix)
			continue
		}
		rest = append(rest, args[i])
	}
	return out, rest
}
