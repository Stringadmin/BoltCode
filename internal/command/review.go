package command

import (
	"fmt"
	"time"

	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
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

// Reject sends a proposal back to the AI with a review note, reopening the task
// so the agent can iterate. Nothing is merged or discarded — the branch stays.
func (a *App) Reject(proposalID, author, note string) error {
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

	if t, err := a.Store.GetTask(p.TaskID); err == nil {
		t.Status = model.TaskOpen
		t.UpdatedAt = now
		return a.Store.SaveTask(t)
	}
	return nil
}
