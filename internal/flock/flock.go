package flock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock represents a file lock.
type Lock struct {
	path string
	file *os.File
}

// New creates a new lock on the given file path.
func New(path string) *Lock {
	return &Lock{path: path}
}

// Lock acquires the lock. It blocks until the lock is acquired or timeout occurs.
func (l *Lock) Lock(timeout time.Duration) error {
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create lock dir: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	l.file = f

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}

		if err != syscall.EAGAIN {
			l.file.Close()
			return fmt.Errorf("flock: %w", err)
		}

		if time.Now().After(deadline) {
			l.file.Close()
			return fmt.Errorf("timeout waiting for lock on %s", l.path)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if l.file == nil {
		return nil
	}
	defer func() {
		l.file.Close()
		l.file = nil
	}()

	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
