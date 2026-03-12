//go:build !windows

package storage

import (
	"fmt"
	"os"
	"syscall"
)

// withFileLock acquires an exclusive file lock, runs fn, and releases the lock.
func (s *Storage) withFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("cannot create lock file: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("cannot acquire lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}
