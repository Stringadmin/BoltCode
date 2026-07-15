//go:build !windows

package fio

import (
	"os"

	"golang.org/x/sys/unix"
)

// fileLock holds an open file descriptor that flock(2) advisory locks are
// applied to. The lock is exclusive (LOCK_EX) and blocking. It is released
// automatically when the descriptor closes — including on process exit — so
// stale locks are impossible.
type fileLock struct {
	f *os.File
}

func newFileLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) lock() error {
	return unix.Flock(int(l.f.Fd()), unix.LOCK_EX)
}

func (l *fileLock) unlock() error {
	return unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
}

func (l *fileLock) close() error {
	return l.f.Close()
}
