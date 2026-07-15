package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/model"
)

// ErrNotFound is returned when a task or proposal id does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidID is returned when an id does not match the canonical idgen
// format (prefix + "_" + 8 hex). Rejecting malformed ids here prevents
// path-traversal attacks (e.g. "../../etc/passwd") from reaching the
// filesystem path builders below.
var ErrInvalidID = errors.New("invalid id")

// validateID returns ErrInvalidID if id is not a well-formed idgen id. List
// operations iterate filenames from disk and are trusted, so they bypass this;
// every method that takes an id from a caller (Get/Save/etc.) validates first.
func validateID(id string) error {
	if !idgen.Validate(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return nil
}

// jsonStore persists metadata as individual JSON files under a root dir,
// conventionally <repo>/.boltcode. One file per entity keeps merges sane and
// makes the on-disk state easy to inspect by hand.
type jsonStore struct {
	root  string        // e.g. <repo>/.boltcode
	mu    sync.Mutex    // serializes operations within one process
	flock *fio.FileLock // serializes operations across processes (parallel agents)
}

// NewJSON returns a Store rooted at dir, creating the layout if needed.
func NewJSON(dir string) (Store, error) {
	for _, sub := range []string{"tasks", "proposals", "reviews", "local"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	// The cross-process lock lives under local/ (which is gitignored and
	// per-machine), so it never gets synced and never conflicts with teammates.
	flock, err := fio.NewFileLock(filepath.Join(dir, "local", ".store.lock"))
	if err != nil {
		return nil, fmt.Errorf("opening store lock: %w", err)
	}
	return &jsonStore{root: dir, flock: flock}, nil
}

func (s *jsonStore) taskPath(id string) string {
	return filepath.Join(s.root, "tasks", id+".json")
}

func (s *jsonStore) proposalPath(id string) string {
	return filepath.Join(s.root, "proposals", id+".json")
}

// writeJSON marshals v and writes it atomically (unique temp file, then rename)
// so a crash mid-write can't leave a half-written metadata file. Using
// fio.WriteFileAtomic (rather than a fixed ".tmp" name) also prevents two
// concurrent writers to the same entity from colliding on the temp file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fio.WriteFileAtomic(path, data, 0o644)
}

// readJSON loads path into v, mapping a missing file to ErrNotFound.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (s *jsonStore) SaveTask(t *model.Task) error {
	if err := validateID(t.ID); err != nil {
		return err
	}
	return writeJSON(s.taskPath(t.ID), t)
}

func (s *jsonStore) GetTask(id string) (*model.Task, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	var t model.Task
	if err := readJSON(s.taskPath(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *jsonStore) SaveProposal(p *model.Proposal) error {
	if err := validateID(p.ID); err != nil {
		return err
	}
	return writeJSON(s.proposalPath(p.ID), p)
}

func (s *jsonStore) GetProposal(id string) (*model.Proposal, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	var p model.Proposal
	if err := readJSON(s.proposalPath(id), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// listIDs returns the entity ids (filenames without .json) in a subdir.
func (s *jsonStore) listIDs(sub string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, sub))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		ids = append(ids, name[:len(name)-len(".json")])
	}
	return ids, nil
}

func (s *jsonStore) ListTasks() ([]*model.Task, error) {
	ids, err := s.listIDs("tasks")
	if err != nil {
		return nil, err
	}
	tasks := make([]*model.Task, 0, len(ids))
	for _, id := range ids {
		t, err := s.GetTask(id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (s *jsonStore) ListProposals() ([]*model.Proposal, error) {
	ids, err := s.listIDs("proposals")
	if err != nil {
		return nil, err
	}
	props := make([]*model.Proposal, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetProposal(id)
		if err != nil {
			return nil, err
		}
		props = append(props, p)
	}
	sort.Slice(props, func(i, j int) bool {
		return props[i].CreatedAt.After(props[j].CreatedAt)
	})
	return props, nil
}

func (s *jsonStore) ProposalsForTask(taskID string) ([]*model.Proposal, error) {
	all, err := s.ListProposals()
	if err != nil {
		return nil, err
	}
	var out []*model.Proposal
	for _, p := range all {
		if p.TaskID == taskID {
			out = append(out, p)
		}
	}
	return out, nil // ListProposals already sorts newest-first
}

func (s *jsonStore) reviewPath(id string) string {
	return filepath.Join(s.root, "reviews", id+".json")
}

func (s *jsonStore) SaveReview(r *model.Review) error {
	if err := validateID(r.ID); err != nil {
		return err
	}
	return writeJSON(s.reviewPath(r.ID), r)
}

func (s *jsonStore) ListReviews() ([]*model.Review, error) {
	ids, err := s.listIDs("reviews")
	if err != nil {
		return nil, err
	}
	reviews := make([]*model.Review, 0, len(ids))
	for _, id := range ids {
		var r model.Review
		if err := readJSON(s.reviewPath(id), &r); err != nil {
			continue
		}
		reviews = append(reviews, &r)
	}
	sort.Slice(reviews, func(i, j int) bool {
		return reviews[i].CreatedAt.After(reviews[j].CreatedAt)
	})
	return reviews, nil
}

// Lock acquires the store-wide exclusive lock: first an in-process mutex (so
// goroutines within one process serialize), then a cross-process file lock (so
// parallel BoltCode agents in separate processes serialize). Callers wrap
// read-modify-write sequences (TaskClaim, Submit, Approve, Reject) in
// Lock/defer Unlock to prevent lost updates.
func (s *jsonStore) Lock() error {
	s.mu.Lock()
	if err := s.flock.Lock(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("acquiring store lock: %w", err)
	}
	return nil
}

// Unlock releases the cross-process file lock then the in-process mutex.
// Order is the reverse of Lock. It is safe to call even if Lock failed partway
// (flock.Unlock on an unlocked handle is a no-op on both platforms).
func (s *jsonStore) Unlock() error {
	err := s.flock.Unlock()
	s.mu.Unlock()
	return err
}

// Close releases the cross-process lock file handle. After Close the store
// must not be used again. Tests must call Close so the temp dir containing the
// lock file can be removed (Windows refuses to delete an open file).
func (s *jsonStore) Close() error {
	return s.flock.Close()
}
