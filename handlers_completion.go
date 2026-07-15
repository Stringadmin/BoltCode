package main

import (
	"fmt"
	"os"
)

// runCompletion handles `completion <shell>` — prints a shell completion script.
func runCompletion(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boltcode completion <bash|zsh|fish>")
	}
	switch args[0] {
	case "bash":
		return printBashCompletion()
	case "zsh":
		return printZshCompletion()
	case "fish":
		return printFishCompletion()
	default:
		return fmt.Errorf("unknown shell %q — supported: bash, zsh, fish", args[0])
	}
}

func printBashCompletion() error {
	_, err := os.Stdout.WriteString(bashCompletion)
	return err
}

func printZshCompletion() error {
	_, err := os.Stdout.WriteString(zshCompletion)
	return err
}

func printFishCompletion() error {
	_, err := os.Stdout.WriteString(fishCompletion)
	return err
}

const bashCompletion = `_boltcode_complete() {
  local cur prev words cword
  _init_completion || return
  if [[ $cword -eq 1 ]]; then
    COMPREPLY=($(compgen -W "init task submit review diff approve merge reject remote push pull spec status sync team tui help completion" -- "$cur"))
    return
  fi
  if [[ "${words[1]}" == "task" && $cword -eq 2 ]]; then
    COMPREPLY=($(compgen -W "new ls claim abandon" -- "$cur"))
  fi
}
complete -F _boltcode_complete boltcode
`

const zshCompletion = `#compdef boltcode
_boltcode() {
  local -a cmds
  cmds=(
    "init:initialize boltcode in a repo"
    "task:manage tasks"
    "submit:submit work for review"
    "review:list pending proposals"
    "diff:show proposal diff"
    "approve:approve a proposal"
    "merge:approve and merge"
    "reject:reject a proposal"
    "remote:configure remotes"
    "push:push metadata"
    "pull:pull metadata"
    "spec:manage project spec"
    "status:team dashboard"
    "sync:sync with team"
    "team:team commands"
    "tui:interactive review"
    "help:show help"
    "completion:generate shell completion"
  )
  _arguments "1: :{_describe 'command' cmds}" && return 0
  case $words[1] in
    task)
      _arguments "1: :{_describe 'task subcommand' task_subcmds}" && return 0
      ;;
  esac
}
_boltcode
`

const fishCompletion = `complete -c boltcode -f
complete -c boltcode -n '__fish_use_subcommand' -a 'init task submit review diff approve merge reject remote push pull spec status sync team tui help completion'
complete -c boltcode -n '__fish_seen_subcommand_from task' -a 'new ls claim abandon'
`
