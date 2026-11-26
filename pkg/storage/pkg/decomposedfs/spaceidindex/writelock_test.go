package spaceidindex

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// TestWriteLocksAreRemoved checks that the in-process lock of an index file is removed again once all writers are
// done, so that the table does not grow with every index file a long running process has ever written.
func TestWriteLocksAreRemoved(t *testing.T) {
	root := t.TempDir()
	i := New(root, "by-type")
	if err := i.Init(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	indexes := map[string]bool{}
	for w := range 8 {
		// half of the writers share one index file, the others write their own
		index := "shared"
		if w%2 == 1 {
			index = fmt.Sprintf("own-%d", w)
		}
		indexes[index] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range 20 {
				if err := i.Add(index, fmt.Sprintf("w%d-%d", w, n), "target"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// only look at the index files of this test, other tests of the package may write at the same time
	writeLocksMu.Lock()
	defer writeLocksMu.Unlock()
	for index := range indexes {
		if _, ok := writeLocks[filepath.Join(root, "by-type", index+".mpk")]; ok {
			t.Errorf("write lock of index %q was not removed", index)
		}
	}
}

// TestLockIndexExcludes checks that the writers of one index file in a process never hold the in-process lock at the
// same time, also while entries are removed and created again.
func TestLockIndexExcludes(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "same.mpk")

	var inside atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				unlock := lockIndex(indexPath)
				if n := inside.Add(1); n > 1 {
					t.Errorf("%d writers hold the lock of the same index", n)
				}
				runtime.Gosched()
				inside.Add(-1)
				unlock()
			}
		}()
	}
	wg.Wait()
}
