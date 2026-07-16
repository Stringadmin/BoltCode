package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSessionMaxFiles      = 500
	defaultSessionRetentionDays = 180
)

type sessionCleanupPolicy struct {
	MaxFiles int
	MaxAge   time.Duration
}

type savedSessionFile struct {
	id      string
	path    string
	updated time.Time
}

// sessionCleanupPolicyFromEnv keeps conservative defaults while allowing
// users with archival or constrained-storage needs to tune either boundary.
// Setting a value to zero disables that boundary.
func sessionCleanupPolicyFromEnv() sessionCleanupPolicy {
	maxFiles := cleanupEnvInt("BOLTCODE_SESSION_MAX", defaultSessionMaxFiles)
	retentionDays := cleanupEnvInt("BOLTCODE_SESSION_RETENTION_DAYS", defaultSessionRetentionDays)
	return sessionCleanupPolicy{
		MaxFiles: maxFiles,
		MaxAge:   time.Duration(retentionDays) * 24 * time.Hour,
	}
}

func cleanupEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

// cleanupSessions removes expired sessions and then enforces the newest-first
// file cap. Corrupt or unrelated files are deliberately ignored rather than
// destroyed, and keepID is never removed.
func cleanupSessions(now time.Time, keepID string, policy sessionCleanupPolicy) (int, error) {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return 0, err
	}
	files := make([]savedSessionFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validSessionID(id) {
			continue
		}
		path := filepath.Join(sessionsDir(), entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var session Session
		if json.Unmarshal(data, &session) != nil {
			continue
		}
		updated := session.UpdatedAt
		if updated.IsZero() {
			if info, statErr := entry.Info(); statErr == nil {
				updated = info.ModTime()
			}
		}
		files = append(files, savedSessionFile{id: id, path: path, updated: updated})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].updated.After(files[j].updated)
	})

	removed := 0
	kept := 0
	for _, file := range files {
		if file.id == keepID {
			kept++
			continue
		}
		expired := policy.MaxAge > 0 && !file.updated.IsZero() && now.Sub(file.updated) > policy.MaxAge
		overLimit := policy.MaxFiles > 0 && kept >= policy.MaxFiles
		if !expired && !overLimit {
			kept++
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
