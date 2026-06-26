package command

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/model"
)

// DetectConflicts checks if any in-progress tasks have overlapping files_changed.
// Returns a list of warnings for the user.
func (a *App) DetectConflicts() []string {
	tasks, err := a.Store.ListTasks()
	if err != nil {
		return nil
	}

	// Only check active tasks
	var active []*model.Task
	for _, t := range tasks {
		if t.Status == model.TaskInProgress || t.Status == model.TaskReview {
			if len(t.FilesChanged) > 0 {
				active = append(active, t)
			}
		}
	}

	var warnings []string
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			overlap := fileOverlap(active[i].FilesChanged, active[j].FilesChanged)
			if len(overlap) > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"⚠ conflict risk: %s (%s) and %s (%s) both touch: %s",
					active[i].ID, active[i].Assignee,
					active[j].ID, active[j].Assignee,
					strings.Join(overlap, ", ")))
			}
		}
	}
	return warnings
}

// normalizeFile returns a canonical key for a path so the same file written with
// different separators ("src/a.go" vs "src\a.go") or casing still compares equal.
// Paths are cleaned, slash-normalized, and lowercased — purely for the overlap
// set; the original spelling is what we report back to the user.
func normalizeFile(p string) string {
	p = filepath.Clean(p)
	p = filepath.ToSlash(p)
	return strings.ToLower(p)
}

// fileOverlap returns the files (using task a's spelling) that both slices
// touch, comparing on a normalized key so separator/case differences don't
// cause false negatives.
func fileOverlap(a, b []string) []string {
	// Map normalized key -> original spelling from a, so the warning shows the
	// path the team would recognize.
	spell := make(map[string]string, len(a))
	for _, f := range a {
		spell[normalizeFile(f)] = f
	}
	var overlap []string
	seen := make(map[string]bool)
	for _, f := range b {
		key := normalizeFile(f)
		if orig, ok := spell[key]; ok && !seen[key] {
			overlap = append(overlap, orig)
			seen[key] = true
		}
	}
	return overlap
}

