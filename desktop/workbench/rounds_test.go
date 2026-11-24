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

func TestReferencesAndCheckpointSurviveRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, repo := t.TempDir(), t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := gitText(dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(dir, name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	git(repo, "init", "-b", "main")
	write(repo, "参考 文件.txt", "实际发送的内容\n")
	write(repo, "remove.txt", "restore this\n")
	write(repo, "change.txt", "original\n")
	write(repo, "binary.bin", "\x00\x00data")
	write(repo, "large.txt", strings.Repeat("x", maxFileBytes+1))
	write(repo, ".gitignore", "ignored.txt\n")
	git(repo, "add", "-A")
	git(repo, "-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	originalHead := git(repo, "rev-parse", "HEAD")
	write(repo, "local-only.txt", "原仓库未提交\n")

	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls.Add(1) {
		case 1:
			if !slices.ContainsFunc(request.Messages, func(message struct{ Role, Content string }) bool {
				return message.Role == "user" && strings.Contains(message.Content, "参考 文件.txt") &&
					strings.Contains(message.Content, "实际发送的内容")
			}) {
				t.Error("model did not receive referenced file contents")
			}
			toolResponse(w, "modify", "write_file", `{"path":"change.txt","content":"after turn\n"}`)
		case 2:
			toolResponse(w, "add", "write_file", `{"path":"added.txt","content":"new\n"}`)
		case 3:
			toolResponse(w, "remove", "delete_file", `{"path":"remove.txt"}`)
		default:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"修改完成。\"}}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer provider.Close()
	service, err := New(root, func() (string, error) { return repo, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveModel("openai", "fixture", provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	if opened, err := service.ChooseRepository(); err != nil || !opened {
		t.Fatalf("open repository: %v", err)
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	names, err := service.SearchFiles(chat.ID, "参考")
	if err != nil || !slices.Equal(names, []string{"参考 文件.txt"}) {
		t.Fatalf("search in Git HEAD: %q, %v", names, err)
	}
	names, err = service.SearchFiles(chat.ID, "local-only")
	if err != nil || len(names) != 0 {
		t.Fatalf("search returned uncommitted original-repo changes: %q, %v", names, err)
	}
	for _, file := range []string{"../secret.txt", ".git", "local-only.txt", "binary.bin", "large.txt"} {
		if err := service.SendWithReferences(chat.ID, "不应该发送", "build", []string{file}); err == nil {
			t.Fatalf("accepted unavailable reference %q", file)
		}
	}
	if calls.Load() != 0 || len(service.State().Chats[0].Messages) != 0 {
		t.Fatal("invalid references reached the model or changed the conversation")
	}
	workdir, err := service.WorktreePath(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	write(workdir, "change.txt", "preexisting manual edit\n")
	write(workdir, "ignored.txt", "keep ignored file\n")
	if err := service.SendWithReferences(chat.ID, "请按参考文件修改", "build", []string{"参考 文件.txt"}); err != nil {
		t.Fatal(err)
	}
	first := service.State().Chats[0].Messages[0]
	if !first.Undoable || first.TurnID == "" || !slices.Equal(first.References, []string{"参考 文件.txt"}) || first.Text != "请按参考文件修改" {
		t.Fatalf("turn metadata missing or transcript contains file payload: %+v", first)
	}
	names, err = service.SearchFiles(chat.ID, "added.txt")
	if err != nil || !slices.Equal(names, []string{"added.txt"}) {
		t.Fatalf("search in active worktree: %q, %v", names, err)
	}
	git(repo, "gc", "--prune=now")
	reopened, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := reopened.PreviewUndo(chat.ID, first.TurnID)
	if err != nil || !preview.Allowed || len(preview.Files) != 3 {
		t.Fatalf("persisted undo preview: %+v, %v", preview, err)
	}
	write(workdir, "unrelated.txt", "keep my new file\n")
	write(workdir, "change.txt", "edited since preview\n")
	if err := reopened.UndoTurn(chat.ID, first.TurnID); err == nil || read(workdir, "added.txt") != "new" {
		t.Fatalf("stale preview allowed a partial or unsafe undo: %v", err)
	}
	preview, err = reopened.PreviewUndo(chat.ID, first.TurnID)
	if err != nil || preview.Allowed || !slices.ContainsFunc(preview.Files, func(file UndoFile) bool {
		return file.Path == "change.txt" && file.Block != ""
	}) {
		t.Fatalf("manual modification not shown as blocked: %+v, %v", preview, err)
	}
	write(workdir, "change.txt", "after turn\n")
	git(workdir, "add", "--", "change.txt")
	if err := reopened.UndoTurn(chat.ID, first.TurnID); err == nil {
		t.Fatal("undo accepted a staged change")
	}
	git(workdir, "restore", "--staged", "change.txt")
	if err := reopened.UndoTurn(chat.ID, first.TurnID); err != nil {
		t.Fatal(err)
	}
	if read(workdir, "change.txt") != "preexisting manual edit" ||
		read(workdir, "remove.txt") != "restore this" ||
		read(workdir, "unrelated.txt") != "keep my new file" ||
		read(workdir, "ignored.txt") != "keep ignored file" {
		t.Fatal("undo lost working files")
	}
	if _, err := os.Stat(filepath.Join(workdir, "added.txt")); !os.IsNotExist(err) {
		t.Fatalf("turn's new file still exists: %v", err)
	}
	if git(repo, "rev-parse", "HEAD") != originalHead || read(repo, "change.txt") != "original" {
		t.Fatal("undo changed original repository")
	}
	reopened, err = New(root, nil, nil)
	if err != nil || !reopened.State().Chats[0].Messages[0].Undone {
		t.Fatalf("undo record did not survive restart: %v", err)
	}
}
