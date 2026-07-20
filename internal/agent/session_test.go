package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempHomeForSessions(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	oldProf := os.Getenv("USERPROFILE")
	oldHome := os.Getenv("HOME")
	oldMax := os.Getenv("BOLTCODE_SESSION_MAX")
	oldRetention := os.Getenv("BOLTCODE_SESSION_RETENTION_DAYS")
	os.Setenv("USERPROFILE", dir)
	os.Setenv("HOME", dir)
	os.Unsetenv("BOLTCODE_SESSION_MAX")
	os.Unsetenv("BOLTCODE_SESSION_RETENTION_DAYS")
	return func() {
		os.Setenv("USERPROFILE", oldProf)
		os.Setenv("HOME", oldHome)
		os.Setenv("BOLTCODE_SESSION_MAX", oldMax)
		os.Setenv("BOLTCODE_SESSION_RETENTION_DAYS", oldRetention)
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()

	a := &Agent{
		Config:   Config{Provider: "deepseek", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", ContextWindow: 256_000, ReasoningEffort: ReasoningMedium},
		Mode:     ModePlan,
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
	if loaded.Model != "deepseek-chat" || loaded.Provider != "deepseek" || loaded.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("loaded protocol config = provider %q, model %q, base URL %q", loaded.Provider, loaded.Model, loaded.BaseURL)
	}
	if loaded.ContextWindow != 256_000 {
		t.Errorf("loaded context window = %d", loaded.ContextWindow)
	}
	if loaded.ReasoningEffort != ReasoningMedium {
		t.Errorf("loaded reasoning effort = %q", loaded.ReasoningEffort)
	}
	if loaded.Mode != ModePlan {
		t.Errorf("loaded mode = %q, want plan", loaded.Mode)
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

func TestCleanupSessionsEnforcesNewestFirstCap(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	now := time.Now()
	for index := 0; index < 5; index++ {
		writeSessionFixture(t, fmt.Sprintf("%08x", index+1), now.Add(-time.Duration(index)*time.Hour))
	}

	removed, err := cleanupSessions(now, "", sessionCleanupPolicy{MaxFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed sessions = %d, want 2", removed)
	}
	sessions, err := ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 || sessions[0].ID != "00000001" || sessions[2].ID != "00000003" {
		t.Fatalf("remaining sessions = %+v", sessions)
	}
}

func TestCleanupSessionsExpiresOldFilesButProtectsActiveSession(t *testing.T) {
	restore := withTempHomeForSessions(t)
	defer restore()
	now := time.Now()
	writeSessionFixture(t, "00000001", now.Add(-48*time.Hour))
	writeSessionFixture(t, "00000002", now.Add(-48*time.Hour))
	writeSessionFixture(t, "00000003", now)

	removed, err := cleanupSessions(now, "00000001", sessionCleanupPolicy{MaxAge: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed sessions = %d, want 1", removed)
	}
	if _, err := LoadSession("00000001"); err != nil {
		t.Fatalf("active session was removed: %v", err)
	}
	if _, err := LoadSession("00000002"); err == nil {
		t.Fatal("expired inactive session was not removed")
	}
}

func TestSessionCleanupPolicyEnvironmentOverrides(t *testing.T) {
	t.Setenv("BOLTCODE_SESSION_MAX", "12")
	t.Setenv("BOLTCODE_SESSION_RETENTION_DAYS", "7")
	policy := sessionCleanupPolicyFromEnv()
	if policy.MaxFiles != 12 || policy.MaxAge != 7*24*time.Hour {
		t.Fatalf("cleanup policy = %+v", policy)
	}

	t.Setenv("BOLTCODE_SESSION_MAX", "0")
	t.Setenv("BOLTCODE_SESSION_RETENTION_DAYS", "0")
	policy = sessionCleanupPolicyFromEnv()
	if policy.MaxFiles != 0 || policy.MaxAge != 0 {
		t.Fatalf("disabled cleanup policy = %+v", policy)
	}
}

func writeSessionFixture(t *testing.T, id string, updated time.Time) {
	t.Helper()
	session := Session{ID: id, Model: "test", UpdatedAt: updated, CreatedAt: updated}
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir(), id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
