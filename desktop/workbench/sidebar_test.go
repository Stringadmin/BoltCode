package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSidebarArchiveDeleteAndRemoveProject(t *testing.T) {
	root := t.TempDir()
	repository := func() string {
		t.Helper()
		repo := t.TempDir()
		for _, args := range [][]string{
			{"init", "-b", "main"},
			{"-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture"},
		} {
			if _, err := gitText(repo, args...); err != nil {
				t.Fatal(err)
			}
		}
		return repo
	}
	open := func(s *Service, repo string) {
		t.Helper()
		if err := s.openRepository(repo, false); err != nil {
			t.Fatal(err)
		}
	}
	newChat := func(s *Service) Chat {
		t.Helper()
		chat, err := s.NewChat()
		if err != nil {
			t.Fatal(err)
		}
		return chat
	}
	reload := func() *Service {
		t.Helper()
		s, err := New(root, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expectExists := func(path string) {
		t.Helper()
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	expectRemoved := func(path string) {
		t.Helper()
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s to be absent: %v", path, err)
		}
	}

	first, second := repository(), repository()
	service := reload()
	open(service, first)
	keep, remove := newChat(service), newChat(service)
	entry, err := service.chat(remove.ID)
	if err != nil {
		t.Fatal(err)
	}
	workdir, err := service.ensureWorktree(entry)
	if err != nil {
		t.Fatal(err)
	}
	unmerged := filepath.Join(workdir, "unmerged.txt")
	write(unmerged, "keep task output")
	memoryDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	memory := filepath.Join(memoryDir, remove.ID+".json")
	write(memory, "{}")
	entry.Messages = []Message{{Role: "user", Text: "historical prompt"}}
	service.active = &execution{id: remove.ID}
	if service.SetChatArchived(remove.ID, true) == nil || service.DeleteChat(remove.ID) == nil || service.RemoveProject(first, true) == nil {
		t.Fatal("side effects allowed during a running turn")
	}
	service.active = nil

	if err := service.SetChatArchived(remove.ID, true); err != nil {
		t.Fatal(err)
	}
	service = reload()
	entry, err = service.chat(remove.ID)
	if err != nil || !entry.Archived || entry.Messages[0].Text != "historical prompt" {
		t.Fatalf("archive and history did not survive restart: %+v, %v", entry, err)
	}
	if err := service.Send(remove.ID, "continue", "build"); err == nil || err.Error() != "请先移出归档，再继续此对话" {
		t.Fatalf("archived conversation accepted a new turn: %v", err)
	}
	if err := service.SetChatArchived(remove.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteChat(remove.ID); err != nil {
		t.Fatal(err)
	}
	expectRemoved(memory)
	expectExists(unmerged)
	service = reload()
	if chats := service.State().Chats; len(chats) != 1 || chats[0].ID != keep.ID {
		t.Fatalf("removed conversation returned after reload: %+v", chats)
	}

	open(service, second)
	other := newChat(service)
	if err := service.ArchiveProject(first); err != nil {
		t.Fatal(err)
	}
	service = reload()
	if state := service.State(); !sameProject(state.Repository, second) || len(state.ArchivedProjects) != 1 ||
		!sameProject(state.ArchivedProjects[0].Path, first) || len(state.Recent) != 1 {
		t.Fatalf("archive did not survive restart: %+v", state)
	}
	if err := service.OpenRecent(first); err == nil {
		t.Fatal("archived project remained in recent projects")
	}
	if err := service.RestoreProject(first); err != nil {
		t.Fatal(err)
	}
	if state := service.State(); len(state.ArchivedProjects) != 0 || len(state.Recent) != 2 {
		t.Fatalf("restore did not return project to recent projects: %+v", state)
	}
	if err := service.DeleteChat(keep.ID); err == nil {
		t.Fatal("removed a conversation from the wrong project")
	}
	if err := service.RemoveProject(first, false); err != nil {
		t.Fatal(err)
	}
	if err := service.OpenRecent(first); err == nil {
		t.Fatal("a removed project still appeared in recent projects")
	}
	if state := service.State(); !sameProject(state.Repository, second) || len(state.Recent) != 1 || len(state.Chats) != 1 || state.Chats[0].ID != other.ID {
		t.Fatalf("removing a different project changed the current project: %+v", state)
	}
	open(service, first)
	if chats := service.State().Chats; len(chats) != 1 || chats[0].ID != keep.ID {
		t.Fatalf("list-only removal lost history: %+v", chats)
	}
	if err := service.SaveProjectChecks(first, ProjectChecks{Test: "echo test"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ArchiveProject(first); err != nil {
		t.Fatal(err)
	}
	if service.State().Repository != "" {
		t.Fatal("archiving the current project kept it open")
	}
	if err := service.RestoreProject(first); err != nil {
		t.Fatal(err)
	}
	open(service, first)
	if state := service.State(); len(state.Chats) != 1 || state.Chats[0].ID != keep.ID || state.ProjectChecks.Test != "echo test" {
		t.Fatalf("project restore lost chats or checks: %+v", state)
	}
	keepMemory := filepath.Join(memoryDir, keep.ID+".json")
	write(keepMemory, "{}")
	if err := service.RemoveProject(first, true); err != nil {
		t.Fatal(err)
	}
	expectRemoved(keepMemory)
	expectExists(filepath.Join(first, ".git"))
	expectExists(unmerged)
	service = reload()
	if state := service.State(); state.Repository != "" || len(state.Recent) != 1 || !sameProject(state.Recent[0].Path, second) {
		t.Fatalf("removed current project returned after reload: %+v", state)
	}
	open(service, first)
	if state := service.State(); len(state.Chats) != 0 || state.ProjectChecks != (ProjectChecks{}) {
		t.Fatalf("project records survived explicit deletion: %+v", state)
	}
}
