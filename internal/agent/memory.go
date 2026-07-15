package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/fio"
)

// ProjectMemory persists across sessions. Stored at .boltcode/local/memory.json.
type ProjectMemory struct {
	// Project understanding
	Architecture string            `json:"architecture,omitempty"`
	FileMap      map[string]string `json:"file_map,omitempty"`
	BuildCmd     string            `json:"build_cmd,omitempty"`
	TestCmd      string            `json:"test_cmd,omitempty"`
	Language     string            `json:"language,omitempty"`
	Framework    string            `json:"framework,omitempty"`

	// User preferences
	Preferences []string `json:"preferences,omitempty"`

	// Past decisions (what was agreed on)
	Decisions []Decision `json:"decisions,omitempty"`

	// Cumulative usage across sessions
	CumulativeStats *CumulativeUsage `json:"cumulative_stats,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

// CumulativeUsage tracks total token usage across all sessions in this project.
// Displayed in the session-end panel so users see lifetime cost.
type CumulativeUsage struct {
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
	TotalCost         float64 `json:"total_cost"`
	SessionCount      int     `json:"session_count"`
}

type Decision struct {
	Topic  string `json:"topic"`
	Choice string `json:"choice"`
	Date   string `json:"date"`
}

func memoryPath(workdir string) string {
	// Walk up to find .boltcode/local/
	dir := workdir
	for {
		candidate := filepath.Join(dir, ".boltcode", "local", "memory.json")
		if _, err := os.Stat(filepath.Dir(candidate)); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(workdir, ".boltcode", "local", "memory.json")
}

// LoadMemory reads the persistent memory from disk.
func (a *Agent) LoadMemory() *ProjectMemory {
	path := memoryPath(a.Workdir)
	data, err := os.ReadFile(path)
	if err != nil {
		return &ProjectMemory{FileMap: make(map[string]string)}
	}
	var mem ProjectMemory
	if err := json.Unmarshal(data, &mem); err != nil {
		return &ProjectMemory{FileMap: make(map[string]string)}
	}
	if mem.FileMap == nil {
		mem.FileMap = make(map[string]string)
	}
	return &mem
}

// SaveMemory persists memory to disk. It now returns an error (previously the
// write failure was silently swallowed) and writes atomically (temp + rename)
// so a crash or signal mid-write can't leave a corrupt memory.json. Memory is
// per-worktree and the agent updates it from the single-threaded tool loop, so
// no cross-process lock is needed — atomicity is enough.
func (a *Agent) SaveMemory(mem *ProjectMemory) error {
	mem.UpdatedAt = time.Now()
	path := memoryPath(a.Workdir)
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	return fio.WriteFileAtomic(path, data, 0o600)
}

// MemoryContext returns a formatted string of the memory for injection into prompts.
func (mem *ProjectMemory) Context() string {
	var parts []string

	if mem.Language != "" {
		parts = append(parts, "Language: "+mem.Language)
	}
	if mem.Framework != "" {
		parts = append(parts, "Framework: "+mem.Framework)
	}
	if mem.BuildCmd != "" {
		parts = append(parts, "Build command: "+mem.BuildCmd)
	}
	if mem.TestCmd != "" {
		parts = append(parts, "Test command: "+mem.TestCmd)
	}
	if mem.Architecture != "" {
		parts = append(parts, "Architecture:\n"+mem.Architecture)
	}
	if len(mem.FileMap) > 0 {
		parts = append(parts, "File map:")
		for file, desc := range mem.FileMap {
			parts = append(parts, "  "+file+" — "+desc)
		}
	}
	if len(mem.Preferences) > 0 {
		parts = append(parts, "User preferences:")
		for _, p := range mem.Preferences {
			parts = append(parts, "  - "+p)
		}
	}
	if len(mem.Decisions) > 0 {
		parts = append(parts, "Past decisions:")
		for _, d := range mem.Decisions {
			parts = append(parts, "  - "+d.Topic+": "+d.Choice)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// saveCumulativeStats loads the persistent memory, adds the current session's
// usage to the cumulative totals, and writes it back. Called at the end of
// Run() so the user sees lifetime stats across sessions.
func (a *Agent) saveCumulativeStats() {
	if a.Stats == nil {
		return
	}
	mem := a.LoadMemory()
	if mem.CumulativeStats == nil {
		mem.CumulativeStats = &CumulativeUsage{}
	}
	cu := mem.CumulativeStats
	cu.TotalInputTokens += a.Stats.InputTokens
	cu.TotalOutputTokens += a.Stats.OutputTokens
	cu.TotalCost += a.Stats.TotalCost
	cu.SessionCount++
	// Attach cumulative data to Stats so Display() can show it.
	a.Stats.SetCumulative(cu)
	// Best-effort save — don't let a write failure shadow the session-end
	// animation.
	_ = a.SaveMemory(mem)
}
