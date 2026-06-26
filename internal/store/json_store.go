package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/Stringadmin/BoltCode/internal/model"
)

// ErrNotFound is returned when a task or proposal id does not exist.
var ErrNotFound = errors.New("not found")

// jsonStore persists metadata as individual JSON files under a root dir,
// conventionally <repo>/.boltcode. One file per entity keeps merges sane and
// makes the on-disk state easy to inspect by hand.
type jsonStore struct {
	root string // e.g. <repo>/.boltcode
}

// NewJSON returns a Store rooted at dir, creating the layout if needed.
func NewJSON(dir string) (Store, error) {
	for _, sub := range []string{"tasks", "proposals", "reviews", "local"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	return &jsonStore{root: dir}, nil
}

func (s *jsonStore) taskPath(id string) string {
	return filepath.Join(s.root, "tasks", id+".json")
}

func (s *jsonStore) proposalPath(id string) string {
	return filepath.Join(s.root, "proposals", id+".json")
}

// writeJSON marshals v and writes it atomically (write temp, then rename) so a
// crash mid-write can't leave a half-written metadata file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	return writeJSON(s.taskPath(t.ID), t)
}

func (s *jsonStore) GetTask(id string) (*model.Task, error) {
	var t model.Task
	if err := readJSON(s.taskPath(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *jsonStore) SaveProposal(p *model.Proposal) error {
	return writeJSON(s.proposalPath(p.ID), p)
}

func (s *jsonStore) GetProposal(id string) (*model.Proposal, error) {
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
