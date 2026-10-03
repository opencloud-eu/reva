// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// runConcurrent runs n goroutines that each acquire the lock for key and then
// increment the shared inCritical counter while holding it. If the lock is not
// exclusive, inCritical will exceed 1 and the test fails.
func runConcurrent(t *testing.T, l Locker, ctx context.Context, key string, n int) {
	t.Helper()
	var inCritical atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			unlock, err := l.Lock(ctx, key)
			if err != nil {
				t.Errorf("lock failed: %v", err)
				return
			}
			defer unlock()

			cur := inCritical.Add(1)
			if cur > 1 {
				t.Errorf("critical section not exclusive (inCritical=%d)", cur)
			}
			inCritical.Add(-1)
		}()
	}
	wg.Wait()
}

func TestMemoryLocker_SerializesSameKey(t *testing.T) {
	runConcurrent(t, NewMemoryLocker(), context.Background(), "key", 50)
}

func TestDiskLocker_SerializesSameKey(t *testing.T) {
	dir := t.TempDir()
	runConcurrent(t, NewDiskLocker(dir), context.Background(), "/data.json", 30)

	if _, err := os.Stat(filepath.Join(dir, "data.json"+LockFileSuffix)); err != nil {
		t.Errorf("expected sidecar lock file, got: %v", err)
	}
}

func TestMemoryLocker_DifferentKeysDoNotBlock(t *testing.T) {
	l := NewMemoryLocker()
	ctx := context.Background()

	unlockA, err := l.Lock(ctx, "a")
	if err != nil {
		t.Fatalf("lock a: %v", err)
	}
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB, err := l.Lock(ctx, "b")
		if err != nil {
			t.Errorf("lock b: %v", err)
			return
		}
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Error("different key was blocked")
	}
}

func TestDiskLocker_DifferentKeysDoNotBlock(t *testing.T) {
	dir := t.TempDir()
	l := NewDiskLocker(dir)
	ctx := context.Background()

	unlockA, err := l.Lock(ctx, "/a.json")
	if err != nil {
		t.Fatalf("lock a: %v", err)
	}
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB, err := l.Lock(ctx, "/b.json")
		if err != nil {
			t.Errorf("lock b: %v", err)
			return
		}
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Error("different key was blocked")
	}
}

// TestDiskLocker_ConcurrentCounter verifies that no updates are lost when many
// goroutines perform a read-modify-write under the lock.
func TestDiskLocker_ConcurrentCounter(t *testing.T) {
	dir := t.TempDir()
	l := NewDiskLocker(dir)
	ctx := context.Background()
	file := filepath.Join(dir, "counter.json")

	const n = 30
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			unlock, err := l.Lock(ctx, "/counter.json")
			if err != nil {
				t.Errorf("lock failed: %v", err)
				return
			}
			defer unlock()

			data, _ := os.ReadFile(file)
			var count int
			if len(data) > 0 {
				fmt.Sscanf(string(data), "%d", &count)
			}
			count++
			os.WriteFile(file, []byte(fmt.Sprintf("%d", count)), 0644)
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	var count int
	fmt.Sscanf(string(data), "%d", &count)
	if count != n {
		t.Errorf("expected %d, got %d (lost updates)", n, count)
	}
}
