package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Stringadmin/BoltCode/internal/model"
)

func TestNewJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	for _, sub := range []string{"tasks", "proposals", "reviews", "local"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); os.IsNotExist(err) {
			t.Errorf("missing subdirectory %s", sub)
		}
	}
}

func TestSaveGetTask(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()

	now := time.Now()
	task := &model.Task{
		ID: "t_a1b2c3d4", Intent: "test task", Status: model.TaskOpen,
		Branch: "boltcode/t_a1b2c3d4", Base: "main",
		CreatedBy: "test", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	got, err := s.GetTask("t_a1b2c3d4")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Intent != "test task" {
		t.Errorf("intent = %q, want %q", got.Intent, "test task")
	}
	if got.Status != model.TaskOpen {
		t.Errorf("status = %q, want %q", got.Status, model.TaskOpen)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	_, err = s.GetTask("t_ffffffff")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetTaskInvalidID(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	_, err = s.GetTask("../../etc/passwd")
	if err == nil || err == ErrNotFound {
		t.Errorf("expected invalid-id error, got %v", err)
	}
}

func TestSaveProposalInvalidID(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	p := &model.Proposal{ID: "../bad", TaskID: "t_a1b2c3d4"}
	err = s.SaveProposal(p)
	if err == nil || err == ErrNotFound {
		t.Errorf("expected invalid-id error, got %v", err)
	}
}

func TestListTasks(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	now := time.Now()
	for _, id := range []string{"t_00000001", "t_00000002"} {
		s.SaveTask(&model.Task{
			ID: id, Intent: "task " + id, Status: model.TaskOpen,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestLockUnlock(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	if err := s.Lock(); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	s.Unlock()
}

func TestProposalsForTask(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	now := time.Now()
	for _, id := range []string{"p_10000001", "p_10000002"} {
		s.SaveProposal(&model.Proposal{
			ID: id, TaskID: "t_a1b2c3d4", State: model.ReviewPending,
			CreatedAt: now,
		})
	}
	s.SaveProposal(&model.Proposal{
		ID: "p_10000003", TaskID: "t_other", State: model.ReviewPending,
		CreatedAt: now,
	})
	props, err := s.ProposalsForTask("t_a1b2c3d4")
	if err != nil {
		t.Fatalf("ProposalsForTask: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(props))
	}
}

func TestSaveReview(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSON(dir)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}
	defer s.Close()
	now := time.Now()
	r := &model.Review{
		ID: "r_a1b2c3d4", ProposalID: "p_10000001", Author: "tester",
		Note: "looks good", Action: model.ReviewApproved, CreatedAt: now,
	}
	if err := s.SaveReview(r); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	reviews, err := s.ListReviews()
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(reviews))
	}
	if reviews[0].Note != "looks good" {
		t.Errorf("note = %q, want %q", reviews[0].Note, "looks good")
	}
}
