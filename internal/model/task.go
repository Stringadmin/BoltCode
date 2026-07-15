// Package model defines the core vocabulary of BoltCode: tasks that humans
// create, and proposals that AI agents submit for human review.
package model

import "time"

// TaskStatus tracks where a unit of work is in its lifecycle.
type TaskStatus string

const (
	TaskOpen       TaskStatus = "open"        // created, not yet claimed
	TaskInProgress TaskStatus = "in_progress" // claimed, AI agent working
	TaskReview     TaskStatus = "review"      // submitted for human review
	TaskMerged     TaskStatus = "merged"      // approved and merged
	TaskAbandoned  TaskStatus = "abandoned"   // dropped
)

// Task is a unit of work in a multi-person multi-agent collaboration.
// Each task owns an isolated git worktree + branch so multiple AI agents
// can work in parallel without stepping on each other.
type Task struct {
	ID     string     `json:"id"`
	Intent string     `json:"intent"`
	Status TaskStatus `json:"status"`
	Branch string     `json:"branch"`
	Base   string     `json:"base"`

	// Collaboration fields
	CreatedBy string   `json:"created_by"`
	Assignee  string   `json:"assignee,omitempty"`
	Agent     string   `json:"agent"`
	Priority  int      `json:"priority,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Tags      []string `json:"tags,omitempty"`

	// Agent output (filled after execution)
	Summary      string   `json:"summary,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`

	// Local-only (not synced, derived at runtime). json:"-" keeps it out of
	// the persisted/synced metadata so a machine-specific worktree path can't
	// leak into the shared .boltcode/tasks/ that teammates pull down.
	Worktree string `json:"-"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
