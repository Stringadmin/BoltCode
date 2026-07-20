package shell

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

// captureStdout swaps os.Stdout for a pipe while fn runs — the scripted REPL
// and the plain session list both print via fmt to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	return string(data)
}

// Piped stdin used to make the REPL print "bye" and exit 0 without executing
// anything, so `echo status | boltcode` silently "succeeded" doing nothing.
// loopScripted must actually execute each line, refuse interactive-only
// commands with a clear message, and stop at exit.
func TestLoopScriptedExecutesCommands(t *testing.T) {
	s := &Session{scripted: true}
	out := captureStdout(t, func() {
		input := "foobar\nrun\n\nexit\nzzz\n"
		if err := s.loopScripted(strings.NewReader(input)); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "unknown: foobar") {
		t.Errorf("unknown command not reported:\n%s", out)
	}
	if !strings.Contains(out, "'run' needs an interactive terminal") {
		t.Errorf("interactive-only command not refused:\n%s", out)
	}
	if strings.Contains(out, "unknown: zzz") {
		t.Errorf("commands after exit must not execute:\n%s", out)
	}
}

// Without a terminal the session picker (a full-screen TUI) would hang, so
// `boltcode --resume` with no id prints a plain list instead. In tests stdin
// is not a terminal, which exercises exactly that path.
func TestResumeListNonInteractivePrintsPlainList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	a := &agent.Agent{
		Workdir:  home,
		Intent:   "write tests",
		Messages: []agent.Message{{Role: "user", Content: "hi"}},
	}
	a.Config.Model = "test-model"
	id, err := agent.SaveSession(a)
	if err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := listAndPickSession("test-version"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, id) || !strings.Contains(out, "write tests") || !strings.Contains(out, "boltcode --resume <id>") {
		t.Fatalf("plain session list missing data:\n%s", out)
	}
}

// `boltcode --resume <id>` drops into the full-screen chat; with piped stdin
// that can never work, so it must fail clearly instead of hanging.
func TestResumeWithIdNonInteractiveFailsClearly(t *testing.T) {
	err := Resume("v", []string{"1234abcd"})
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("expected clear terminal error, got %v", err)
	}
}
