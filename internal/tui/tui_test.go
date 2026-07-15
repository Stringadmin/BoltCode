package tui

import (
	"os"
	"os/exec"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/gitx"
	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func initApp(t *testing.T) *command.App {
	t.Helper()
	dir, err := os.MkdirTemp("", "tuicmd-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	// Init a bare-bones git repo so Repo.Run works.
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "user.name", "tester")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	st, err := store.NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &command.App{
		Repo:  &gitx.Repo{Root: dir},
		Store: st,
	}
}

func TestNewModel(t *testing.T) {
	_, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestInitialView(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	view := m.View()
	if view == "" {
		t.Error("empty initial view")
	}
}

func TestQuitMessage(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	result, _ := m.Update(msg)
	m2 := result.(Model)
	if !m2.quitting {
		t.Error("expected quitting=true after Ctrl-C")
	}
}

func TestEmptyListDoesNotPanic(t *testing.T) {
	m, err := New(initApp(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, key := range []string{"up", "down", "enter", "a", "m", "r"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on key %q: %v", key, r)
				}
			}()
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}()
	}
}

func TestRemoveAt(t *testing.T) {
	props := []*model.Proposal{
		{ID: "p_001"}, {ID: "p_002"}, {ID: "p_003"},
	}
	got := removeAt(props, 1)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].ID != "p_001" || got[1].ID != "p_003" {
		t.Errorf("wrong elements after remove: %v", got)
	}
}

func TestRemoveAtFirst(t *testing.T) {
	props := []*model.Proposal{{ID: "p_001"}, {ID: "p_002"}}
	got := removeAt(props, 0)
	if len(got) != 1 || got[0].ID != "p_002" {
		t.Errorf("remove first: %v", got)
	}
}

func TestRemoveAtLast(t *testing.T) {
	props := []*model.Proposal{{ID: "p_001"}, {ID: "p_002"}}
	got := removeAt(props, 1)
	if len(got) != 1 || got[0].ID != "p_001" {
		t.Errorf("remove last: %v", got)
	}
}
