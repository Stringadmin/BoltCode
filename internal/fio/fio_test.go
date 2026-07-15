package fio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt") // subdir doesn't exist yet

	if err := WriteFileAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first" {
		t.Fatalf("after first write = %q", string(got))
	}

	// Overwrite — must replace, not append.
	if err := WriteFileAtomic(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "second" {
		t.Fatalf("after overwrite = %q, want %q", string(got), "second")
	}
}

func TestWriteFileAtomicNoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := WriteFileAtomic(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestFileLockSerializes(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	fl1, err := NewFileLock(lockPath)
	if err != nil {
		t.Fatalf("newFileLock 1: %v", err)
	}
	defer fl1.Close()
	if err := fl1.Lock(); err != nil {
		t.Fatalf("lock 1: %v", err)
	}
	// Unlock then re-lock should work (the lock is reusable).
	if err := fl1.Unlock(); err != nil {
		t.Fatalf("unlock 1: %v", err)
	}
	if err := fl1.Lock(); err != nil {
		t.Fatalf("re-lock 1: %v", err)
	}

	// A second lock on the same file in the same process: on Unix flock is
	// per-fd, so a second FileLock (different fd) blocks. We can't easily test
	// blocking in a unit test without timing out, so just verify a fresh lock
	// object can be created and that close is clean.
	fl2, err := NewFileLock(lockPath)
	if err != nil {
		t.Fatalf("newFileLock 2: %v", err)
	}
	if err := fl2.Close(); err != nil {
		t.Fatalf("close 2: %v", err)
	}
}
