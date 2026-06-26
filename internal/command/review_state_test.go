package command

import (
	"path/filepath"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/model"
	"github.com/Stringadmin/BoltCode/internal/store"
)

// newMemStore builds a JSON store in a temp dir so Approve/Reject can be
// exercised without a real git repo (neither touches Repo).
func newMemStore(t *testing.T) *App {
	t.Helper()
	st, err := store.NewJSON(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return &App{Store: st}
}

func seedProposal(t *testing.T, a *App, state model.ReviewState, taskStatus model.TaskStatus) string {
	t.Helper()
	task := &model.Task{ID: "t_test", Intent: "x", Status: taskStatus}
	if err := a.Store.SaveTask(task); err != nil {
		t.Fatalf("save task: %v", err)
	}
	p := &model.Proposal{
		ID: "p_test", TaskID: "t_test", State: state,
		BaseCommit: "b", Commit: "c", Summary: "s",
	}
	if err := a.Store.SaveProposal(p); err != nil {
		t.Fatalf("save proposal: %v", err)
	}
	return p.ID
}

func TestApproveRejectedProposalFails(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewRejected, model.TaskReview)
	if _, err := a.Approve(pid, "alice"); err == nil {
		t.Fatal("approving a rejected proposal must fail")
	}
}

func TestApproveMergedTaskFails(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewPending, model.TaskMerged)
	if _, err := a.Approve(pid, "alice"); err == nil {
		t.Fatal("approving a proposal whose task is already merged must fail")
	}
}

func TestApprovePendingWritesReview(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewPending, model.TaskReview)
	if _, err := a.Approve(pid, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	reviews, err := a.Store.ListReviews()
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(reviews) != 1 || reviews[0].Action != model.ReviewApproved {
		t.Fatalf("expected 1 approved review, got %+v", reviews)
	}
}

func TestApproveIsIdempotent(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewPending, model.TaskReview)
	if _, err := a.Approve(pid, "alice"); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, err := a.Approve(pid, "alice"); err != nil {
		t.Fatalf("second approve should be idempotent, got: %v", err)
	}
	reviews, _ := a.Store.ListReviews()
	if len(reviews) != 1 {
		t.Fatalf("idempotent approve wrote %d reviews, want 1", len(reviews))
	}
}

func TestRejectApprovedProposalFails(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewApproved, model.TaskReview)
	if err := a.Reject(pid, "alice", "nope"); err == nil {
		t.Fatal("rejecting an approved proposal must fail")
	}
}

func TestRejectPendingWritesReview(t *testing.T) {
	a := newMemStore(t)
	pid := seedProposal(t, a, model.ReviewPending, model.TaskReview)
	if err := a.Reject(pid, "alice", "redo it"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	reviews, _ := a.Store.ListReviews()
	if len(reviews) != 1 || reviews[0].Action != model.ReviewRejected {
		t.Fatalf("expected 1 rejected review, got %+v", reviews)
	}
	// Task should be reopened.
	task, _ := a.Store.GetTask("t_test")
	if task.Status != model.TaskOpen {
		t.Fatalf("task status = %s, want open", task.Status)
	}
}

// --- conflict path normalization ---

func TestFileOverlapNormalizesSeparatorsAndCase(t *testing.T) {
	// Same file, different separator and case, must still count as overlap.
	a := []string{"src/Auth.go"}
	b := []string{filepath.Join("SRC", "auth.go")}
	got := fileOverlap(a, b)
	if len(got) != 1 {
		t.Fatalf("expected overlap across separator/case differences, got %v", got)
	}
}

func TestFileOverlapDedups(t *testing.T) {
	// Duplicate entries shouldn't inflate the overlap count.
	a := []string{"a.go", "a.go"}
	b := []string{"a.go"}
	got := fileOverlap(a, b)
	if len(got) != 1 {
		t.Fatalf("expected dedup to 1, got %v", got)
	}
}
