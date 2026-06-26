// Package store persists BoltCode metadata (tasks, proposals) for a repo.
//
// The default backend writes JSON files under .boltcode/ in the repo root.
// It is deliberately hidden behind the Store interface so a future backend
// (e.g. custom git refs synced via push/pull) can replace it without
// touching command or TUI code.
package store

import "github.com/Stringadmin/BoltCode/internal/model"

// Store is the persistence boundary for BoltCode metadata.
type Store interface {
	SaveTask(t *model.Task) error
	GetTask(id string) (*model.Task, error)
	ListTasks() ([]*model.Task, error)

	SaveProposal(p *model.Proposal) error
	GetProposal(id string) (*model.Proposal, error)
	ListProposals() ([]*model.Proposal, error)
	// ProposalsForTask returns proposals belonging to a task, newest first.
	ProposalsForTask(taskID string) ([]*model.Proposal, error)

	SaveReview(r *model.Review) error
	ListReviews() ([]*model.Review, error)
}
