package agent

import (
	"strings"
	"testing"
)

// TestScanProjectAtRealRepo runs the /triage scan against the actual BoltCode
// repo to confirm it gathers structure + config files (the input /triage sends
// to the LLM). It's a smoke test that the scan produces useful, non-empty
// context for a real codebase.
func TestScanProjectAtRealRepo(t *testing.T) {
	a := &Agent{Workdir: "../../", RepoRoot: "../../"}
	out := a.scanProjectAt("../../")
	if out == "" {
		t.Fatal("scanProjectAt returned empty for real repo")
	}
	// go.mod is the defining config for this repo — it must be captured.
	if !strings.Contains(out, "go.mod") {
		t.Errorf("scan missing go.mod; output:\n%s", out)
	}
	if !strings.Contains(out, "Project Structure") {
		t.Errorf("scan missing structure header; output:\n%s", out)
	}
}

// TestRunTriageAppendsPrompt verifies /triage builds a triage prompt, appends
// it to the conversation, and signals the loop to continue (return true). It
// must NOT call the API directly.
func TestRunTriageAppendsPrompt(t *testing.T) {
	a := &Agent{
		Workdir:  "../../",
		RepoRoot: "../../",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	before := len(a.Messages)
	got := a.runTriage("worktree")
	if !got {
		t.Fatal("runTriage should return true so the loop calls the API")
	}
	if len(a.Messages) != before+1 {
		t.Fatalf("messages: %d before, %d after (expected +1)", before, len(a.Messages))
	}
	last := a.Messages[len(a.Messages)-1]
	content, _ := last.Content.(string)
	if !strings.Contains(content, "triage mode") {
		t.Errorf("appended message is not a triage prompt: %q", content)
	}
	if !strings.Contains(content, "worktree") {
		t.Errorf("triage prompt should name the scope: %q", content)
	}
}

// TestRunTriageRepoScopeFallback confirms that when RepoRoot is empty, the
// repo scope degrades gracefully to worktree instead of crashing.
func TestRunTriageRepoScopeFallback(t *testing.T) {
	a := &Agent{Workdir: "../../", RepoRoot: ""} // no repo root
	got := a.runTriage("repo")
	if !got {
		t.Fatal("runTriage(repo) with empty RepoRoot should still return true")
	}
}
