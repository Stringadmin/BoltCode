package model

import "time"

// ReviewState is the human's verdict on a proposal.
type ReviewState string

const (
	ReviewPending  ReviewState = "pending"  // submitted, not yet looked at
	ReviewApproved ReviewState = "approved" // human approved; ready to merge
	ReviewRejected ReviewState = "rejected" // sent back to the AI with notes
)

// Proposal is a snapshot of AI-produced work submitted for human review.
// It binds a diff to the intent and reasoning that produced it — the context
// that plain git commits lose. AI never merges; a human reviews then merges.
type Proposal struct {
	ID         string      `json:"id"`          // "p_a1b2c3"
	TaskID     string      `json:"task_id"`     // the task this belongs to
	Commit     string      `json:"commit"`      // tip commit of the proposal branch
	BaseCommit string      `json:"base_commit"` // base the diff is computed against
	Summary    string      `json:"summary"`     // AI's one-line summary of the change
	Rationale  string      `json:"rationale"`   // AI's reasoning / prompt context
	Agent      string      `json:"agent"`       // which AI tool produced this
	State      ReviewState `json:"state"`
	Comments   []Comment   `json:"comments"` // human review notes
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// Comment is a human review note, optionally anchored to a file and line.
type Comment struct {
	Author string    `json:"author"`
	File   string    `json:"file,omitempty"` // empty = general comment
	Line   int       `json:"line,omitempty"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}
