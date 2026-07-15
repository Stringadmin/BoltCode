package command

import (
	"fmt"
	"path/filepath"
	"sync"
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
	// Close the store (releasing the cross-process lock file handle) before
	// t.TempDir's cleanup removes the dir — Windows refuses to delete an open
	// file. Cleanups run LIFO, so this runs before the TempDir removal.
	t.Cleanup(func() { st.Close() })
	return &App{Store: st}
}

func seedProposal(t *testing.T, a *App, state model.ReviewState, taskStatus model.TaskStatus) string {
	t.Helper()
	task := &model.Task{ID: "t_aabbccdd", Intent: "x", Status: taskStatus}
	if err := a.Store.SaveTask(task); err != nil {
		t.Fatalf("save task: %v", err)
	}
	p := &model.Proposal{
		ID: "p_aabbccdd", TaskID: "t_aabbccdd", State: state,
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
	task, _ := a.Store.GetTask("t_aabbccdd")
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

// --- path-traversal protection (P0) ---
// Store methods must reject malformed ids before building a filesystem path,
// so a crafted id like "../../etc/passwd" can't read or write outside .boltcode.

func TestStoreRejectsTraversalTaskID(t *testing.T) {
	a := newMemStore(t)
	if _, err := a.Store.GetTask("../../etc/passwd"); err == nil {
		t.Fatal("GetTask must reject traversal id")
	}
	if _, err := a.Store.GetTask("t_not_hex"); err == nil {
		t.Fatal("GetTask must reject non-hex id")
	}
	if err := a.Store.SaveTask(&model.Task{ID: "../escape"}); err == nil {
		t.Fatal("SaveTask must reject traversal id")
	}
}

func TestStoreRejectsTraversalProposalID(t *testing.T) {
	a := newMemStore(t)
	if _, err := a.Store.GetProposal("../../etc/passwd"); err == nil {
		t.Fatal("GetProposal must reject traversal id")
	}
	if err := a.Store.SaveProposal(&model.Proposal{ID: "p_..", TaskID: "t_aabbccdd"}); err == nil {
		t.Fatal("SaveProposal must reject malformed id")
	}
}

func TestStoreAcceptsWellFormedID(t *testing.T) {
	// A canonical idgen id must round-trip through the store.
	a := newMemStore(t)
	task := &model.Task{ID: "t_deadbeef", Intent: "x", Status: model.TaskOpen}
	if err := a.Store.SaveTask(task); err != nil {
		t.Fatalf("SaveTask well-formed id: %v", err)
	}
	got, err := a.Store.GetTask("t_deadbeef")
	if err != nil {
		t.Fatalf("GetTask well-formed id: %v", err)
	}
	if got.ID != "t_deadbeef" {
		t.Errorf("round-trip id = %q", got.ID)
	}
}

// --- cycle detection (P1) ---

func TestDepsHaveCycleDetectsCycle(t *testing.T) {
	a := newMemStore(t)
	save := func(id string, deps []string) {
		t.Helper()
		if err := a.Store.SaveTask(&model.Task{ID: id, DependsOn: deps, Status: model.TaskOpen}); err != nil {
			t.Fatal(err)
		}
	}
	// A -> B -> C -> A (back edge)
	save("t_aaaaaaaa", []string{"t_bbbbbbbb"})
	save("t_bbbbbbbb", []string{"t_cccccccc"})
	save("t_cccccccc", []string{"t_aaaaaaaa"})

	cyc, err := a.depsHaveCycle([]string{"t_aaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if !cyc {
		t.Fatal("expected cycle detection for A->B->C->A")
	}
}

func TestDepsHaveCycleNoFalsePositive(t *testing.T) {
	a := newMemStore(t)
	save := func(id string, deps []string) {
		t.Helper()
		if err := a.Store.SaveTask(&model.Task{ID: id, DependsOn: deps, Status: model.TaskOpen}); err != nil {
			t.Fatal(err)
		}
	}
	// A -> B -> C (a DAG, no cycle)
	save("t_aaaaaaaa", []string{"t_bbbbbbbb"})
	save("t_bbbbbbbb", []string{"t_cccccccc"})
	save("t_cccccccc", nil)

	cyc, err := a.depsHaveCycle([]string{"t_aaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if cyc {
		t.Fatal("false positive: A->B->C is not a cycle")
	}
}

// --- store lock serialization (P1) ---
// Two concurrent claims on the same task must produce exactly one winner; the
// store-wide lock prevents the TOCTOU where both read an unclaimed task and
// both write themselves as assignee.

func TestTaskClaimSerializesUnderLock(t *testing.T) {
	a := newMemStore(t)
	if err := a.Store.SaveTask(&model.Task{ID: "t_aaaaaaaa", Intent: "x", Status: model.TaskOpen}); err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, failures := 0, 0
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			assignee := fmt.Sprintf("agent-%d", i)
			_, err := a.TaskClaim("t_aaaaaaaa", assignee)
			mu.Lock()
			if err == nil {
				successes++
			} else {
				failures++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("expected exactly 1 successful claim, got %d (failures=%d) — lock failed to serialize", successes, failures)
	}
	if failures != goroutines-1 {
		t.Fatalf("expected %d failures, got %d", goroutines-1, failures)
	}
}
