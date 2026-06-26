package command

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// TaskNew creates a task: it allocates an id, provisions an isolated worktree +
// branch off the given base (or the configured default), and persists the task
// as Open. The returned worktree path is where the human points their AI tool.
func (a *App) TaskNew(intent, agent, base string) (*model.Task, error) {
	if intent == "" {
		return nil, fmt.Errorf("task intent must not be empty")
	}
	if base == "" {
		b, err := a.Repo.CurrentBranch()
		if err != nil {
			b, _ = a.Repo.DefaultBranch()
		}
		base = b
	}

	// Ensure at least one commit exists (git worktree requires it)
	if err := a.Repo.EnsureInitialCommit(); err != nil {
		return nil, fmt.Errorf("preparing repo: %w", err)
	}

	id := idgen.New("t")
	branch := "boltcode/" + id
	worktree, err := a.Repo.AddWorktree(branch, base)
	if err != nil {
		return nil, fmt.Errorf("provisioning worktree: %w", err)
	}

	// Copy spec into the worktree so AI agents see the project constraints
	// immediately when they start working. Best-effort: missing spec is fine.
	if a.SpecExists() {
		specContent, _ := a.SpecContent()
		specDst := filepath.Join(worktree, ".boltcode-spec.md")
		_ = os.WriteFile(specDst, []byte(specContent), 0o644)
	}

	now := time.Now()
	// Get current user from config for created_by
	user := ""
	if cfg, err := a.LoadConfig(); err == nil {
		user = cfg.User
	}
	t := &model.Task{
		ID: id, Intent: intent, Branch: branch, Worktree: worktree,
		Base: base, Agent: agent, Status: model.TaskOpen,
		CreatedBy: user,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := a.Store.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}

// TaskNewWithDeps creates a task with dependency on other tasks.
func (a *App) TaskNewWithDeps(intent, agent, base string, deps []string) (*model.Task, error) {
	t, err := a.TaskNew(intent, agent, base)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 {
		// Validate that all dependency task IDs exist
		for _, dep := range deps {
			if _, err := a.Store.GetTask(dep); err != nil {
				return nil, fmt.Errorf("dependency %s not found", dep)
			}
		}
		t.DependsOn = deps
		t.UpdatedAt = time.Now()
		if err := a.Store.SaveTask(t); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// CheckDependencies verifies all tasks in DependsOn are merged or abandoned.
// Returns a list of blocking task IDs, or nil if all dependencies are satisfied.
func (a *App) CheckDependencies(t *model.Task) []string {
	if len(t.DependsOn) == 0 {
		return nil
	}
	var blocking []string
	for _, depID := range t.DependsOn {
		dep, err := a.Store.GetTask(depID)
		if err != nil {
			blocking = append(blocking, depID+" (not found)")
			continue
		}
		if dep.Status != model.TaskMerged && dep.Status != model.TaskAbandoned {
			blocking = append(blocking, fmt.Sprintf("%s [%s]", depID, dep.Status))
		}
	}
	return blocking
}

// ListTasks returns all tasks, newest first.
func (a *App) ListTasks() ([]*model.Task, error) {
	return a.Store.ListTasks()
}

// TaskClaim assigns a task to a team member. If the task is already claimed by
// someone else, it returns an error — this prevents duplicate work across the
// team. Claiming your own task again is a no-op.
func (a *App) TaskClaim(taskID, assignee string) (*model.Task, error) {
	t, err := a.Store.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", taskID, err)
	}
	if t.Assignee != "" && t.Assignee != assignee {
		return nil, fmt.Errorf("task %s already claimed by %q", taskID, t.Assignee)
	}
	t.Assignee = assignee
	t.UpdatedAt = time.Now()
	if err := a.Store.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}

// WhoAmI returns the git user.name for the current repo, used as the default
// identity for claiming tasks and authoring reviews.
func (a *App) WhoAmI() string {
	name, err := a.Repo.Run("config", "user.name")
	if err != nil || name == "" {
		return "anonymous"
	}
	return name
}
