//go:build windows

package storage

import "os"

// withFileLock on Windows uses a simple lockfile-based mutual exclusion.
// It creates a lock file exclusively; if it already exists, it falls through (best effort).
func (s *Storage) withFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if err != nil {
		// If lock file exists, fall through without locking (best effort)
		return fn()
	}
	defer func() {
		f.Close()
		os.Remove(lockPath)
	}()

	return fn()
}
