package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Session is a saved agent conversation, persisted to ~/.boltcode/sessions/
// so a user can resume it later (like `claude --resume <id>`). Stored at user
// level so sessions are visible across projects; the original Workdir is
// recorded so resume can re-enter the right directory context.
type Session struct {
	ID        string    `json:"id"`
	Workdir   string    `json:"workdir"`
	RepoRoot  string    `json:"repo_root,omitempty"`
	Intent    string    `json:"intent,omitempty"`
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// sessionsDir returns ~/.boltcode/sessions/, creating it if needed.
func sessionsDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".boltcode", "sessions")
	os.MkdirAll(dir, 0o700)
	return dir
}

// newSessionID returns a short hex id like "5521faf4" (8 chars), matching the
// Claude Code resume id style. Uses crypto/rand so ids are unpredictable.
func newSessionID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a time-based id so we never block.
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}

// SaveSession persists the agent's conversation to disk under a fresh (or
// existing) session id, and returns that id. If a.SessionID is already set
// (a resumed session), it reuses it so resume-of-resume stays one thread.
func SaveSession(a *Agent) (string, error) {
	id := a.SessionID
	if id == "" {
		id = newSessionID()
		a.SessionID = id
	}
	now := time.Now()
	// If resuming, preserve the original CreatedAt.
	created := now
	if a.SessionCreatedAt != nil {
		created = *a.SessionCreatedAt
	}
	s := Session{
		ID:        id,
		Workdir:   a.Workdir,
		RepoRoot:  a.RepoRoot,
		Intent:    a.Intent,
		Model:     a.Config.Model,
		Messages:  a.Messages,
		CreatedAt: created,
		UpdatedAt: now,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(sessionsDir(), id+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// LoadSession reads a saved session by id. Returns an error if not found.
func LoadSession(id string) (*Session, error) {
	path := filepath.Join(sessionsDir(), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", id, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session %s corrupt: %w", id, err)
	}
	return &s, nil
}

// ListSessions returns all saved sessions, newest first.
func ListSessions() ([]Session, error) {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if json.Unmarshal(data, &s) == nil {
			sessions = append(sessions, s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// DeleteSession removes a saved session file (best-effort).
func DeleteSession(id string) error {
	path := filepath.Join(sessionsDir(), id+".json")
	return os.Remove(path)
}
