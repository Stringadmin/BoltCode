package model

import "time"

// Review is a standalone record of a human review action (approve/reject).
// Written to .boltcode/reviews/ so the team can browse review history
// independently of proposal files.
type Review struct {
	ID         string      `json:"id"`
	ProposalID string      `json:"proposal_id"`
	TaskID     string      `json:"task_id"`
	Action     ReviewState `json:"action"` // "approved" or "rejected"
	Author     string      `json:"author"`
	Note       string      `json:"note,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}
