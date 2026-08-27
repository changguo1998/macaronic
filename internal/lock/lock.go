// Package lock provides a cross-process advisory exclusive lock with
// fail-fast semantics, backed by syscall.Flock on a sibling lock file.
package lock

import (
	"fmt"
	"os"
	"syscall"
)

// LockFile takes an exclusive advisory lock on path, creating the
// file if needed. It returns a release function. The lock is
// fail-fast: if another process holds it, the call returns an error
// immediately instead of blocking.
func LockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s busy (another process holds it): %v", path, err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
