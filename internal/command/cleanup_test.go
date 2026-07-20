package command

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTeamTask builds a temp repo in team mode with one task whose branch has
// one commit beyond the base, made inside the task's worktree. It returns the
// loaded app and the task ID.
func setupTeamTask(t *testing.T) (*App, string) {
	t.Helper()
	app := setupRepo(t) // solo by default — upgrade below
	dir := app.Repo.Root
	if err := TeamInit(dir, ""); err != nil {
		t.Fatalf("TeamInit: %v", err)
	}
	// Reload so the app picks up team mode from the updated config.
	app, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { app.Store.Close() })

	task, err := app.TaskNew("cleanup test task", "builtin", "")
	if err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	app.ResolveWorktree(task)
	if task.Worktree == "" {
		t.Fatal("task worktree not resolved")
	}
	if err := os.WriteFile(filepath.Join(task.Worktree, "work.txt"), []byte("work"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	mustRunGit(t, task.Worktree, "add", "-A")
	mustRunGit(t, task.Worktree, "commit", "-m", "task work")
	return app, task.ID
}

// A merged task's branch commits live in the base branch, so merge must remove
// the now-redundant worktree checkout and task branch instead of letting them
// accumulate. This is also the regression guard for the worktree path never
// being re-derived after a task is loaded from disk.
func TestApproveAndMergeCleansUpWorkspace(t *testing.T) {
	app, taskID := setupTeamTask(t)

	task, err := app.Store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	app.ResolveWorktree(task)
	worktree := task.Worktree
	branch := task.Branch

	p, err := app.Submit(taskID, "done", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := app.ApproveAndMerge(p.ID, "tester"); err != nil {
		t.Fatalf("ApproveAndMerge: %v", err)
	}

	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("worktree still present after merge: %s", worktree)
	}
	if app.Repo.BranchExists(branch) {
		t.Errorf("task branch %s still exists after merge", branch)
	}
}

// Abandoning a task discards it, so the worktree checkout and the
// boltcode-owned branch must both go. Proposals/reviews metadata stays.
func TestTaskAbandonCleansUpWorkspace(t *testing.T) {
	app, taskID := setupTeamTask(t)

	task, err := app.Store.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	app.ResolveWorktree(task)
	worktree := task.Worktree
	branch := task.Branch

	if _, err := app.TaskAbandon(taskID); err != nil {
		t.Fatalf("TaskAbandon: %v", err)
	}

	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("worktree still present after abandon: %s", worktree)
	}
	if app.Repo.BranchExists(branch) {
		t.Errorf("task branch %s still exists after abandon", branch)
	}
}
