package lock

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestLockFailFast verifies fail-fast semantics: only one goroutine
// can hold the lock at a time and others fail immediately (-race-safe).
func TestLockFailFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.mac.run.lock")
	var (
		held  int
		busy  int
		mu    sync.Mutex
		group sync.WaitGroup
	)
	const n = 16
	for i := 0; i < n; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			release, err := LockFile(path)
			if err != nil {
				mu.Lock()
				busy++
				mu.Unlock()
				return
			}
			mu.Lock()
			held++
			mu.Unlock()
			// hold it briefly so contention is plausible
			time.Sleep(2 * time.Millisecond)
			release()
		}()
	}
	group.Wait()
	if held != 1 {
		t.Fatalf("expected exactly 1 lock holder, held=%d busy=%d", held, busy)
	}
	if busy != n-1 {
		t.Errorf("expected %d busy, got %d", n-1, busy)
	}
	// after release, can re-lock
	rel, err := LockFile(path)
	if err != nil {
		t.Fatalf("relock failed: %v", err)
	}
	rel()
}
