package workbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Stringadmin/BoltCode/internal/keyring"
)

func TestModelProfilesKeepSeparateCredentialsAndMigrate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	service, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := map[string]string{"/first/v1/chat/completions": "first-key", "/second/v1/chat/completions": "second-key"}
		key, ok := expected[r.URL.Path]
		if !ok || r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "unexpected endpoint or key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()

	firstKey, err := keyring.Wrap("first-key")
	if err != nil {
		t.Fatal(err)
	}
	legacy := savedModel{Model: Model{Provider: "openai", Name: "first", BaseURL: provider.URL + "/first"}, Key: firstKey}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.modelPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	state := service.State()
	if !state.Model.Ready || state.Model.ID != legacyModelID || len(state.Models) != 1 {
		t.Fatalf("legacy model was not loaded: %+v", state)
	}
	// A second channel cannot reuse credentials from the active profile.
	if _, err := service.SaveModelProfile("", "", "openai", "missing", "https://example.invalid", ""); err == nil {
		t.Fatal("saved a new remote profile with no key")
	}
	second, err := service.SaveModelProfile("", "第二渠道", "openai", "second", provider.URL+"/second", "second-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.TestModelProfile(second.ID, "openai", "second", provider.URL+"/second", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TestModelProfile(legacyModelID, "openai", "first", provider.URL+"/first", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareModel(&legacy, "openai", "first", "https://different.invalid", ""); err == nil {
		t.Fatal("reused a key after changing endpoint")
	}
	if state := service.State(); state.Model.ID != legacyModelID || len(state.Models) != 2 {
		t.Fatalf("adding a profile replaced the active profile: %+v", state)
	}
	if err := service.SelectModel(second.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state := reopened.State(); state.Model.ID != second.ID || state.Model.Label != "第二渠道" {
		t.Fatalf("selection was not persisted: %+v", state.Model)
	}
	config, err := reopened.agentConfig()
	if err != nil || config.Model != "second" || config.APIKey != "second-key" {
		t.Fatalf("active model or key did not reopen: %v", err)
	}
	data, err := os.ReadFile(service.modelPath())
	if err != nil || strings.Contains(string(data), "first-key") || strings.Contains(string(data), "second-key") {
		t.Fatal("a profile key was written in plaintext")
	}
	summary, err := json.Marshal(reopened.State())
	if err != nil || strings.Contains(string(summary), "first-key") || strings.Contains(string(summary), "second-key") {
		t.Fatal("desktop state exposed a model key")
	}
}

func TestDesktopAccessOnlySkipsApprovalForRoutineCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, repo := t.TempDir(), t.TempDir()
	initRepo := func(path string) {
		t.Helper()
		for _, args := range [][]string{
			{"init", "-b", "main"},
			{"-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial"},
		} {
			if _, err := gitText(path, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	initRepo(repo)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls.Add(1) {
		case 1:
			toolResponse(w, "restricted", "run_command", `{"command":"echo restricted-output"}`)
		case 3:
			toolResponse(w, "full", "run_command", `{"command":"echo full-output"}`)
		case 4:
			toolResponse(w, "diagnostic", "get_diagnostics", `{"command":"echo diagnostic-output"}`)
		case 5:
			// The literal echo payload matches the high-risk string rule but cannot delete anything.
			toolResponse(w, "high_risk", "run_command", `{"command":"echo rm -rf /"}`)
		default:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"完成\"}}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer provider.Close()
	var service *Service
	var approvals, outputs []string
	var changeWhileRunning error
	emit := func(event Event) {
		if event.Kind == "confirm" {
			approvals = append(approvals, event.Prompt.Command)
			changeWhileRunning = service.SetFullAccess(repo, false)
			service.AnswerApproval(event.Prompt.ID, event.Prompt.Command == "echo restricted-output")
		}
		if event.Kind == "tool_finished" {
			outputs = append(outputs, event.Output)
		}
	}
	service, err := New(root, func() (string, error) { return repo, nil }, emit)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := service.ChooseRepository(); err != nil || !opened {
		t.Fatalf("open repo: %v", err)
	}
	if err := service.SaveModel("openai", "fixture", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if service.State().FullAccess {
		t.Fatal("first launch must default to restricted access")
	}
	if err := service.Send(chat.ID, "跑一条常规命令", "build"); err != nil {
		t.Fatal(err)
	}
	originalWorktree, err := os.Stat(service.worktree(chat.ID))
	if err != nil {
		t.Fatal(err)
	}
	checks := ProjectChecks{Test: "echo configured check"}
	if err := service.SaveProjectChecks(repo, checks); err != nil {
		t.Fatal(err)
	}
	if err := service.SetFullAccess(repo, true); err != nil || !service.State().FullAccess {
		t.Fatalf("enable full access: %v", err)
	}
	service, err = New(root, nil, emit)
	if err != nil {
		t.Fatal(err)
	}
	if state := service.State(); !state.FullAccess || state.ProjectChecks != checks {
		t.Fatalf("project access and checks were not restored: %+v", state)
	}
	if err := service.SaveProjectChecks(repo, ProjectChecks{}); err != nil || !service.State().FullAccess {
		t.Fatalf("clearing check commands changed access: %v", err)
	}
	if err := service.Send(chat.ID, "执行命令和诊断", "build"); err != nil {
		t.Fatal(err)
	}
	reusedWorktree, err := os.Stat(service.worktree(chat.ID))
	if err != nil || !os.SameFile(originalWorktree, reusedWorktree) {
		t.Fatalf("resuming recreated the worktree directory: %v", err)
	}
	if !slices.Equal(approvals, []string{"echo restricted-output", "echo rm -rf /"}) || changeWhileRunning == nil {
		t.Fatalf("unexpected approvals %q or access changed during a task: %v", approvals, changeWhileRunning)
	}
	if !slices.Contains(outputs, "restricted-output") || !slices.Contains(outputs, "full-output") ||
		!slices.Contains(outputs, "diagnostic-output") || !slices.Contains(outputs, "user denied this command") {
		t.Fatalf("command and diagnostic output missing: %q", outputs)
	}
	other := t.TempDir()
	initRepo(other)
	if err := service.openRepository(other, false); err != nil || service.State().FullAccess {
		t.Fatalf("another project inherited the grant: %v", err)
	}
	if err := service.SetFullAccess(repo, true); err == nil {
		t.Fatal("a stale grant changed the selected project's access")
	}
	if err := service.OpenRecent(repo); err != nil || !service.State().FullAccess {
		t.Fatalf("reopening the approved project lost the grant: %v", err)
	}
	if err := service.SetFullAccess(repo, false); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root, nil, nil)
	if err != nil || reopened.State().FullAccess {
		t.Fatalf("revocation did not survive restart: %v", err)
	}
}
