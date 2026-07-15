package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskMarshalUnmarshal(t *testing.T) {
	now := time.Now()
	orig := &Task{
		ID: "t_a1b2c3d4", Intent: "test", Status: TaskOpen,
		Branch: "boltcode/t_a1b2c3d4", Base: "main",
		CreatedBy: "alice", Assignee: "bob",
		Agent: "builtin", Priority: 1,
		DependsOn: []string{"t_00000001"}, Tags: []string{"bug"},
		Summary: "fix bug", FilesChanged: []string{"main.go"},
		Worktree:  "/tmp/worktree",
		CreatedAt: now, UpdatedAt: now,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != orig.ID {
		t.Errorf("ID = %q, want %q", got.ID, orig.ID)
	}
	if got.Status != TaskOpen {
		t.Errorf("Status = %q, want %q", got.Status, TaskOpen)
	}
	// Worktree has json:"-" so it must NOT survive round-trip.
	if got.Worktree != "" {
		t.Errorf("Worktree should be empty after round-trip, got %q", got.Worktree)
	}
}

func TestTaskStatusValues(t *testing.T) {
	if TaskOpen != "open" {
		t.Errorf("TaskOpen = %q", TaskOpen)
	}
	if TaskInProgress != "in_progress" {
		t.Errorf("TaskInProgress = %q", TaskInProgress)
	}
	if TaskReview != "review" {
		t.Errorf("TaskReview = %q", TaskReview)
	}
	if TaskMerged != "merged" {
		t.Errorf("TaskMerged = %q", TaskMerged)
	}
	if TaskAbandoned != "abandoned" {
		t.Errorf("TaskAbandoned = %q", TaskAbandoned)
	}
}

func TestProposalRoundTrip(t *testing.T) {
	now := time.Now()
	p := &Proposal{
		ID: "p_a1b2c3d4", TaskID: "t_00000001",
		Commit: "abc123", BaseCommit: "def456",
		Summary: "fix the thing", Rationale: "because",
		Agent: "builtin", State: ReviewPending,
		Comments: []Comment{
			{Author: "alice", File: "main.go", Line: 42, Body: "nice", At: now},
		},
		CreatedAt: now, UpdatedAt: now,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Proposal
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Summary != "fix the thing" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.State != ReviewPending {
		t.Errorf("State = %q", got.State)
	}
	if len(got.Comments) != 1 || got.Comments[0].Author != "alice" {
		t.Errorf("comments not preserved")
	}
}

func TestReviewRoundTrip(t *testing.T) {
	now := time.Now()
	r := &Review{
		ID: "r_a1b2c3d4", ProposalID: "p_00000001", TaskID: "t_00000001",
		Action: ReviewApproved, Author: "alice", Note: "LGTM",
		CreatedAt: now,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Review
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != ReviewApproved {
		t.Errorf("Action = %q", got.Action)
	}
	if got.Note != "LGTM" {
		t.Errorf("Note = %q", got.Note)
	}
}

func TestReviewStateValues(t *testing.T) {
	if ReviewPending != "pending" {
		t.Errorf("ReviewPending = %q", ReviewPending)
	}
	if ReviewApproved != "approved" {
		t.Errorf("ReviewApproved = %q", ReviewApproved)
	}
	if ReviewRejected != "rejected" {
		t.Errorf("ReviewRejected = %q", ReviewRejected)
	}
}
