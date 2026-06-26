package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/command"
)

// runRemote handles `remote add`, `remote setup`, `remote show`.
func runRemote(wd string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: boltcode remote <add|setup|show>")
	}

	switch args[0] {
	case "add":
		return runRemoteAdd(wd, args[1:])
	case "setup":
		return runRemoteSetup(wd)
	case "show":
		return runRemoteShow(wd)
	default:
		return fmt.Errorf("unknown remote subcommand %q", args[0])
	}
}

func runRemoteAdd(wd string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: boltcode remote add <url>\n  e.g. git@github.com:user/repo.git  |  https://github.com/user/repo.git  |  user@host:path")
	}
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	name, args := popFlag(args, "--name", "origin")
	url := strings.TrimSpace(strings.Join(args, ""))
	if err := app.RemoteAdd(url, name); err != nil {
		return err
	}
	fmt.Printf("remote %q added: %s\n", name, url)
	if command.IsHostedPlatform(url) {
		// The repo already exists on the platform; no bare-repo init needed.
		fmt.Println("  next step: boltcode remote setup  (configure SSH key / HTTPS auth)")
		fmt.Println("         or: boltcode push  (push directly)")
	} else {
		fmt.Println("  next step: boltcode remote setup")
	}
	return nil
}

func runRemoteShow(wd string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	rc, err := app.LoadRemoteConfig()
	if err != nil {
		return err
	}
	fmt.Printf("remote: %s (%s)\n", rc.URL, rc.Name)
	return nil
}

// runRemoteSetup is the guided flow for setting up access to the remote.
// For self-hosted servers it walks through: key generation → copy to server →
// test → init bare repo. For hosted platforms (GitHub/GitLab/…) the repo
// already exists, so it only covers SSH key + auth guidance.
func runRemoteSetup(wd string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	rc, err := app.LoadRemoteConfig()
	if err != nil {
		return err
	}

	if command.IsHostedPlatform(rc.URL) {
		return runHostedSetup(app, rc)
	}
	return runSelfHostedSetup(rc)
}

// runHostedSetup covers GitHub/GitLab-style remotes. The repo exists already;
// we just make sure an SSH key exists and tell the user how to register it (or
// use HTTPS with a token).
func runHostedSetup(app *command.App, rc *command.RemoteConfig) error {
	fmt.Println("=== BoltCode Remote Setup (hosted platform) ===")
	fmt.Printf("  remote: %s\n\n", rc.URL)

	fmt.Println("[1/2] Checking SSH key...")
	pubKey, generated, err := command.EnsureSSHKey()
	if err != nil {
		return err
	}
	if generated {
		fmt.Println("  generated new SSH key")
	} else {
		fmt.Println("  existing key found")
	}

	fmt.Println()
	fmt.Println("[2/2] Authenticate with the platform.")
	if strings.HasPrefix(rc.URL, "https://") || strings.HasPrefix(rc.URL, "http://") {
		fmt.Println("  Using HTTPS. On first push, git will ask for your username and a")
		fmt.Println("  personal access token (use it as the password).")
	} else {
		fmt.Println("  Using SSH. Add this public key to your account")
		fmt.Println("  (e.g. GitHub → Settings → SSH and GPG keys → New SSH key):")
		fmt.Println()
		fmt.Printf("    %s\n", pubKey)
	}
	fmt.Println()
	fmt.Println("Setup complete! You can now push with: boltcode push")
	return nil
}

// runSelfHostedSetup is the original guided flow for a self-hosted git server.
func runSelfHostedSetup(rc *command.RemoteConfig) error {
	user, host, path, err := command.ParseRemoteURL(rc.URL)
	if err != nil {
		return err
	}

	fmt.Println("=== BoltCode Remote Setup (self-hosted) ===")
	fmt.Printf("  server: %s@%s\n  repo:   %s\n\n", user, host, path)

	// Step 1: SSH key
	fmt.Println("[1/4] Checking SSH key...")
	pubKey, generated, err := command.EnsureSSHKey()
	if err != nil {
		return err
	}
	if generated {
		fmt.Println("  generated new SSH key")
	} else {
		fmt.Println("  existing key found")
	}
	fmt.Printf("  public key: %s\n\n", pubKey)

	// Step 2: Copy key to server
	fmt.Println("[2/4] Copy your public key to the server.")
	fmt.Println("  Run this command (it will ask for your server password once):")
	fmt.Println()
	fmt.Printf("    %s\n\n", command.CopyIDHint(user, host))
	fmt.Print("  Press Enter after you've done this (or 's' to skip if already set up): ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	fmt.Println()

	// Step 3: Test SSH
	fmt.Println("[3/4] Testing SSH connection...")
	if err := command.TestSSH(user, host); err != nil {
		fmt.Printf("  ❌ %v\n", err)
		fmt.Println("  Fix the SSH connection and re-run: boltcode remote setup")
		return err
	}
	fmt.Println("  ✅ SSH connection successful")
	fmt.Println()

	// Step 4: Init bare repo on server
	fmt.Println("[4/4] Initializing bare repository on server...")
	if err := command.InitBareRepo(user, host, path); err != nil {
		return fmt.Errorf("failed to init remote repo: %w", err)
	}
	fmt.Println("  ✅ Remote repository ready")
	fmt.Println()
	fmt.Println("Setup complete! You can now push with: boltcode push")

	if input == "s" || input == "S" {
		_ = input // skip was acknowledged above
	}
	return nil
}

// runPush handles `boltcode push`.
func runPush(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	force := false
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		}
	}
	return app.Push(force)
}

// runPull handles `boltcode pull`.
func runPull(wd string, args []string) error {
	app, err := command.Load(wd)
	if err != nil {
		return err
	}
	return app.Pull()
}
