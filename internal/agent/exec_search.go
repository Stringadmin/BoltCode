package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func (a *Agent) toolSearchCode(pattern, path, glob string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("invalid search pattern: %v", err), true
	}

	maxMatches := a.toolResultItemLimit(48, 100, 1_000)
	var matches []string
	truncated := false
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".boltcode":
				return filepath.SkipDir
			}
			return nil
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(a.Workdir, p)
		if relErr != nil {
			rel = p
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
				if len(matches) >= maxMatches {
					truncated = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Sprintf("search failed: %v", walkErr), true
	}
	if len(matches) == 0 {
		return "no matches found", false
	}
	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (truncated at %d matches for the active context window)", maxMatches)
	}
	return result, false
}

func (a *Agent) toolListFiles(path string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	maxFiles := a.toolResultItemLimit(32, 100, 2_000)
	var files []string
	truncated := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".boltcode" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(a.Workdir, p)
		files = append(files, rel)
		return nil
	})
	result := strings.Join(files, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (listing truncated at %d files)", maxFiles)
	}
	return result, false
}

// toolGlob finds files matching a glob pattern. Supports ** for recursive
// directory matching (e.g. "**/*.go", "src/**/*.ts"). Excludes .git,
// node_modules, and .boltcode directories from results.
func (a *Agent) toolGlob(pattern, path string) (string, bool) {
	dir := a.Workdir
	if path != "" {
		full, err := safeJoin(a.Workdir, path)
		if err != nil {
			return err.Error(), true
		}
		dir = full
	}
	regex, err := globToRegex(pattern)
	if err != nil {
		return fmt.Sprintf("invalid pattern %q: %v", pattern, err), true
	}
	maxFiles := a.toolResultItemLimit(32, 100, 2_000)
	var matches []string
	truncated := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".boltcode" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(a.Workdir, p)
		rel = filepath.ToSlash(rel)
		if regex.MatchString(rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if len(matches) == 0 {
		return "no files matched pattern: " + pattern, false
	}
	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (results truncated at %d files)", maxFiles)
	}
	return result, false
}

// globToRegex converts a glob pattern to a regular expression. Supports:
//   - ** : matches any sequence of characters including path separators
//   - *  : matches any sequence of non-separator characters
//   - ?  : matches any single non-separator character
//   - .  : literal dot (escaped)
func globToRegex(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(pattern)
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch {
		case i+2 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*' && pattern[i+2] == '/':
			// A globstar followed by a slash means zero or more complete
			// directories. Keeping the slash inside the optional group makes
			// **/*.go match both root.go and nested/root.go, while preventing
			// src/**/test from accidentally matching src/contest.
			b.WriteString("(?:.*/)?")
			i += 3
		case i+1 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*':
			b.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
