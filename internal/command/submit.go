package command

import (
	"fmt"
	"time"

	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// Submit snapshots the current state of a task's branch as a proposal awaiting
// human review. This is the hand-off point: the AI (or a hook on its behalf)
// calls this when work is ready. It does NOT merge — that's the human's call.
//
// The diff is computed from the merge-base of the task branch and its base, so
// the proposal reflects only what this task changed even if base moved on.
//
// The proposal creation + task status transition (→ review) are wrapped in the
// store-wide lock so two agents can't double-submit the same task and produce
// two pending proposals for one piece of work.
func (a *App) Submit(taskID, summary, rationale string) (*model.Proposal, error) {
	if err := a.Store.Lock(); err != nil {
		return nil, err
	}
	defer a.Store.Unlock()
	t, err := a.Store.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", taskID, err)
	}

	commit, err := a.Repo.Head(t.Branch)
	if err != nil {
		return nil, fmt.Errorf("reading branch tip: %w", err)
	}
	baseCommit, err := a.Repo.MergeBase(t.Base, t.Branch)
	if err != nil {
		return nil, fmt.Errorf("finding merge base: %w", err)
	}
	if commit == baseCommit {
		return nil, fmt.Errorf("task %s has no new commits to submit", taskID)
	}

	// Backfill files_changed so conflict detection works across the team
	if files, err := a.Repo.ChangedFiles(baseCommit, commit); err == nil && len(files) > 0 {
		t.FilesChanged = files
	}

	now := time.Now()
	p := &model.Proposal{
		ID: idgen.New("p"), TaskID: t.ID, Commit: commit, BaseCommit: baseCommit,
		Summary: summary, Rationale: rationale, Agent: t.Agent,
		State: model.ReviewPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.Store.SaveProposal(p); err != nil {
		return nil, err
	}

	t.Status = model.TaskReview
	t.UpdatedAt = now
	if err := a.Store.SaveTask(t); err != nil {
		return nil, err
	}
	return p, nil
}
