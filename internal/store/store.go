// Package store persists BoltCode metadata (tasks, proposals) for a repo.
//
// The default backend writes JSON files under .boltcode/ in the repo root.
// It is deliberately hidden behind the Store interface so a future backend
// (e.g. custom git refs synced via push/pull) can replace it without
// touching command or TUI code.
package store

import "github.com/Stringadmin/BoltCode/internal/model"

// Store is the persistence boundary for BoltCode metadata.
//
// Lock/Unlock provide a coarse, store-wide exclusive lock that callers should
// hold around read-modify-write sequences (e.g. TaskClaim: GetTask → check
// Assignee → SaveTask). Without it, two parallel agents in separate processes
// can both read an unclaimed task and both write themselves as assignee,
// producing a lost update. The lock is cross-process (a file lock under the
// store root) so it works for the documented multi-worktree, multi-agent
// workflow.
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

	// Lock blocks until the store-wide exclusive lock is acquired. Pair every
	// Lock with a deferred Unlock.
	Lock() error
	// Unlock releases the store-wide lock.
	Unlock() error

	// Close releases any underlying resources (e.g. the cross-process lock
	// file handle). It should be called when the store is no longer needed;
	// in long-lived processes (the interactive shell) it is fine to never
	// call it, but tests must Close so temp dirs can be cleaned up.
	Close() error
}
