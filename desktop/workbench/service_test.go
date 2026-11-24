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
	"time"
)

func TestConversationReviewAndMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := gitText(repo, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "-b", "main")
	git("config", "user.name", "BoltCode test")
	git("config", "user.email", "boltcode@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("commit", "--allow-empty", "-m", "initial")

	original := git("rev-parse", "HEAD")
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unexpected provider request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch requests.Add(1) {
		case 1:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"我先创建示例文件。\"}}]}\n\n")
			toolResponse(w, "call_write", "write_file", `{"path":"example.txt","content":"hello from BoltCode\n"}`)
		case 2:
			toolResponse(w, "call_echo", "run_command", `{"command":"echo boltcode"}`)
		case 3:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"修改完成。\"}}]}\n\ndata: [DONE]\n\n")
		default:
			http.Error(w, "too many calls", http.StatusBadRequest)
		}
	}))
	defer provider.Close()

	var service *Service
	var approvalSeen, streamed bool
	var completedMS *int64
	var err error
	stateDir := t.TempDir()
	service, err = New(stateDir, func() (string, error) { return repo, nil }, func(event Event) {
		if event.Kind == "confirm" {
			approvalSeen = event.Prompt.Command == "echo boltcode"
			service.AnswerApproval(event.Prompt.ID, true)
		}
		if event.Kind == "text_delta" {
			streamed = strings.Contains(event.Text, "修改完成")
		}
		if event.Kind == "finished" {
			completedMS = event.DurationMS
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if chosen, err := service.ChooseRepository(); !chosen || err != nil {
		t.Fatalf("open repository: chosen=%t, err=%v", chosen, err)
	}
	if err := service.SaveModel("openai", "test-model", provider.URL, "test-key"); err != nil {
		t.Fatal(err)
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(chat.ID, "请创建一个示例文件", "build"); err != nil {
		t.Fatal(err)
	}
	if !approvalSeen || !streamed || requests.Load() != 3 {
		t.Fatalf("unexpected conversation lifecycle: approval=%v streamed=%v requests=%d", approvalSeen, streamed, requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, "example.txt")); !os.IsNotExist(err) {
		t.Fatalf("Agent wrote outside the task worktree: %v", err)
	}
	if got := git("rev-parse", "HEAD"); got != original {
		t.Fatalf("Agent changed main before review: %s", got)
	}

	review, err := service.Review(chat.ID)
	if err != nil || !slices.Equal(review.Files, []ReviewFile{{Path: "example.txt", Added: 1}}) {
		t.Fatalf("review of untracked files: %#v, %v", review, err)
	}
	patch, err := service.Diff(chat.ID, review.Token, "example.txt")
	if err != nil || !strings.Contains(patch, "+hello from BoltCode") {
		t.Fatalf("unexpected patch: %s, %v", patch, err)
	}
	if err := os.WriteFile(filepath.Join(service.worktree(chat.ID), "example.txt"), []byte("review changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Merge(chat.ID, review.Token); err == nil {
		t.Fatal("merge accepted a stale review")
	}
	if got := git("rev-parse", "HEAD"); got != original {
		t.Fatal("stale review changed the target")
	}
	review, err = service.Review(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Merge(chat.ID, review.Token); err != nil {
		t.Fatal(err)
	}
	if got := git("show", "HEAD:example.txt"); got != "review changed" {
		t.Fatalf("merged unexpected content %q", got)
	}
	reopened, err := New(stateDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.State()
	if len(state.Chats) != 1 || !state.Chats[0].Merged || len(state.Chats[0].Messages) < 4 {
		t.Fatalf("conversation did not survive restart: %#v", state)
	}
	messages := state.Chats[0].Messages
	if completedMS == nil || messages[0].DurationMS == nil || *messages[0].DurationMS != *completedMS {
		t.Fatal("saved duration differs from the completion event")
	}
	if !slices.ContainsFunc(messages, func(message Message) bool {
		return message.Role == "assistant" && message.Progress && message.Text == "我先创建示例文件。"
	}) || messages[len(messages)-1].Progress || messages[len(messages)-1].Text != "修改完成。" {
		t.Fatalf("reply and tool progress were not distinguished: %+v", messages)
	}
	if !slices.ContainsFunc(state.Chats[0].Messages, func(message Message) bool {
		return message.Text == "run_command" && strings.Contains(message.Output, "boltcode")
	}) {
		t.Fatal("approved command output was lost")
	}
}

func TestModelTestUsesConfiguredProtocolAndKeepsSettings(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/chat/completions" {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		var request struct {
			Messages []map[string]any `json:"messages"`
			Tools    []any            `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 2 || request.Messages[1]["content"] != "Reply with OK." || len(request.Tools) != 0 {
			t.Errorf("unexpected connection-test payload: %#v", request)
		}
		if r.Header.Get("Authorization") == "Bearer fixture-rejected-key" {
			http.Error(w, "Rejected fixture-rejected-key", http.StatusUnauthorized)
			return
		}
		if header := r.Header.Get("Authorization"); header != "" {
			t.Errorf("unexpected credentials for the local endpoint: %q", header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	service, err := New(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	address := provider.URL + "/api/v3"
	if _, err := service.TestModel("volc", "fixture", address, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TestModel("volc", "fixture", address, "fixture-rejected-key"); err == nil ||
		strings.Contains(err.Error(), "fixture-rejected-key") || !strings.Contains(err.Error(), "API 401") {
		t.Fatalf("test error should be useful and redact credentials: %v", err)
	}
	if _, err := os.Stat(service.modelPath()); !os.IsNotExist(err) {
		t.Fatalf("connection check modified model settings: %v", err)
	}
}

func TestStopKeepsWorktreeAndAllowsFollowUpAfterRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=BoltCode Test", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := gitText(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			toolResponse(w, "pending_command", "run_command", `{"command":"echo should-not-run"}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"继续成功。\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()

	stateDir := t.TempDir()
	approval := make(chan struct{}, 1)
	service, err := New(stateDir, func() (string, error) { return repo, nil }, func(event Event) {
		if event.Kind == "confirm" {
			approval <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Close(service)
	if chosen, err := service.ChooseRepository(); !chosen || err != nil {
		t.Fatalf("open repository: chosen=%t, err=%v", chosen, err)
	}
	if err := service.SaveModel("openai", "fixture", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- service.Send(chat.ID, "验证停止", "build") }()
	select {
	case <-approval:
		service.Stop(chat.ID)
	case <-time.After(10 * time.Second):
		t.Fatal("the command never asked for approval")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not release approval")
	}
	reopened, err := New(stateDir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := reopened.State()
	if len(restored.Chats) != 1 || restored.Chats[0].RunState != "interrupted" || requests.Load() != 1 {
		t.Fatalf("interruption not restored: %#v", restored)
	}
	if err := reopened.OpenRecent(restored.Recent[0].Path); err != nil {
		t.Fatal(err)
	}
	worktree, err := reopened.WorktreePath(chat.ID)
	if err != nil || !sameDirectory(worktree, service.worktree(chat.ID)) {
		t.Fatalf("worktree not restored: %v", err)
	}
	if err := reopened.Send(chat.ID, "继续验证", "build"); err != nil {
		t.Fatal(err)
	}
	current := reopened.State()
	if current.Chats[0].RunState != "done" || requests.Load() != 2 {
		t.Fatalf("follow-up did not complete: %#v", current.Chats[0])
	}
}

func TestChooseRepositoryCancelLeavesCurrentRepository(t *testing.T) {
	service, err := New(t.TempDir(), func() (string, error) { return "", nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.data.Repository = "current-repo"
	chosen, err := service.ChooseRepository()
	if chosen || err != nil || service.data.Repository != "current-repo" {
		t.Fatalf("cancel changed the selection: chosen=%t, err=%v, current=%q", chosen, err, service.data.Repository)
	}
}

func toolResponse(w http.ResponseWriter, id, name, arguments string) {
	delta := map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "type": "function", "id": id, "function": map[string]string{"name": name, "arguments": arguments},
	}}}
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
}
