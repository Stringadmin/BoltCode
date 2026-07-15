// Package fio provides small filesystem helpers used across BoltCode:
// atomic file writes (so a crash mid-write can't corrupt a metadata file) and
// cross-process file locking (so parallel agents can't race on read-modify-write
// sequences in the store).
package fio

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically: it writes to a uniquely-named
// temp file in the same directory, then renames it over the target. A crash at
// any point leaves either the old file intact or a temp file that never reaches
// the target — never a half-written target. The temp file is created with
// os.CreateTemp (unique name) so two concurrent writers to the same path don't
// collide on a fixed ".tmp" name. The parent directory is created if needed.
//
// On Windows, os.Rename uses MoveFileEx with MOVEFILE_REPLACE_EXISTING (since
// Go 1.5), so overwriting an existing target works cross-platform.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup on any failure path below: close + remove the temp
	// file so we don't litter .tmp-* files in the metadata dir.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// FileLock is a cross-process advisory file lock. On Unix it uses flock(2); on
// Windows, LockFileEx. The lock is exclusive and blocking. It is released when
// the underlying handle closes — including on process exit — so a crashed
// process never leaves a stale lock behind. This is what lets parallel BoltCode
// agents (separate processes, separate worktrees, but sharing one .boltcode/
// store) serialize read-modify-write sequences without lost updates.
type FileLock struct {
	fl *fileLock
}

// NewFileLock opens (creating if needed) a lock file at path. The lock is NOT
// held until Lock is called.
func NewFileLock(path string) (*FileLock, error) {
	fl, err := newFileLock(path)
	if err != nil {
		return nil, err
	}
	return &FileLock{fl: fl}, nil
}

// Lock blocks until the exclusive lock is acquired.
func (l *FileLock) Lock() error {
	return l.fl.lock()
}

// Unlock releases the lock. It is safe to call on an unlocked or already-closed
// FileLock (the underlying call is idempotent on both platforms).
func (l *FileLock) Unlock() error {
	return l.fl.unlock()
}

// Close releases the lock (if held) and closes the underlying file handle. It
// should be called when the lock is no longer needed.
func (l *FileLock) Close() error {
	return l.fl.close()
}
