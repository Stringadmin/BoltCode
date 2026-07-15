//go:build windows

package fio

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLock holds an open file handle that LockFileEx advisory locks are applied
// to. The lock is exclusive and blocking. It is released when the handle closes
// (including on process exit), so stale locks are impossible — matching the
// flock(2) semantics on Unix.
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

const lockfileExclusiveLock uint32 = 0x00000002

func (l *fileLock) lock() error {
	h := windows.Handle(l.f.Fd())
	var ol windows.Overlapped
	// Lock 1 byte at offset 0, blocking until acquired.
	return windows.LockFileEx(h, lockfileExclusiveLock, 0, 1, 0, &ol)
}

func (l *fileLock) unlock() error {
	h := windows.Handle(l.f.Fd())
	var ol windows.Overlapped
	return windows.UnlockFileEx(h, 0, 1, 0, &ol)
}

func (l *fileLock) close() error {
	return l.f.Close()
}
