package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReviewReusesOnlyAnUnchangedSnapshot(t *testing.T) {
	repo := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := gitText(dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write := func(dir, name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(repo, "init", "-b", "main")
	write(repo, "selected.txt", "before\n")
	git(repo, "add", "-A")
	git(repo, "-c", "user.name=BoltCode", "-c", "user.email=boltcode@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")

	service, err := New(t.TempDir(), func() (string, error) { return repo, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := service.ChooseRepository(); err != nil || !opened {
		t.Fatalf("open repository: %v", err)
	}
	chat, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := service.chat(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	workdir, err := service.ensureWorktree(thread)
	if err != nil {
		t.Fatal(err)
	}
	write(workdir, "first.txt", "added file\n")
	write(workdir, "selected.txt", "after1\n")
	beforeIndex := git(workdir, "write-tree")

	first, err := service.OpenReview(chat.ID, "selected.txt")
	if err != nil || len(first.Files) != 2 || first.Selected != "selected.txt" ||
		first.PatchError != "" || !strings.Contains(first.Patch, "+after1") {
		t.Fatalf("initial list and patch: %+v, %v", first, err)
	}
	reopened, err := service.OpenReview(chat.ID, "selected.txt")
	if err != nil || reopened.Token != first.Token || reopened.Patch != first.Patch {
		t.Fatalf("unchanged review did not reuse snapshot: %+v, %v", reopened, err)
	}
	if git(workdir, "write-tree") != beforeIndex {
		t.Fatal("review changed the real Git index")
	}

	// Keep the same size and modification time to catch content changes that
	// a timestamp-only refresh would miss.
	path := filepath.Join(workdir, "selected.txt")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	write(workdir, "selected.txt", "after2\n")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := service.Merge(chat.ID, first.Token); err == nil {
		t.Fatal("merge accepted a stale review")
	}
	updated, err := service.OpenReview(chat.ID, "selected.txt")
	if err != nil || updated.Token == first.Token || !strings.Contains(updated.Patch, "+after2") {
		t.Fatalf("edit with preserved timestamp did not invalidate cache: %+v, %v", updated, err)
	}
	if _, err := service.Diff(chat.ID, first.Token, "selected.txt"); err == nil {
		t.Fatal("stale token still read a patch")
	}
	if err := os.Remove(filepath.Join(workdir, "first.txt")); err != nil {
		t.Fatal(err)
	}
	fallback, err := service.OpenReview(chat.ID, "first.txt")
	if err != nil || fallback.Selected != "selected.txt" || fallback.PatchError != "" {
		t.Fatalf("missing selection did not fall back to current files: %+v, %v", fallback, err)
	}
}
