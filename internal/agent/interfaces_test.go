package agent

// Tests for the CommandRunner and FileSystem dependency interfaces: tools run
// against fakes, no real processes and no real disk.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── fake CommandRunner ──────────────────────────────────────────────────────

type fakeRunner struct {
	output  []byte
	err     error
	command string
	dir     string
	env     []string
}

func (f *fakeRunner) CombinedOutput(_ context.Context, command, dir string, env []string) ([]byte, error) {
	f.command, f.dir, f.env = command, dir, env
	return f.output, f.err
}

func TestRunCommandUsesInjectedRunner(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	runner := &fakeRunner{output: []byte("build ok\n")}
	a := &Agent{ExecEnv: ExecEnv{Workdir: `C:\fake\proj`, Runner: runner}}

	out, isErr := a.toolRunCommand("go build ./...")
	if isErr || out != "build ok" {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
	if runner.command != "go build ./..." || runner.dir != a.Workdir {
		t.Fatalf("runner got command=%q dir=%q", runner.command, runner.dir)
	}
	for _, kv := range runner.env {
		if strings.Contains(kv, "ANTHROPIC_API_KEY") || strings.Contains(kv, "sk-secret") {
			t.Fatalf("secret env leaked to child: %q", kv)
		}
	}
}

func TestRunCommandReportsRunnerError(t *testing.T) {
	runner := &fakeRunner{output: []byte("boom"), err: errors.New("exit status 1")}
	a := &Agent{ExecEnv: ExecEnv{Workdir: `C:\fake\proj`, Runner: runner}}

	out, isErr := a.toolRunCommand("false")
	if !isErr || !strings.Contains(out, "boom") || !strings.Contains(out, "exit status 1") {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
}

func TestGetDiagnosticsUsesInjectedRunner(t *testing.T) {
	runner := &fakeRunner{output: []byte("vet passed")}
	a := &Agent{ExecEnv: ExecEnv{Workdir: `C:\fake\proj`, Runner: runner}}

	out, isErr := a.toolGetDiagnostics("go vet ./...")
	if isErr || out != "vet passed" {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
	if runner.command != "go vet ./..." {
		t.Fatalf("diagnostics ran %q", runner.command)
	}
}

// ── fake FileSystem ─────────────────────────────────────────────────────────

type fakeFileInfo struct {
	name string
	size int64
	dir  bool
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() interface{}   { return nil }

type fakeDirEntry struct{ info fakeFileInfo }

func (d fakeDirEntry) Name() string               { return d.info.name }
func (d fakeDirEntry) IsDir() bool                { return d.info.dir }
func (d fakeDirEntry) Type() os.FileMode          { return d.info.mode }
func (d fakeDirEntry) Info() (os.FileInfo, error) { return d.info, nil }

type fakeFS struct {
	files map[string][]byte
	dirs  map[string]bool
}

func newFakeFS(workdir string) *fakeFS {
	return &fakeFS{files: map[string][]byte{}, dirs: map[string]bool{filepath.Clean(workdir): true}}
}

func (f *fakeFS) ReadFile(name string) ([]byte, error) {
	data, ok := f.files[filepath.Clean(name)]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.files[filepath.Clean(name)] = append([]byte(nil), data...)
	return nil
}

func (f *fakeFS) MkdirAll(path string, perm os.FileMode) error {
	f.dirs[filepath.Clean(path)] = true
	return nil
}

func (f *fakeFS) Stat(name string) (os.FileInfo, error) {
	clean := filepath.Clean(name)
	if data, ok := f.files[clean]; ok {
		return fakeFileInfo{name: filepath.Base(clean), size: int64(len(data)), mode: 0o644}, nil
	}
	if f.dirs[clean] {
		return fakeFileInfo{name: filepath.Base(clean), dir: true, mode: 0o755 | os.ModeDir}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
}

func (f *fakeFS) ReadDir(name string) ([]os.DirEntry, error) {
	clean := filepath.Clean(name)
	if !f.dirs[clean] {
		return nil, &os.PathError{Op: "readdir", Path: name, Err: os.ErrNotExist}
	}
	prefix := clean + string(filepath.Separator)
	var out []os.DirEntry
	for path, data := range f.files {
		rest := strings.TrimPrefix(path, prefix)
		if rest != path && !strings.ContainsRune(rest, filepath.Separator) {
			out = append(out, fakeDirEntry{info: fakeFileInfo{name: filepath.Base(path), size: int64(len(data)), mode: 0o644}})
		}
	}
	return out, nil
}

func (f *fakeFS) Remove(name string) error {
	clean := filepath.Clean(name)
	if _, ok := f.files[clean]; ok {
		delete(f.files, clean)
		return nil
	}
	if f.dirs[clean] {
		delete(f.dirs, clean)
		return nil
	}
	return &os.PathError{Op: "remove", Path: name, Err: os.ErrNotExist}
}

func (f *fakeFS) Rename(oldpath, newpath string) error {
	oldClean, newClean := filepath.Clean(oldpath), filepath.Clean(newpath)
	data, ok := f.files[oldClean]
	if !ok {
		return &os.PathError{Op: "rename", Path: oldpath, Err: os.ErrNotExist}
	}
	f.files[newClean] = data
	delete(f.files, oldClean)
	return nil
}

// ── file tools on the fake ──────────────────────────────────────────────────

func TestFileToolsRoundtripOnFakeFS(t *testing.T) {
	workdir := `C:\fake\proj`
	a := &Agent{ExecEnv: ExecEnv{Workdir: workdir, FS: newFakeFS(workdir)}}
	joined := func(parts ...string) string {
		return filepath.Join(append([]string{workdir}, parts...)...)
	}

	if msg, isErr := a.toolWriteFile("notes.txt", "hello\nworld\n"); isErr {
		t.Fatalf("write: %s", msg)
	}
	out, isErr := a.toolReadFile("notes.txt", "", "")
	if isErr || !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("read: out=%q isErr=%v", out, isErr)
	}
	if msg, isErr := a.toolEditFile("notes.txt", "world", "there"); isErr {
		t.Fatalf("edit: %s", msg)
	}
	if got := string(a.FS.(*fakeFS).files[joined("notes.txt")]); got != "hello\nthere\n" {
		t.Fatalf("edited content = %q", got)
	}
	if msg, isErr := a.toolRenameFile("notes.txt", "docs/notes.txt"); isErr {
		t.Fatalf("rename: %s", msg)
	}
	if out, isErr = a.toolReadFile("docs/notes.txt", "", ""); isErr || !strings.Contains(out, "there") {
		t.Fatalf("read renamed: out=%q isErr=%v", out, isErr)
	}
	if msg, isErr := a.toolDeleteFile("docs/notes.txt"); isErr {
		t.Fatalf("delete: %s", msg)
	}
	if _, err := a.FS.Stat(joined("docs", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file still stat-able: %v", err)
	}
}

func TestMultiEditOnFakeFS(t *testing.T) {
	workdir := `C:\fake\proj`
	fs := newFakeFS(workdir)
	a := &Agent{ExecEnv: ExecEnv{Workdir: workdir, FS: fs}}
	fs.files[filepath.Join(workdir, "m.go")] = []byte("alpha\nbeta\n")

	result := a.toolMultiEdit("id1", json.RawMessage(`{"path":"m.go","edits":[{"old_string":"beta","new_string":"BETA"}]}`))
	if result.IsError {
		t.Fatalf("multi_edit: %s", result.Content)
	}
	if got := string(fs.files[filepath.Join(workdir, "m.go")]); got != "alpha\nBETA\n" {
		t.Fatalf("content = %q", got)
	}
	if !strings.Contains(result.Diff, "+ BETA") {
		t.Fatalf("diff preview missing:\n%s", result.Diff)
	}
}

func TestReadFileMissingOnFakeFS(t *testing.T) {
	a := &Agent{ExecEnv: ExecEnv{Workdir: `C:\fake\proj`, FS: newFakeFS(`C:\fake\proj`)}}
	out, isErr := a.toolReadFile("nope.txt", "", "")
	if !isErr || !strings.Contains(out, "error reading") {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
}

func TestNilDependenciesFallBackToOS(t *testing.T) {
	a := &Agent{}
	if _, ok := a.commandRunner().(osCommandRunner); !ok {
		t.Fatal("nil Runner should default to the OS runner")
	}
	if _, ok := a.fileSystem().(osFS); !ok {
		t.Fatal("nil FS should default to the OS filesystem")
	}
}
