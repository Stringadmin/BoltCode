package command

import (
	"os/exec"
	"testing"
)

// setupRepo creates a real git repo + boltcode init in a temp dir, returning
// an App ready to test mode transitions against the actual config file.
func setupRepo(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "t@t.test")
	mustRunGit(t, dir, "config", "user.name", "tester")
	mustRunGit(t, dir, "commit", "--allow-empty", "-m", "init")

	if err := Init(dir, "tester"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	app, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Close the store's lock handle before the temp dir is removed (Windows
	// can't delete an open file). LIFO cleanup ordering runs this first.
	t.Cleanup(func() { app.Store.Close() })
	return app
}

func TestSoloByDefault(t *testing.T) {
	app := setupRepo(t)
	if app.IsTeamMode() {
		t.Fatal("freshly initialized repo should be solo mode, not team")
	}
	if err := app.RequireTeam("task"); err == nil {
		t.Fatal("RequireTeam should error in solo mode")
	}
}

func TestTeamInitUpgradesMode(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "t@t.test")
	mustRunGit(t, dir, "config", "user.name", "tester")
	mustRunGit(t, dir, "commit", "--allow-empty", "-m", "init")
	if err := Init(dir, "tester"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := TeamInit(dir, "the-team"); err != nil {
		t.Fatalf("TeamInit: %v", err)
	}
	app, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { app.Store.Close() })
	if !app.IsTeamMode() {
		t.Fatal("expected team mode after TeamInit")
	}
	if err := app.RequireTeam("task"); err != nil {
		t.Fatalf("RequireTeam should pass in team mode, got: %v", err)
	}
	cfg, _ := app.LoadConfig()
	if cfg.Team != "the-team" {
		t.Errorf("team name = %q, want the-team", cfg.Team)
	}
}

func TestTeamInitIdempotentFails(t *testing.T) {
	app := setupRepo(t)
	dir := app.Repo.Root
	if err := TeamInit(dir, ""); err != nil {
		t.Fatalf("first TeamInit: %v", err)
	}
	if err := TeamInit(dir, ""); err == nil {
		t.Fatal("second TeamInit should fail (already in team mode)")
	}
}

func TestTeamInitRequiresInitFirst(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	// No boltcode init yet.
	if err := TeamInit(dir, ""); err == nil {
		t.Fatal("TeamInit should fail when boltcode not initialized")
	}
}

// mustRunGit runs git in dir, failing the test on error.
func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
