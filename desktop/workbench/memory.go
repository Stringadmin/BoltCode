package workbench

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/agent"
)

type WorkspaceFact struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Source    string    `json:"source"`
	Version   string    `json:"version"`
	FirstSeen time.Time `json:"firstSeen,omitzero"`
}

type WorkspaceCheck struct {
	ID         string    `json:"id"`
	Tree       string    `json:"tree"`
	FinishedAt time.Time `json:"finishedAt"`
}

type WorkspaceMemory struct {
	Facts       []WorkspaceFact    `json:"facts,omitempty"`
	Checks      []WorkspaceCheck   `json:"checks,omitempty"`
	ToolLessons []agent.ToolLesson `json:"toolLessons,omitempty"`
}

// Use explicit package manager declarations so the inferred commands contain
// only known executable names and script keys.
var packageManagerPattern = regexp.MustCompile(`^(npm|pnpm|yarn|bun)@[0-9][0-9A-Za-z.+-]{0,95}$`)

func readWorkspaceFacts(workdir string) []WorkspaceFact {
	root, err := os.OpenRoot(workdir)
	if err != nil {
		return nil
	}
	defer root.Close()
	data, err := readReference(root, "package.json")
	if err != nil {
		return nil
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	var packageManager string
	if err := json.Unmarshal(manifest["packageManager"], &packageManager); err != nil {
		return nil
	}
	match := packageManagerPattern.FindStringSubmatch(packageManager)
	if match == nil {
		return nil
	}
	paths, err := worktreeFiles(workdir, "package.json")
	if err != nil || !slices.Contains(paths, "package.json") {
		return nil
	}

	facts := []WorkspaceFact{workspaceFact("package_manager", packageManager, "package.json#packageManager", packageManager)}
	var scripts map[string]json.RawMessage
	if err := json.Unmarshal(manifest["scripts"], &scripts); err != nil {
		return facts
	}
	for _, script := range []string{"build", "test"} {
		var command string
		if err := json.Unmarshal(scripts[script], &command); err != nil || strings.TrimSpace(command) == "" {
			continue
		}
		facts = append(facts, workspaceFact(script+"_command", match[1]+" run "+script,
			"package.json#packageManager + scripts."+script, packageManager, command))
	}
	return facts
}

func workspaceFact(key, value, source string, evidence ...string) WorkspaceFact {
	data, _ := json.Marshal(evidence)
	hash := sha256.Sum256(data)
	return WorkspaceFact{Key: key, Value: value, Source: source, Version: fmt.Sprintf("%x", hash)}
}

func (thread *storedChat) workspaceFacts(workdir string, record bool) []WorkspaceFact {
	facts := readWorkspaceFacts(workdir)
	for i, fact := range facts {
		index := slices.IndexFunc(thread.MemoryVersions, func(version WorkspaceFact) bool {
			return version.Key == fact.Key && version.Version == fact.Version && version.Value == fact.Value && version.Source == fact.Source
		})
		if index >= 0 {
			facts[i] = thread.MemoryVersions[index]
		} else if record {
			fact.FirstSeen = time.Now().UTC()
			thread.MemoryVersions = append(thread.MemoryVersions, fact)
			facts[i] = fact
		}
	}
	return facts
}

func (s *Service) workspaceMemory(thread *storedChat, workdir string, record bool, mode agent.Mode) WorkspaceMemory {
	memory := WorkspaceMemory{Facts: thread.workspaceFacts(workdir, record)}
	for _, lesson := range s.projectSettings().ToolLessons {
		if lesson.Guidance(mode) != "" {
			memory.ToolLessons = append(memory.ToolLessons, lesson)
		}
	}
	if len(memory.Facts) == 0 && len(thread.MemoryVersions) == 0 {
		return memory
	}
	configured := s.projectChecks().configured()
	hasPass := slices.ContainsFunc(thread.CheckResults, func(saved savedCheck) bool {
		return saved.Result.Status == "passed" && !saved.Changed && slices.ContainsFunc(configured, func(check CheckState) bool {
			return saved.ID == check.ID && saved.Result.Command == check.Command
		})
	})
	if !hasPass {
		return memory
	}
	current, err := s.capture(thread)
	if err != nil {
		return memory
	}
	for _, check := range s.verification(thread, nil, current).Checks {
		if check.Status == "passed" && check.Result != nil {
			memory.Checks = append(memory.Checks, WorkspaceCheck{ID: check.ID, Tree: current.tree, FinishedAt: check.Result.FinishedAt})
		}
	}
	return memory
}

func (s *Service) ReadWorkspaceMemory(id string) (WorkspaceMemory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, err := s.chat(id)
	if err != nil {
		return WorkspaceMemory{}, err
	}
	if thread.Start == "" || s.active != nil || s.checking != nil {
		return WorkspaceMemory{}, nil
	}
	workdir := s.worktree(id)
	if err := validateWorktree(thread, workdir); err != nil {
		return WorkspaceMemory{}, err
	}
	return s.workspaceMemory(thread, workdir, false, agent.ModeBuild), nil
}

func (memory WorkspaceMemory) prompt() string {
	var context strings.Builder
	for _, fact := range memory.Facts {
		fmt.Fprintf(&context, "\n- %s: %s (%s)", fact.Key, fact.Value, fact.Source)
	}
	for _, check := range memory.Checks {
		fmt.Fprintf(&context, "\n- Configured %s check passed for workspace snapshot %s.", check.ID, check.Tree[:12])
	}
	if context.Len() == 0 {
		return "\nNo package commands or passed checks could be verified for the current workspace."
	}
	return context.String()
}
