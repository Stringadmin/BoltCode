package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		{"absolute path inside workdir", filepath.Join(workdir, "main.go"), false},
		{"traversal blocked", "../../../etc/passwd", true},
		{"traversal in middle", "src/../../secret.txt", true},
		{"dot-dot only", "..", true},
	}

	// An absolute path outside the workdir must still be rejected.
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name    string
			path    string
			wantErr bool
		}{"unix absolute outside blocked", "/etc/passwd", true})
	}

	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name    string
			path    string
			wantErr bool
		}{"windows absolute outside blocked", `D:\other\file.txt`, true})
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

func TestToolReadFileAcceptsAbsolutePathInsideWorkdir(t *testing.T) {
	a := newAgentInTemp(t)
	path := filepath.Join(a.Workdir, "main.go")
	writeFile(t, path, "package main\n")

	content, isErr := a.toolReadFile(path, "", "")
	if isErr {
		t.Fatalf("absolute in-workdir path should be accepted: %s", content)
	}
	if !strings.Contains(content, "package main") {
		t.Fatalf("content = %q", content)
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

func TestCopyThenDeletePreservesFilePermissions(t *testing.T) {
	a := newAgentInTemp(t)
	source := filepath.Join(a.Workdir, "source.sh")
	destination := filepath.Join(a.Workdir, "nested", "destination.sh")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("#!/bin/sh\necho ok\n"), 0o750); err != nil {
		t.Fatal(err)
	}

	message, isErr := a.copyThenDelete(source, destination, "source.sh", "nested/destination.sh")
	if isErr {
		t.Fatalf("copyThenDelete failed: %s", message)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "#!/bin/sh\necho ok\n" {
		t.Fatalf("destination content = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o750 {
			t.Fatalf("destination permissions = %o, want 750", got)
		}
	}
}

func TestGlobToRegexPreservesGlobstarDirectoryBoundaries(t *testing.T) {
	tests := []struct {
		pattern string
		matches []string
		rejects []string
	}{
		{
			pattern: "src/**/test/*.go",
			matches: []string{"src/test/a.go", "src/unit/test/a.go", "src/a/b/test/a.go"},
			rejects: []string{"src/contest/a.go", "src/test/nested/a.go", "other/src/test/a.go"},
		},
		{
			pattern: "**/*.go",
			matches: []string{"main.go", "internal/agent/exec.go"},
			rejects: []string{"main.ts"},
		},
		{
			pattern: "files/file[1].go",
			matches: []string{"files/file[1].go"},
			rejects: []string{"files/file1.go"},
		},
	}
	for _, test := range tests {
		t.Run(test.pattern, func(t *testing.T) {
			re, err := globToRegex(test.pattern)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range test.matches {
				if !re.MatchString(path) {
					t.Errorf("%q should match %q (regex %q)", test.pattern, path, re.String())
				}
			}
			for _, path := range test.rejects {
				if re.MatchString(path) {
					t.Errorf("%q should not match %q (regex %q)", test.pattern, path, re.String())
				}
			}
		})
	}
}

// --- symlink escape protection (P0) ---
// A symlink inside the worktree pointing outside must not let a tool escape
// the sandbox. Before the fix, safeJoin only did a string-based ".." check and
// happily followed a symlink to /etc (or a sibling temp dir).

func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows; tested on Unix")
	}
	a := newAgentInTemp(t)

	// outsideTarget lives outside the workdir (sibling temp dir).
	outsideTarget := t.TempDir()
	secret := filepath.Join(outsideTarget, "secret.txt")
	writeFile(t, secret, "top secret")

	// Plant a symlink inside the worktree pointing at the outside target.
	link := filepath.Join(a.Workdir, "evil")
	if err := os.Symlink(outsideTarget, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Reading through the symlink must be refused.
	if _, isErr := a.toolReadFile("evil/secret.txt", "", ""); !isErr {
		t.Fatal("toolReadFile must reject symlink that escapes workdir")
	}
	// Writing through the symlink must also be refused.
	if _, isErr := a.toolWriteFile("evil/leaked.txt", "exfiltrated"); !isErr {
		t.Fatal("toolWriteFile must reject symlink that escapes workdir")
	}
	// And the secret must be untouched.
	got, _ := os.ReadFile(secret)
	if string(got) != "top secret" {
		t.Fatalf("secret file modified via symlink escape: %q", string(got))
	}
}

func TestSafeJoinAllowsSymlinkInsideWorkdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows; tested on Unix")
	}
	a := newAgentInTemp(t)
	// A symlink that points to another location INSIDE the workdir is fine.
	writeFile(t, filepath.Join(a.Workdir, "real.txt"), "data")
	if err := os.Symlink("real.txt", filepath.Join(a.Workdir, "alias")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, isErr := a.toolReadFile("alias", "", ""); isErr {
		t.Fatal("toolReadFile should allow an intra-workdir symlink")
	}
}

// --- destructive-command denylist (P2) ---
// Even under AutoAllow, catastrophic commands must be flagged for confirmation.

func TestIsDestructive(t *testing.T) {
	bad := []string{
		"rm -rf /",
		"rm -rf /home",
		"rm -rf ~",
		"rm -rf $HOME",
		"sudo rm -rf /var",
		"git push --force origin main",
		"git push -f origin master",
		":(){ :|:& };:",
		"dd if=/dev/zero of=/dev/sda",
		"mkfs.ext4 /dev/sda1",
		"chmod -R 777 /",
	}
	for _, cmd := range bad {
		if !isDestructive(cmd) {
			t.Errorf("isDestructive(%q) = false, want true", cmd)
		}
	}
	good := []string{
		"rm -rf build/",                       // scoped to a dir, not root/home
		"git push origin main",                // non-force push
		"git push --force origin feature/xyz", // force-push to a feature branch
		"npm test",
		"go build ./...",
		"rm file.txt",
		"chmod 644 config.yml",
	}
	for _, cmd := range good {
		if isDestructive(cmd) {
			t.Errorf("isDestructive(%q) = true, want false (false positive)", cmd)
		}
	}
}
