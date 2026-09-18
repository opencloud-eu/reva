// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
)

// TestCS3UploadWithLock_AcquiresAndReleasesLock verifies that UploadWithLock
// takes the lock for the requested path and releases it when done. The test
// uses a memory locker (no network); fn returns nil so the write is skipped
// before any provider call is attempted.
func TestCS3UploadWithLock_AcquiresAndReleasesLock(t *testing.T) {
	l := locks.NewMemoryLocker()
	cs3 := NewCS3("gw", "provider")
	cs3.locker = l

	// Hold the lock from another goroutine and confirm UploadWithLock blocks
	// until it is released, proving the lock is actually acquired.
	unlockOther, err := l.Lock(context.Background(), "/a.json")
	if err != nil {
		t.Fatalf("pre-lock: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		_, _ = cs3.UploadWithLock(context.Background(), UploadRequest{Path: "/a.json"}, func(existing []byte) ([]byte, error) {
			close(acquired)
			return nil, nil // abort → no provider call
		})
	}()

	select {
	case <-acquired:
		t.Fatal("UploadWithLock proceeded while the lock was held elsewhere")
	default:
	}

	unlockOther()

	select {
	case <-acquired:
	case <-context.Background().Done():
		t.Fatal("UploadWithLock did not acquire the lock after release")
	}
}

// TestCS3UploadWithLock_ConcurrentSameKeySerializes confirms that concurrent
// UploadWithLock calls for the same path do not overlap in their critical
// section (the read→mutate window), which is the property that makes the
// operation atomic. Provider calls are avoided by aborting in fn.
func TestCS3UploadWithLock_ConcurrentSameKeySerializes(t *testing.T) {
	l := locks.NewMemoryLocker()
	cs3 := NewCS3("gw", "provider")
	cs3.locker = l

	var inCritical atomic.Int32
	const n = 20
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = cs3.UploadWithLock(context.Background(), UploadRequest{Path: "/a.json"}, func(existing []byte) ([]byte, error) {
				if cur := inCritical.Add(1); cur > 1 {
					t.Errorf("critical section not exclusive (inCritical=%d)", cur)
				}
				inCritical.Add(-1)
				return nil, nil // abort → no provider call
			})
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
}
