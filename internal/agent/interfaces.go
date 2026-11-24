package agent

// Small dependency interfaces for the execution side of the agent. They exist
// so tests can substitute fakes instead of spawning real processes or writing
// to a real disk. Both default to the real OS implementations when the Agent
// fields are nil.

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/Stringadmin/BoltCode/internal/subprocess"
)

// CommandRunner executes a shell command string and returns its merged
// stdout/stderr. ctx carries the timeout/cancellation.
type CommandRunner interface {
	CombinedOutput(ctx context.Context, command, dir string, env []string) ([]byte, error)
}

// ExecEnv groups the execution-side dependencies of the agent: everything a
// tool needs to run, and nothing it does not. It is embedded in Agent, so
// existing reads (a.Workdir, a.AutoAllow, …) keep working through promotion,
// while later work (subpackage extraction) can take an ExecEnv value instead
// of the whole Agent.
type ExecEnv struct {
	Workdir     string                // the task worktree (or repo root in solo mode)
	AutoAllow   bool                  // session-level "allow all commands" flag
	HideConsole bool                  // desktop tools run without a separate console
	ConfirmFn   func(cmd string) bool // ask user before running commands
	Runner      CommandRunner         // nil uses the real os/exec runner
	FS          FileSystem            // nil uses the real OS filesystem
}

type osCommandRunner struct{ hideConsole bool }

func (runner osCommandRunner) CombinedOutput(ctx context.Context, command, dir string, env []string) ([]byte, error) {
	return shellCommand(ctx, command, dir, env, runner.hideConsole).CombinedOutput()
}

func shellCommand(ctx context.Context, command, dir string, env []string, hideConsole bool) *exec.Cmd {
	cmd := exec.CommandContext(ctx, shellName(), shellArgs(command)...)
	if hideConsole {
		subprocess.HideConsole(cmd)
	}
	cmd.Dir = dir
	cmd.Env = env
	// A subprocess can leave inherited output pipes open after the shell exits.
	// Bound the wait so cancellation can always return control to the client.
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func (a *Agent) commandRunner() CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return osCommandRunner{hideConsole: a.HideConsole}
}

// FileSystem abstracts the file operations the file tools use. safeJoin stays
// outside it: path safety is resolved against the real filesystem, and it
// already degrades to pure string checks when the workdir does not exist on
// disk (which is exactly the fake-FS test setup).
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
	ReadDir(name string) ([]os.DirEntry, error)
	Remove(name string) error
	Rename(oldpath, newpath string) error
}

type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
func (osFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (osFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (osFS) ReadDir(name string) ([]os.DirEntry, error)   { return os.ReadDir(name) }
func (osFS) Remove(name string) error                     { return os.Remove(name) }
func (osFS) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }

func (a *Agent) fileSystem() FileSystem {
	if a.FS != nil {
		return a.FS
	}
	return osFS{}
}
