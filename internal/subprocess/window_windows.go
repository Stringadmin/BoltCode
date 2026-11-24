package subprocess

import (
	"os/exec"
	"syscall"
)

// HideConsole prevents a console subprocess from opening a new console window.
func HideConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
