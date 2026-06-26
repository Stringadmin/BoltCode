package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	workdir := "/home/user/project"
	if runtime.GOOS == "windows" {
		workdir = `C:\Users\user\project`
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"simple file", "main.go", false},
		{"nested path", "src/utils/helper.go", false},
		{"dot prefix", "./README.md", false},
		{"traversal blocked", "../../../etc/passwd", true},
		{"traversal in middle", "src/../../secret.txt", true},
		{"dot-dot only", "..", true},
	}

	// On Unix, /etc/passwd is absolute and must be rejected.
	// On Windows, it's relative (resolves inside workdir) so it's safe.
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name    string
			path    string
			wantErr bool
		}{"unix absolute path blocked", "/etc/passwd", true})
	}

	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name    string
			path    string
			wantErr bool
		}{"windows absolute blocked", `D:\other\file.txt`, true})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeJoin(workdir, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeJoin(%q, %q) error = %v, wantErr %v",
					workdir, tt.path, err, tt.wantErr)
			}
		})
	}
}

// newAgentInTemp sets up an Agent whose Workdir is a throwaway temp dir, so
// file-backed tools can be exercised against real files.
func newAgentInTemp(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	return &Agent{Workdir: dir}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestToolEditFileUniqueMatch(t *testing.T) {
	a := newAgentInTemp(t)
	path := filepath.Join(a.Workdir, "a.txt")
	writeFile(t, path, "foo\nbar\nbaz\n")

	msg, isErr := a.toolEditFile("a.txt", "bar", "BAR")
	if isErr {
		t.Fatalf("expected success, got error: %s", msg)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "foo\nBAR\nbaz\n" {
		t.Fatalf("file content = %q, want %q", string(got), "foo\nBAR\nbaz\n")
	}
}

func TestToolEditFileAmbiguousMatchRejected(t *testing.T) {
	// old_string appears twice — the tool must refuse rather than silently
	// editing the first occurrence (which could be the wrong line).
	a := newAgentInTemp(t)
	path := filepath.Join(a.Workdir, "a.txt")
	writeFile(t, path, "dup\ndup\nother\n")

	_, isErr := a.toolEditFile("a.txt", "dup", "X")
	if !isErr {
		t.Fatalf("expected ambiguous-match error, got success")
	}

	// File must be left untouched on rejection.
	got, _ := os.ReadFile(path)
	if string(got) != "dup\ndup\nother\n" {
		t.Fatalf("file modified despite ambiguous match: %q", string(got))
	}
}

func TestToolEditFileNotFound(t *testing.T) {
	a := newAgentInTemp(t)
	path := filepath.Join(a.Workdir, "a.txt")
	writeFile(t, path, "hello\n")

	_, isErr := a.toolEditFile("a.txt", "missing", "X")
	if !isErr {
		t.Fatalf("expected not-found error, got success")
	}
}
