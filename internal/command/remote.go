package command

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RemoteConfig stores the user's self-hosted git server settings.
type RemoteConfig struct {
	URL  string `json:"url"`  // e.g. "root@your-server.com:boltcode.git"
	Name string `json:"name"` // git remote name, default "origin"
}

// hostedPlatforms are well-known code-hosting domains whose repos already exist
// server-side, so the self-hosted setup wizard (bare-repo init, key copy) does
// not apply to them.
var hostedPlatforms = map[string]bool{
	"github.com":    true,
	"gitlab.com":    true,
	"bitbucket.org": true,
	"gitee.com":     true,
	"codeberg.org":  true,
}

// IsHostedPlatform reports whether url points at a known code-hosting service
// (GitHub, GitLab, …) rather than a self-hosted git server. Such platforms
// already have the repo and an account system, so BoltCode skips the
// bare-repo initialization and SSH-key-copy steps it performs for self-hosted
// servers.
//
// It must be called BEFORE ParseRemoteURL, because ParseRemoteURL rejects
// https:// URLs (no user@host:path shape) and would error first.
func IsHostedPlatform(url string) bool {
	url = strings.TrimSpace(url)
	// HTTPS (or HTTP) transport → always a hosted platform.
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return true
	}
	// SSH form git@github.com:user/repo.git → check the host against known domains.
	if at := strings.Index(url, "@"); at >= 0 {
		rest := url[at+1:]
		if colon := strings.Index(rest, ":"); colon >= 0 {
			host := rest[:colon]
			return hostedPlatforms[host]
		}
	}
	return false
}

func remoteConfigPath(app *App) string {
	return filepath.Join(app.Repo.Root, ".boltcode", "remote.json")
}

func (a *App) LoadRemoteConfig() (*RemoteConfig, error) {
	var rc RemoteConfig
	data, err := os.ReadFile(remoteConfigPath(a))
	if err != nil {
		return nil, fmt.Errorf("no remote configured — run: boltcode remote add <user@host:path>")
	}
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, err
	}
	return &rc, nil
}

func (a *App) SaveRemoteConfig(rc *RemoteConfig) error {
	data, _ := json.MarshalIndent(rc, "", "  ")
	return os.WriteFile(remoteConfigPath(a), data, 0o644)
}

// RemoteAdd records the server URL and registers it as a git remote.
func (a *App) RemoteAdd(url, name string) error {
	if name == "" {
		name = "origin"
	}
	rc := &RemoteConfig{URL: url, Name: name}
	if err := a.SaveRemoteConfig(rc); err != nil {
		return err
	}
	a.Repo.Run("remote", "remove", name)
	if _, err := a.Repo.Run("remote", "add", name, url); err != nil {
		return fmt.Errorf("git remote add failed: %w", err)
	}
	return nil
}

// SSHKeyPath returns the default SSH key path for the current OS.
func SSHKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "id_ed25519")
}

// EnsureSSHKey checks if an SSH key exists; if not, generates one. Returns the
// public key content so the user can copy it to the server.
func EnsureSSHKey() (pubKey string, generated bool, err error) {
	keyPath := SSHKeyPath()
	pubPath := keyPath + ".pub"

	if data, err := os.ReadFile(pubPath); err == nil {
		return strings.TrimSpace(string(data)), false, nil
	}

	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("creating ~/.ssh: %w", err)
	}

	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-C", "boltcode")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", false, fmt.Errorf("ssh-keygen failed: %w", err)
	}

	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

// ParseRemoteURL extracts user and host from a git remote URL like
// "root@your-server.com:boltcode.git" → ("root", "your-server.com", "boltcode.git")
func ParseRemoteURL(url string) (user, host, path string, err error) {
	at := strings.Index(url, "@")
	colon := strings.LastIndex(url, ":")
	if at < 0 || colon < 0 || colon <= at {
		return "", "", "", fmt.Errorf("invalid remote URL %q (expected user@host:path)", url)
	}
	return url[:at], url[at+1 : colon], url[colon+1:], nil
}

// TestSSH tries to connect to the server and run a simple command. Returns nil
// if the connection succeeds.
func TestSSH(user, host string) error {
	cmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10", user+"@"+host, "echo ok")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w\n%s", err, string(out))
	}
	return nil
}

// InitBareRepo creates a bare git repository on the remote server via SSH.
// path is user-supplied and validated for shell metacharacters before being
// sent to the remote shell, to prevent command injection.
func InitBareRepo(user, host, path string) error {
	if err := safeRemotePath(path); err != nil {
		return err
	}
	cmd := exec.Command("ssh", user+"@"+host,
		fmt.Sprintf("git init --bare %s 2>&1 || echo 'already exists'", shellQuoteRemote(path)))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// safeRemotePath rejects shell metacharacters in a remote repo path.
func safeRemotePath(p string) error {
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

// shellQuoteRemote single-quotes a path for safe shell interpolation.
func shellQuoteRemote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Push pushes the current branch to the configured remote.
func (a *App) Push(force bool) error {
	rc, err := a.LoadRemoteConfig()
	if err != nil {
		return err
	}
	branch, err := a.Repo.CurrentBranch()
	if err != nil {
		return err
	}
	args := []string{"push", "-u", rc.Name, branch}
	if force {
		args = []string{"push", "--force-with-lease", "-u", rc.Name, branch}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = a.Repo.Root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Pull fetches and merges from the configured remote.
func (a *App) Pull() error {
	rc, err := a.LoadRemoteConfig()
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "pull", rc.Name)
	cmd.Dir = a.Repo.Root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyIDHint returns the platform-appropriate command for the user to copy
// their public key to the server.
func CopyIDHint(user, host string) string {
	pubPath := SSHKeyPath() + ".pub"
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(
			"type \"%s\" | ssh %s@%s \"mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys\"",
			filepath.ToSlash(pubPath), user, host)
	}
	return fmt.Sprintf("ssh-copy-id -i %s %s@%s", pubPath, user, host)
}
