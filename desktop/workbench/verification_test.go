package workbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProjectGuidanceAndVerificationFollowTheWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	repo, root := t.TempDir(), t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		if _, err := gitText(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(repo, "init", "-b", "main")
	write(repo, "AGENTS.md", "guidance-original\n")
	write(repo, "CLAUDE.md", "guidance-fallback\n")
	write(repo, "candidate.txt", "before\n")
	git(repo, "add", "-A")
	git(repo, "-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")

	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		call := requests.Add(1)
		if call > 3 {
			t.Error("running a check called the model")
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		system := ""
		for _, message := range body.Messages {
			if message.Role == "system" || message.Role == "developer" {
				system += message.Content
			}
		}
		rules := []string{"guidance-original", "guidance-reloaded", "guidance-fallback"}
		for i, rule := range rules {
			if strings.Contains(system, rule) != (i == int(call)-1) {
				t.Errorf("request %d received the wrong project rules", call)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"完成\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()

	service, err := New(root, func() (string, error) { return repo, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := service.ChooseRepository(); !ok || err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if err := service.SaveModel("openai", "fixture", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	checks := ProjectChecks{Test: "echo test output", Typecheck: "echo type error && exit 7"}
	if err := service.SaveProjectChecks(repo, checks); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveProjectChecks(t.TempDir(), ProjectChecks{}); err == nil {
		t.Fatal("settings written to the wrong project")
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(chat.ID, "检查规则", "ask"); err != nil {
		t.Fatal(err)
	}
	workdir, err := service.WorktreePath(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(workdir, "AGENTS.md", "guidance-reloaded\n")
	if err := service.Send(chat.ID, "再读规则", "ask"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workdir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := service.Send(chat.ID, "读取兼容规则", "ask"); err != nil {
		t.Fatal(err)
	}
	rules := []string{}
	for _, message := range service.State().Chats[0].Messages {
		rules = append(rules, message.ProjectRules...)
	}
	if !slices.Equal(rules, []string{"AGENTS.md", "AGENTS.md", "CLAUDE.md"}) {
		t.Fatalf("wrong rule provenance: %q", rules)
	}

	review, err := service.Review(chat.ID)
	if err != nil || len(review.Verification.Checks) != 2 || review.Verification.Checks[0].Status != "pending" {
		t.Fatalf("initial verification: %+v, %v", review.Verification, err)
	}
	results, err := service.RunChecks(chat.ID, review.Token)
	if err != nil || len(results.Checks) != 2 {
		t.Fatalf("run checks: %+v, %v", results, err)
	}
	test, typecheck := results.Checks[0], results.Checks[1]
	if test.Status != "passed" || test.Result.ExitCode == nil || *test.Result.ExitCode != 0 ||
		!strings.Contains(test.Result.Output, "test output") || typecheck.Status != "failed" ||
		typecheck.Result.ExitCode == nil || *typecheck.Result.ExitCode != 7 || !strings.Contains(typecheck.Result.Output, "type error") {
		t.Fatalf("incorrect process results: %+v / %+v", test.Result, typecheck.Result)
	}

	write(workdir, "candidate.txt", "source edited after check\n")
	status, err := service.CheckStatus(chat.ID, review.Token)
	if err != nil || !status.Changed || status.Checks[0].Status != "stale" || status.Checks[1].Status != "stale" {
		t.Fatalf("edit left a valid result: %+v, %v", status, err)
	}
	if _, err := service.RunChecks(chat.ID, review.Token); err == nil {
		t.Fatal("a stale Diff was used for a new verification")
	}
	checks.Typecheck = "echo type checks pass"
	if err := service.SaveProjectChecks(repo, checks); err != nil {
		t.Fatal(err)
	}
	review, err = service.Review(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	results, err = service.RunChecks(chat.ID, review.Token)
	if err != nil || results.Checks[0].Status != "passed" || results.Checks[1].Status != "passed" {
		t.Fatalf("rerun did not pass: %+v, %v", results, err)
	}
	restarted, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	review, err = restarted.Review(chat.ID)
	if err != nil || restarted.State().ProjectChecks != checks || len(review.Verification.Checks) != 2 || review.Verification.Checks[1].Status != "passed" {
		t.Fatalf("restart lost verification and configuration: %+v, %v", review.Verification, err)
	}

	checks.Test = "echo check created a file > generated.txt"
	if err := restarted.SaveProjectChecks(repo, checks); err != nil {
		t.Fatal(err)
	}
	status, err = restarted.CheckStatus(chat.ID, review.Token)
	if err != nil || status.Changed || status.Checks[0].Status != "stale" || status.Checks[1].Status != "passed" {
		t.Fatalf("changed command should only invalidate its own check: %+v, %v", status, err)
	}
	results, err = restarted.RunChecks(chat.ID, review.Token)
	if err != nil || !results.Changed || results.Checks[0].Status != "stale" || results.Checks[1].Status != "stale" ||
		!results.Checks[1].Result.FinishedAt.Equal(status.Checks[1].Result.FinishedAt) {
		t.Fatalf("workspace changed during validation but checks continued or results stayed current: %+v, %v", results, err)
	}
}
