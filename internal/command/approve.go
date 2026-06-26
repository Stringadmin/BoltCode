package command

import (
	"fmt"
	"time"

	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// Approve marks a proposal approved. It does NOT merge by itself — merging is a
// separate, explicit step (ApproveAndMerge) so a human can approve in bulk and
// merge deliberately. AI tools never have a path to either.
func (a *App) Approve(proposalID, author string) (*model.Proposal, error) {
	p, err := a.Store.GetProposal(proposalID)
	if err != nil {
		return nil, err
	}
	switch p.State {
	case model.ReviewApproved:
		return p, nil // idempotent — already approved
	case model.ReviewRejected:
		return nil, fmt.Errorf("proposal %s was rejected — resubmit before approving", proposalID)
	}
	// Guard against approving a proposal whose task is already merged: that
	// would write a contradictory review record against finished work.
	if t, err := a.Store.GetTask(p.TaskID); err == nil && t.Status == model.TaskMerged {
		return nil, fmt.Errorf("task %s is already merged — nothing to approve", t.ID)
	}
	now := time.Now()
	p.State = model.ReviewApproved
	p.UpdatedAt = now
	if err := a.Store.SaveProposal(p); err != nil {
		return nil, err
	}

	// Write standalone review record for team visibility
	review := &model.Review{
		ID: idgen.New("r"), ProposalID: p.ID, TaskID: p.TaskID,
		Action: model.ReviewApproved, Author: author,
		CreatedAt: now,
	}
	if err := a.Store.SaveReview(review); err != nil {
		// The proposal state is already updated; a failed review record is
		// non-fatal but shouldn't be silently swallowed — surface it.
		return p, fmt.Errorf("approved, but failed to write review record: %w", err)
	}

	return p, nil
}

// ApproveAndMerge approves a proposal and merges its branch into the task's
// base in one step. The merge runs in the main worktree, which must be on the
// base branch and clean. On conflict the merge is aborted and an error is
// returned for the human to resolve — BoltCode never auto-resolves.
func (a *App) ApproveAndMerge(proposalID, author string) (*model.Task, error) {
	p, err := a.Approve(proposalID, author)
	if err != nil {
		return nil, err
	}
	t, err := a.Store.GetTask(p.TaskID)
	if err != nil {
		return nil, err
	}

	cur, err := a.Repo.CurrentBranch()
	if err != nil {
		return nil, err
	}
	if cur != t.Base {
		return nil, fmt.Errorf(
			"main worktree is on %q but proposal merges into %q; switch first",
			cur, t.Base)
	}

	msg := fmt.Sprintf("boltcode: merge %s (task %s)\n\n%s", p.ID, t.ID, p.Summary)
	if err := a.Repo.Merge(t.Branch, msg); err != nil {
		return nil, err
	}

	now := time.Now()
	t.Status = model.TaskMerged
	t.UpdatedAt = now
	if err := a.Store.SaveTask(t); err != nil {
		return nil, err
	}
	return t, nil
}
