package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempHomeForSessions(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	oldProf := os.Getenv("USERPROFILE")
	oldHome := os.Getenv("HOME")
	os.Setenv("USERPROFILE", dir)
	os.Setenv("HOME", dir)
	return func() {
		os.Setenv("USERPROFILE", oldProf)
		os.Setenv("HOME", oldHome)
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()

	a := &Agent{
		Config:   Config{Model: "claude-x"},
		Workdir:  "/some/dir",
		RepoRoot: "/some",
		Intent:   "fix the bug",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}

	id, err := SaveSession(a)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty session id")
	}
	if a.SessionID != id {
		t.Errorf("agent.SessionID = %q, want %q", a.SessionID, id)
	}

	loaded, err := LoadSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != id {
		t.Errorf("loaded id = %q", loaded.ID)
	}
	if loaded.Workdir != "/some/dir" {
		t.Errorf("loaded workdir = %q", loaded.Workdir)
	}
	if loaded.Model != "claude-x" {
		t.Errorf("loaded model = %q", loaded.Model)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("loaded %d messages, want 2", len(loaded.Messages))
	}
}

func TestSaveSessionReusesId(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()

	a := &Agent{
		Messages: []Message{{Role: "user", Content: "x"}},
	}
	id1, _ := SaveSession(a)
	id2, _ := SaveSession(a) // second save should reuse the id (resume-of-resume)
	if id1 != id2 {
		t.Errorf("second save produced new id %q, want reuse %q", id2, id1)
	}
}

func TestLoadSessionNotFound(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	if _, err := LoadSession("nonexistent"); err == nil {
		t.Fatal("expected error loading missing session")
	}
}

func TestListSessionsNewestFirst(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()

	// Save two sessions; the second is newer.
	a1 := &Agent{Messages: []Message{{Role: "user", Content: "first"}}}
	id1, _ := SaveSession(a1)
	a2 := &Agent{Messages: []Message{{Role: "user", Content: "second"}}}
	id2, _ := SaveSession(a2)

	list, err := ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if list[0].ID != id2 {
		t.Errorf("newest first: got %q, want %q", list[0].ID, id2)
	}

	// Files actually exist on disk.
	if _, err := os.Stat(filepath.Join(sessionsDir(), id1+".json")); err != nil {
		t.Errorf("session file missing: %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	a := &Agent{Messages: []Message{{Role: "user", Content: "x"}}}
	id, _ := SaveSession(a)
	if err := DeleteSession(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := LoadSession(id); err == nil {
		t.Fatal("session should be gone after delete")
	}
}

// --- path-traversal protection (P0) ---
// LoadSession/DeleteSession must reject malformed ids before joining them into
// a filesystem path, so `boltcode --resume ../../etc/passwd` can't read or
// delete arbitrary files.

func TestLoadSessionRejectsTraversalID(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	bad := []string{
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"nonexistent", // wrong length / not hex
		"ZZZZZZZZ",    // not hex
		"abcdefg",     // 7 chars
		"abcdefghi",   // 9 chars
	}
	for _, id := range bad {
		if _, err := LoadSession(id); err == nil {
			t.Errorf("LoadSession(%q) should reject malformed id", id)
		}
	}
}

func TestDeleteSessionRejectsTraversalID(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	if err := DeleteSession("../../etc/passwd"); err == nil {
		t.Fatal("DeleteSession must reject traversal id")
	}
}

func TestLoadSessionAcceptsWellFormedID(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	a := &Agent{Messages: []Message{{Role: "user", Content: "x"}}}
	id, _ := SaveSession(a)
	// The saved id must be loadable (round-trip), proving validation doesn't
	// reject legitimate ids.
	if _, err := LoadSession(id); err != nil {
		t.Fatalf("LoadSession(legit %q): %v", id, err)
	}
}
