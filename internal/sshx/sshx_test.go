package sshx

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSafeRemotePathValid(t *testing.T) {
	cases := []string{
		"/home/git/repo.git",
		"~/repos/my-project",
		"/var/git/my_repo",
		"/path/with spaces",
		"/simple",
		".",
	}
	for _, c := range cases {
		if err := SafeRemotePath(c); err != nil {
			t.Errorf("SafeRemotePath(%q) = %v, want nil", c, err)
		}
	}
}

func TestSafeRemotePathInvalid(t *testing.T) {
	cases := []string{
		"x; rm -rf ~",
		"`reboot`",
		"$(cat /etc/passwd)",
		"foo|bar",
		"foo>bar",
		"foo<bar",
		"foo&bar",
		"foo'bar",
		`foo"bar`,
	}
	for _, c := range cases {
		if err := SafeRemotePath(c); err == nil {
			t.Errorf("SafeRemotePath(%q) = nil, want error", c)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"hello", "'hello'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"simple path", "'simple path'"},
		{"slash/here", "'slash/here'"},
	}
	for _, c := range cases {
		got := ShellQuote(c.input)
		if got != c.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestIsKeyError(t *testing.T) {
	// nil is not a *knownhosts.KeyError
	var target *knownhosts.KeyError
	if isKeyError(nil, &target) {
		t.Error("isKeyError(nil) = true, want false")
	}
	if target != nil {
		t.Errorf("target should be nil, got %v", target)
	}
}

func TestKnownHostsPath(t *testing.T) {
	path := knownHostsPath()
	if path == "" {
		t.Error("knownHostsPath() returned empty string")
	}
	// Should end with .ssh/known_hosts (OS-appropriate separators)
	if !strings.HasSuffix(path, ".ssh/known_hosts") && !strings.HasSuffix(path, `.ssh\known_hosts`) {
		t.Errorf("knownHostsPath() = %q, expected suffix .ssh/known_hosts", path)
	}
}
