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

func TestWorkspaceMemoryAcrossChatsChecksAndUndo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, repo := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if _, err := gitText(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := func(manager string) string {
		return fmt.Sprintf("{\"packageManager\":%q,\"scripts\":{\"build\":\"echo build\",\"test\":\"echo test\"}}\n", manager)
	}
	const npm, pnpm = "npm@10.9.2", "pnpm@10.15.0"
	git("init", "-b", "main")
	git("config", "user.name", "BoltCode")
	git("config", "user.email", "boltcode@example.invalid")
	git("config", "commit.gpgsign", "false")
	write(repo, "package.json", manifest(npm))
	write(repo, "README.md", "original\n")
	git("add", "-A")
	git("commit", "-m", "fixture")

	var migrate atomic.Bool
	requests := make(chan string, 32)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		for _, message := range body.Messages {
			if message.Role == "system" {
				var system string
				if err := json.Unmarshal(message.Content, &system); err != nil {
					t.Error(err)
				}
				_, memory, _ := strings.Cut(system, "## Current workspace memory\n")
				requests <- memory
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if migrate.Swap(false) {
			args, _ := json.Marshal(map[string]string{"path": "package.json", "content": manifest(pnpm)})
			toolResponse(w, "migration", "write_file", string(args))
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()

	service, err := New(root, func() (string, error) { return repo, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := service.ChooseRepository(); err != nil || !opened {
		t.Fatalf("open repository: %v", err)
	}
	if err := service.SaveModel("openai", "fixture", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveProjectChecks(repo, ProjectChecks{Test: "git diff --check"}); err != nil {
		t.Fatal(err)
	}
	newChat := func() Chat {
		t.Helper()
		chat, err := service.NewChat()
		if err != nil {
			t.Fatal(err)
		}
		return chat
	}
	send := func(chat Chat, mode, manager string, passed, edit bool) Message {
		t.Helper()
		count := 1
		if edit {
			migrate.Store(true)
			count = 2
		}
		if err := service.Send(chat.ID, "Continue with the current project settings", mode); err != nil {
			t.Fatal(err)
		}
		if len(requests) != count {
			t.Fatalf("got %d requests, want %d", len(requests), count)
		}
		for range count {
			context := <-requests
			switch {
			case manager == "" && !strings.Contains(context, "No package commands"):
				t.Fatalf("stale package commands reached model: %q", context)
			case manager != "" && !strings.Contains(context, "package_manager: "+manager):
				t.Fatalf("model saw incorrect manager: %q, want %s", context, manager)
			}
			for _, other := range []string{npm, pnpm} {
				if other != manager && strings.Contains(context, "package_manager: "+other) {
					t.Fatalf("inactive version reached model: %q", context)
				}
			}
			if strings.Contains(context, "Configured test check passed") != passed || strings.Contains(context, "git diff --check") {
				t.Fatalf("model saw incorrect check information: %q", context)
			}
		}
		for _, entry := range service.State().Chats {
			if entry.ID == chat.ID {
				for _, message := range slices.Backward(entry.Messages) {
					if message.Role == "user" {
						return message
					}
				}
			}
		}
		t.Fatal("chat was not persisted")
		return Message{}
	}
	read := func(chat Chat) WorkspaceMemory {
		t.Helper()
		memory, err := service.ReadWorkspaceMemory(chat.ID)
		if err != nil {
			t.Fatal(err)
		}
		return memory
	}

	a, b := newChat(), newChat()
	first := send(a, "build", npm, false, true)
	if !first.Undoable || first.WorkspaceMemory == nil || len(first.WorkspaceMemory.Facts) != 3 {
		t.Fatalf("initial memory or checkpoint missing: %+v", first)
	}
	baseline := first.WorkspaceMemory.Facts
	migrated := read(a)
	if len(migrated.Facts) != 3 || migrated.Facts[0].Value != pnpm ||
		migrated.Facts[1].Value != "pnpm run build" || migrated.Facts[2].Value != "pnpm run test" ||
		migrated.Facts[0].FirstSeen.IsZero() {
		t.Fatalf("migration not recorded: %+v", migrated)
	}
	review, err := service.Review(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunChecks(a.ID, review.Token)
	if err != nil || len(result.Checks) != 1 || result.Checks[0].Status != "passed" {
		t.Fatalf("check did not pass: %+v, %v", result, err)
	}
	if len(read(a).Checks) != 1 {
		t.Fatal("check was not exposed with the exact snapshot")
	}
	send(b, "ask", npm, false, false)
	send(a, "ask", pnpm, true, false)

	workdir, err := service.WorktreePath(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(workdir, "README.md", "unrelated source edit\n")
	if got := read(a); !slices.Equal(got.Facts, migrated.Facts) || len(got.Checks) != 0 {
		t.Fatalf("README edit should keep fields but invalidate the check: %+v", got)
	}
	write(workdir, "README.md", "original\n")
	service, err = New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(read(a).Checks) != 1 {
		t.Fatal("the exact-snapshot check did not survive restart")
	}
	if err := service.UndoTurn(a.ID, first.TurnID); err != nil {
		t.Fatal(err)
	}
	if got := read(a); !slices.Equal(got.Facts, baseline) || len(got.Checks) != 0 {
		t.Fatalf("undo should select the original stored versions: %+v", got)
	}
	send(a, "ask", npm, false, false)
	write(workdir, "package.json", "{\"scripts\":{\"test\":\"echo test\"}}\n")
	inactive := send(a, "ask", "", false, false)
	if inactive.WorkspaceMemory != nil || len(read(a).Facts) != 0 {
		t.Fatal("unsupported evidence activated historical facts")
	}
	write(workdir, "package.json", manifest(npm))

	send(a, "build", npm, false, true)
	review, err = service.Review(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Merge(a.ID, review.Token); err != nil {
		t.Fatal(err)
	}
	send(newChat(), "ask", pnpm, false, false)
	send(b, "ask", npm, false, false)
}
