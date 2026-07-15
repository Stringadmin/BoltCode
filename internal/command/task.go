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

// TaskNewWithDeps creates a task with dependency on other tasks. Dependencies
// are validated (existence + no cycle) BEFORE the worktree is provisioned, so
// an invalid request doesn't leave an orphaned worktree behind.
func (a *App) TaskNewWithDeps(intent, agent, base string, deps []string) (*model.Task, error) {
	if len(deps) > 0 {
		// Validate that all dependency task IDs exist before doing any work.
		for _, dep := range deps {
			if _, err := a.Store.GetTask(dep); err != nil {
				return nil, fmt.Errorf("dependency %s not found", dep)
			}
		}
		// Reject cycles in the dependency graph. Without this, tasks A→B and
		// B→A would both block forever (each waiting for the other to merge),
		// and CheckDependencies can't surface it. A new task can't itself be
		// part of a cycle (nothing depends on it yet), but the deps it lists
		// might already form a cycle (e.g. from a future "edit deps" feature or
		// manual edits) — detect that here.
		if cyc, err := a.depsHaveCycle(deps); err != nil {
			return nil, err
		} else if cyc {
			return nil, fmt.Errorf("dependency cycle detected among: %v", deps)
		}
	}
	t, err := a.TaskNew(intent, agent, base)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 {
		t.DependsOn = deps
		t.UpdatedAt = time.Now()
		if err := a.Store.SaveTask(t); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// depsHaveCycle walks the transitive DependsOn graph of the given dep ids and
// reports whether any cycle exists (a back edge in the DFS). Missing tasks are
// treated as leaves (the caller validates existence separately).
func (a *App) depsHaveCycle(deps []string) (bool, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int)
	var visit func(id string) (bool, error)
	visit = func(id string) (bool, error) {
		switch color[id] {
		case gray:
			return true, nil // back edge → cycle
		case black:
			return false, nil
		}
		color[id] = gray
		t, err := a.Store.GetTask(id)
		if err != nil {
			// Missing task: caller reports existence; treat as a leaf here.
			color[id] = black
			return false, nil
		}
		for _, d := range t.DependsOn {
			cyc, err := visit(d)
			if err != nil {
				return false, err
			}
			if cyc {
				return true, nil
			}
		}
		color[id] = black
		return false, nil
	}
	for _, dep := range deps {
		cyc, err := visit(dep)
		if err != nil {
			return false, err
		}
		if cyc {
			return true, nil
		}
	}
	return false, nil
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
//
// The whole read-check-write is wrapped in the store-wide lock so two parallel
// agents (separate processes) can't both observe an unclaimed task and both
// write themselves as assignee — a classic TOCTOU lost-update.
func (a *App) TaskClaim(taskID, assignee string) (*model.Task, error) {
	if err := a.Store.Lock(); err != nil {
		return nil, err
	}
	defer a.Store.Unlock()
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

// TaskAbandon marks a task as abandoned and removes its worktree. Proposals
// and reviews for the task are preserved for audit, and the git branch (with
// history) is kept so work isn't lost — only the worktree checkout is cleaned up.
func (a *App) TaskAbandon(taskID string) (*model.Task, error) {
	if err := a.Store.Lock(); err != nil {
		return nil, err
	}
	defer a.Store.Unlock()
	t, err := a.Store.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", taskID, err)
	}
	if t.Status == model.TaskMerged {
		return nil, fmt.Errorf("task %s is already merged — cannot abandon", taskID)
	}
	if t.Status == model.TaskAbandoned {
		return nil, fmt.Errorf("task %s is already abandoned", taskID)
	}
	t.Status = model.TaskAbandoned
	t.UpdatedAt = time.Now()
	// Remove the worktree so we don't accumulate stale checkouts.
	if t.Worktree != "" {
		_ = a.Repo.RemoveWorktree(t.Worktree, true)
	}
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

// ResolveWorktree sets t.Worktree from the task's branch if it isn't already
// set. The worktree path is deterministic (WorktreeBase + "/" + branch) and
// machine-local, so it is NOT persisted in the synced task metadata (json:"-")
// — it must be re-derived whenever a task is loaded from disk and its worktree
// is needed. Callers that load a task and then use its worktree (e.g. `run
// <task-id>`) call this after GetTask.
func (a *App) ResolveWorktree(t *model.Task) {
	if t.Worktree == "" && t.Branch != "" {
		t.Worktree = filepath.Join(a.Repo.WorktreeBase(), t.Branch)
	}
}
