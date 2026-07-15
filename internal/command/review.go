package command

import (
	"errors"
	"fmt"
	"time"

	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/store"
)

// ListPending returns proposals still awaiting human review, newest first.
func (a *App) ListPending() ([]*model.Proposal, error) {
	all, err := a.Store.ListProposals()
	if err != nil {
		return nil, err
	}
	var out []*model.Proposal
	for _, p := range all {
		if p.State == model.ReviewPending {
			out = append(out, p)
		}
	}
	return out, nil
}

// ProposalDiff returns the unified diff a proposal introduces.
func (a *App) ProposalDiff(p *model.Proposal) (string, error) {
	return a.Repo.Diff(p.BaseCommit, p.Commit)
}

// AddComment appends a human review comment to a proposal. The comment may be
// anchored to a specific file and line, or free-text (when file is empty).
func (a *App) AddComment(proposalID, author, body, file string, line int) error {
	p, err := a.Store.GetProposal(proposalID)
	if err != nil {
		return fmt.Errorf("proposal %s: %w", proposalID, err)
	}
	p.Comments = append(p.Comments, model.Comment{
		Author: author, Body: body, File: file, Line: line, At: time.Now(),
	})
	p.UpdatedAt = time.Now()
	return a.Store.SaveProposal(p)
}

// Reject sends a proposal back to the AI with a review note, reopening the task
// so the agent can iterate. Nothing is merged or discarded — the branch stays.
//
// The whole transition (proposal → rejected, review record, task → reopened) is
// wrapped in the store-wide lock so a concurrent Approve can't flip the same
// proposal to approved mid-reject.
func (a *App) Reject(proposalID, author, note string) error {
	if err := a.Store.Lock(); err != nil {
		return err
	}
	defer a.Store.Unlock()
	p, err := a.Store.GetProposal(proposalID)
	if err != nil {
		return err
	}
	switch p.State {
	case model.ReviewRejected:
		return nil // idempotent — already rejected
	case model.ReviewApproved:
		return fmt.Errorf("proposal %s is already approved — cannot reject", proposalID)
	}
	// Can't reject work that's already merged.
	if t, err := a.Store.GetTask(p.TaskID); err == nil && t.Status == model.TaskMerged {
		return fmt.Errorf("task %s is already merged — cannot reject", t.ID)
	}
	now := time.Now()
	p.State = model.ReviewRejected
	p.UpdatedAt = now
	if note != "" {
		p.Comments = append(p.Comments, model.Comment{
			Author: author, Body: note, At: now,
		})
	}
	if err := a.Store.SaveProposal(p); err != nil {
		return err
	}

	// Write standalone review record for team visibility
	review := &model.Review{
		ID: idgen.New("r"), ProposalID: p.ID, TaskID: p.TaskID,
		Action: model.ReviewRejected, Author: author, Note: note,
		CreatedAt: now,
	}
	if err := a.Store.SaveReview(review); err != nil {
		return fmt.Errorf("rejected, but failed to write review record: %w", err)
	}

	// Reopen the task so the agent can iterate. If the task genuinely doesn't
	// exist (deleted), there's nothing to reopen and the rejection still
	// succeeded. Any OTHER store error (corrupt file, permission failure) must
	// surface — previously it was swallowed and Reject returned nil, leaving
	// the proposal rejected but the task stuck in "review" with no signal.
	t, err := a.Store.GetTask(p.TaskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // task gone; nothing to reopen
		}
		return fmt.Errorf("rejected, but failed to reopen task %s: %w", p.TaskID, err)
	}
	t.Status = model.TaskOpen
	t.UpdatedAt = now
	if err := a.Store.SaveTask(t); err != nil {
		return fmt.Errorf("rejected, but failed to save reopened task %s: %w", t.ID, err)
	}
	return nil
}
