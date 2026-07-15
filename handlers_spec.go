package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runSpec handles `spec init`, `spec show`, `spec edit`.
func runSpec(wd string, args []string) error {
	if len(args) == 0 {
		return runSpecShow(wd)
	}
	switch args[0] {
	case "init":
		return runSpecInit(wd)
	case "show":
		return runSpecShow(wd)
	case "edit":
		return runSpecEdit(wd)
	default:
		return fmt.Errorf("usage: boltcode spec [init|show|edit]")
	}
}

func runSpecInit(wd string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if app.SpecExists() {
		return fmt.Errorf("spec already exists — edit with: boltcode spec edit")
	}
	if err := app.SpecInit(); err != nil {
		return err
	}
	fmt.Println("created .boltcode/spec.md — edit it to define your project constraints")
	fmt.Println("all AI agents will see this spec when working on tasks")
	return nil
}

func runSpecShow(wd string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	content, err := app.SpecContent()
	if err != nil {
		return err
	}
	fmt.Println(content)
	return nil
}

func runSpecEdit(wd string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	if !app.SpecExists() {
		return fmt.Errorf("no spec yet — create one with: boltcode spec init")
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}
	path := app.SpecPath()
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}
	fmt.Println("spec updated")
	return nil
}
