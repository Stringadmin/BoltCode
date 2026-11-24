//go:build !windows

package subprocess

import "os/exec"

func HideConsole(*exec.Cmd) {}
