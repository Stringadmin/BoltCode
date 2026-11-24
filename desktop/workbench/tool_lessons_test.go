package workbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestToolRepairsBecomeProjectLessons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, repo := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"}, {"add", "README.md"},
		{"-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture"},
	} {
		if _, err := gitText(repo, args...); err != nil {
			t.Fatal(err)
		}
	}

	type step struct{ tool, arguments string }
	steps := []step{
		{"read_file", `{"path":`},
		{"read_file", `{"path":"README.md"}`},
		{"read_file", `{"path":"README.md","limit":1}`},
		{"read_file", `{"path":"README.md","limit":"1"}`},
		{"search_code", `{}`},
		{"run_command", `{"command":"echo declined"}`},
	}
	if runtime.GOOS == "windows" {
		steps = append(steps,
			step{"run_command", `{"command":"Get-ChildItem -Name"}`},
			step{"run_command", `{"command":"powershell -NoProfile -Command \"Get-ChildItem -Name\""}`},
		)
	}
	var callCount atomic.Int32
	observed := make(chan string, len(steps)+3)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Effort   string `json:"reasoning_effort"`
			Messages []struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		call := int(callCount.Add(1))
		if request.Effort != "max" {
			t.Errorf("extra high for DeepSeek sent %q", request.Effort)
		}
		if call > 1 && call <= len(steps)+1 && !slices.ContainsFunc(request.Messages, func(message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		}) bool {
			return message.Role == "assistant" && message.Reasoning == "fixture reasoning"
		}) {
			t.Error("DeepSeek tool continuation lost reasoning content")
		}
		for _, message := range request.Messages {
			if message.Role == "system" {
				_, hint, _ := strings.Cut(message.Content, "## Verified tool recovery lessons")
				observed <- hint
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if call <= len(steps) {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"fixture reasoning\"}}]}\n\n")
			current := steps[call-1]
			toolResponse(w, fmt.Sprintf("step%d", call), current.tool, current.arguments)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()

	var service *Service
	emit := func(event Event) {
		if event.Kind == "confirm" {
			service.AnswerApproval(event.Prompt.ID, event.Prompt.Command != "echo declined")
		}
	}
	var err error
	service, err = New(root, func() (string, error) { return repo, nil }, emit)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := service.ChooseRepository(); !opened || err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if err := service.SaveModel("deepseek", "deepseek-v4-pro", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.SetModelReasoningEffort(service.State().Model.ID, "xhigh"); err != nil {
		t.Fatal(err)
	}
	first, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(first.ID, "Exercise tool repair", "build"); err != nil {
		t.Fatal(err)
	}
	for range len(steps) + 1 {
		if len(observed) == 0 || <-observed != "" {
			t.Fatal("new lessons injected into the training turn")
		}
	}
	service, err = New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if service.State().Model.ReasoningEffort != "xhigh" {
		t.Fatal("model effort did not survive reopening")
	}
	memory, err := service.ReadWorkspaceMemory(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, lesson := range memory.ToolLessons {
		kinds = append(kinds, lesson.Kind)
	}
	expected := []string{"json_object", "argument_type"}
	if runtime.GOOS == "windows" {
		expected = append(expected, "windows_shell")
	}
	if !slices.Equal(kinds, expected) {
		t.Fatalf("saved lessons = %q, want %q", kinds, expected)
	}
	second, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(second.ID, "Read a file", "ask"); err != nil {
		t.Fatal(err)
	}
	if len(observed) == 0 {
		t.Fatal("model request was not captured")
	}
	prompt := <-observed
	if !strings.Contains(prompt, "read_file: send arguments as one valid JSON object") ||
		!strings.Contains(prompt, "read_file: pass limit as JSON type string") ||
		strings.Contains(prompt, "run_command") || strings.Contains(prompt, "README.md") {
		t.Fatalf("wrong lesson context in new Ask chat: %q", prompt)
	}
}
