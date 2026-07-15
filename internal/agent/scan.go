package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// scanProject gathers project context for the AI at session start.
// Returns a structured text summary of the project. Defaults to the agent's
// worktree; scanProjectAt lets /triage target the main repo root instead.
func (a *Agent) scanProject() string {
	return a.scanProjectAt(a.Workdir)
}

// scanProjectAt gathers project context rooted at root.
func (a *Agent) scanProjectAt(root string) string {
	var parts []string

	// 1. File tree (top 2 levels)
	tree := a.getFileTreeAt(root)
	if tree != "" {
		parts = append(parts, "## Project Structure\n```\n"+tree+"\n```")
	}

	// 2. Detect project type and key config
	if content := a.readIfExistsAt(root, "package.json"); content != "" {
		parts = append(parts, "## package.json\n```json\n"+truncateStr(content, 800, "\n... (truncated)")+"\n```")
	}
	if content := a.readIfExistsAt(root, "go.mod"); content != "" {
		parts = append(parts, "## go.mod\n```\n"+truncateStr(content, 500, "\n... (truncated)")+"\n```")
	}
	if content := a.readIfExistsAt(root, "Cargo.toml"); content != "" {
		parts = append(parts, "## Cargo.toml\n```toml\n"+truncateStr(content, 500, "\n... (truncated)")+"\n```")
	}
	if content := a.readIfExistsAt(root, "pom.xml"); content != "" {
		parts = append(parts, "## pom.xml (first 500 chars)\n```xml\n"+truncateStr(content, 500, "\n... (truncated)")+"\n```")
	}
	if content := a.readIfExistsAt(root, "requirements.txt"); content != "" {
		parts = append(parts, "## requirements.txt\n```\n"+truncateStr(content, 500, "\n... (truncated)")+"\n```")
	}
	if content := a.readIfExistsAt(root, "pyproject.toml"); content != "" {
		parts = append(parts, "## pyproject.toml\n```toml\n"+truncateStr(content, 500, "\n... (truncated)")+"\n```")
	}

	// 3. Spec file
	if content := a.readIfExistsAt(root, ".boltcode-spec.md"); content != "" {
		parts = append(parts, "## Project Spec\n"+truncateStr(content, 1000, "\n... (truncated)"))
	}

	if len(parts) == 0 {
		return "(empty project — no files found)"
	}
	return strings.Join(parts, "\n\n")
}

// getFileTree returns a compact file listing (top 2 levels, max 50 entries),
// implemented in pure Go so it works identically on every OS.
func (a *Agent) getFileTree() string {
	return a.getFileTreeAt(a.Workdir)
}

// skipDirs are directories never descended into during a project scan. They
// hold build output, dependencies, or editor state — large, noisy, and never
// useful to the AI. Centralized here so the scan and the file-tree listing
// share one list.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".boltcode": true,
	"vendor": true, "dist": true, "build": true, "target": true,
	".next": true, ".nuxt": true, ".output": true,
	"__pycache__": true, ".venv": true, "venv": true, ".pytest_cache": true,
	".idea": true, ".vscode": true,
	"coverage": true, ".cache": true,
}

// getFileTreeAt returns a compact file listing rooted at root. It prunes skip
// dirs and short-circuits the walk as soon as 50 entries are collected (using
// filepath.SkipAll), so a 100k-file repo doesn't build a huge slice before
// truncating.
func (a *Agent) getFileTreeAt(root string) string {
	const maxEntries = 50
	var entries []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Skip noise dirs entirely.
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
		}
		// Limit to two levels deep.
		if strings.Count(rel, "/") > 1 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Short-circuit once we have enough entries — don't keep walking.
		if len(entries) >= maxEntries {
			return filepath.SkipAll
		}
		entries = append(entries, rel)
		return nil
	})
	if walkErr != nil || len(entries) == 0 {
		return ""
	}
	if len(entries) >= maxEntries {
		// The walk was cut short; note that there may be more.
		entries = append(entries, fmt.Sprintf("... (listing truncated at %d files)", maxEntries))
	}
	return strings.Join(entries, "\n")
}

// readIfExists reads a file from the worktree if it exists.
func (a *Agent) readIfExists(name string) string {
	return a.readIfExistsAt(a.Workdir, name)
}

// readIfExistsAt reads a file from root if it exists.
func (a *Agent) readIfExistsAt(root, name string) string {
	path := filepath.Join(root, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// truncateStr truncates s to at most max runes, appending suffix if truncation
// occurred. It is rune-safe so multi-byte characters (CJK, emoji) aren't split
// mid-glyph. This is the single truncate helper for the agent package; the
// per-call suffix lets each caller pick its display convention ("..." vs
// "\n... (truncated)") without duplicating the slicing logic.
func truncateStr(s string, max int, suffix string) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + suffix
	}
	return s
}
