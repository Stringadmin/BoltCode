// Package sshx handles SSH operations internally so users never need to leave
// BoltCode to configure their server. It connects with password, copies the
// public key, and runs remote commands — all from within the interactive session.
package sshx

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Config holds the info needed to connect to the user's git server.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	RepoPath string
}

// knownHostsPath returns the path to the user's known_hosts file.
func knownHostsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

// tofuHostKeyCallback implements Trust On First Use: if the host is unknown it
// accepts the key and appends it to known_hosts; if the key has changed (MITM)
// it rejects the connection.
func tofuHostKeyCallback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		khPath := knownHostsPath()

		// Ensure ~/.ssh exists (0o700: SSH refuses world-readable ~/.ssh).
		if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
			return fmt.Errorf("cannot create ~/.ssh: %w", err)
		}

		// Try loading existing known_hosts
		if _, err := os.Stat(khPath); err == nil {
			cb, err := knownhosts.New(khPath)
			if err == nil {
				err = cb(hostname, remote, key)
				if err == nil {
					return nil // known host, key matches
				}
				// Check if it's a "key changed" error vs "host unknown"
				var keyErr *knownhosts.KeyError
				if isKeyError(err, &keyErr) && len(keyErr.Want) > 0 {
					return fmt.Errorf("HOST KEY CHANGED for %s — possible MITM attack. Remove the old key from %s to proceed", hostname, khPath)
				}
				// Host unknown — fall through to TOFU
			}
		}

		// TOFU: accept and record the key. 0o600 so hostnames/keys aren't
		// world-readable on shared machines.
		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
		f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("cannot write known_hosts: %w", err)
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, line)
		return err
	}
}

// isKeyError checks if err is a knownhosts.KeyError and extracts it.
func isKeyError(err error, target **knownhosts.KeyError) bool {
	if ke, ok := err.(*knownhosts.KeyError); ok {
		*target = ke
		return true
	}
	return false
}

// Connect establishes an SSH connection using password auth with TOFU host key
// verification (Trust On First Use: unknown hosts are accepted and recorded;
// changed keys are rejected to prevent MITM attacks).
func Connect(cfg *Config) (*ssh.Client, error) {
	sshCfg := &ssh.ClientConfig{
		User: cfg.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(cfg.Password),
		},
		HostKeyCallback: tofuHostKeyCallback(),
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	return ssh.Dial("tcp", addr, sshCfg)
}

// RunCommand executes a command on the remote server and returns its output.
func RunCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	return strings.TrimSpace(string(out)), err
}

// CopyPublicKey reads the local public key and appends it to the server's
// authorized_keys. The key is piped via stdin to avoid shell injection — no part
// of the key content is ever interpolated into a command string.
func CopyPublicKey(client *ssh.Client) error {
	home, _ := os.UserHomeDir()
	pubPath := filepath.Join(home, ".ssh", "id_ed25519.pub")
	pubKey, err := os.ReadFile(pubPath)
	if err != nil {
		return fmt.Errorf("no public key at %s: %w", pubPath, err)
	}
	key := strings.TrimSpace(string(pubKey))

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("opening session: %w", err)
	}
	defer session.Close()

	// Pipe the key via stdin; the remote script reads it safely
	session.Stdin = strings.NewReader(key + "\n")
	cmd := `mkdir -p ~/.ssh && chmod 700 ~/.ssh && ` +
		`KEY=$(head -n1) && ` +
		`grep -qF "$KEY" ~/.ssh/authorized_keys 2>/dev/null || echo "$KEY" >> ~/.ssh/authorized_keys && ` +
		`chmod 600 ~/.ssh/authorized_keys`

	return session.Run(cmd)
}

// InitBareRepo creates a bare git repository at the given path on the server.
// repoPath is user-supplied, so it is validated against shell metacharacters
// before being interpolated into the remote command — otherwise a path like
// "x; rm -rf ~" would execute on the server.
func InitBareRepo(client *ssh.Client, repoPath string) (string, error) {
	if err := SafeRemotePath(repoPath); err != nil {
		return "", err
	}
	return RunCommand(client, fmt.Sprintf("git init --bare %s 2>&1", ShellQuote(repoPath)))
}

// SafeRemotePath rejects paths containing characters that could break out of
// the shell command context. Bare-repo paths are filesystem paths, so we allow
// alphanumerics, path separators, dots, dashes, underscores, tilde and spaces —
// anything else (semicolons, backticks, $, etc.) is refused.
//
// Exported so command/remote.go (which builds its own SSH command strings for
// InitBareRepo) can share the same validation instead of duplicating it.
func SafeRemotePath(p string) error {
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '/', r == '\\', r == '.', r == '-', r == '_', r == '~', r == ' ':
			// allowed
		default:
			return fmt.Errorf("unsafe character %q in repo path %q", r, p)
		}
	}
	return nil
}

// ShellQuote wraps s in single quotes for safe shell interpolation, escaping
// any embedded single quotes. Used as defense in depth alongside SafeRemotePath.
// Exported for the same reason as SafeRemotePath.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestConnection verifies the SSH connection is alive.
func TestConnection(client *ssh.Client) error {
	out, err := RunCommand(client, "echo ok")
	if err != nil {
		return err
	}
	if !strings.Contains(out, "ok") {
		return fmt.Errorf("unexpected response: %s", out)
	}
	return nil
}
