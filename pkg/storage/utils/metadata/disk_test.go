// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
)

func newTestDisk(t *testing.T, l locks.Locker) *Disk {
	t.Helper()
	d, err := NewDiskStorage(t.TempDir(), WithLocker(l))
	if err != nil {
		t.Fatalf("NewDiskStorage: %v", err)
	}
	return d.(*Disk)
}

func TestDiskUploadWithLock_BasicCreateAndRead(t *testing.T) {
	d := newTestDisk(t, locks.NewMemoryLocker())
	ctx := context.Background()

	res, err := d.UploadWithLock(ctx, UploadRequest{Path: "/a.json"}, func(existing []byte) ([]byte, error) {
		if existing != nil {
			t.Fatalf("expected no existing content, got %q", existing)
		}
		return []byte(`{"n":1}`), nil
	})
	if err != nil {
		t.Fatalf("UploadWithLock: %v", err)
	}
	if res.Etag == "" {
		t.Error("expected a non-empty etag")
	}

	got, err := d.SimpleDownload(ctx, "/a.json")
	if err != nil {
		t.Fatalf("SimpleDownload: %v", err)
	}
	if string(got) != `{"n":1}` {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestDiskUploadWithLock_CreateOnlyRejectsExisting(t *testing.T) {
	d := newTestDisk(t, locks.NewMemoryLocker())
	ctx := context.Background()

	if _, err := d.UploadWithLock(ctx, UploadRequest{Path: "/a.json"}, func([]byte) ([]byte, error) {
		return []byte("x"), nil
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// A second create-only attempt must fail with AlreadyExists.
	_, err := d.UploadWithLock(ctx, UploadRequest{Path: "/a.json", IfNoneMatch: []string{"*"}}, func(existing []byte) ([]byte, error) {
		return []byte("y"), nil
	})
	if _, ok := err.(errtypes.AlreadyExists); !ok {
		t.Errorf("expected AlreadyExists, got %v", err)
	}
}

func TestDiskUploadWithLock_AbortOnNil(t *testing.T) {
	d := newTestDisk(t, locks.NewMemoryLocker())
	ctx := context.Background()

	if _, err := d.UploadWithLock(ctx, UploadRequest{Path: "/a.json"}, func([]byte) ([]byte, error) {
		return []byte("x"), nil
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// fn returns nil → write skipped, content unchanged.
	res, err := d.UploadWithLock(ctx, UploadRequest{Path: "/a.json"}, func(existing []byte) ([]byte, error) {
		if string(existing) != "x" {
			t.Fatalf("expected existing %q, got %q", "x", existing)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if res.Etag == "" {
		t.Error("expected the current etag to be returned on abort")
	}

	got, _ := d.SimpleDownload(ctx, "/a.json")
	if string(got) != "x" {
		t.Errorf("content changed despite abort: %q", got)
	}
}

// TestDiskUploadWithLock_ConcurrentCounter verifies that no updates are lost
// when many goroutines perform a read-modify-write under the lock. This is the
// core property that pessimistic locking provides over etag-based retries.
func TestDiskUploadWithLock_ConcurrentCounter(t *testing.T) {
	d := newTestDisk(t, locks.NewMemoryLocker())
	ctx := context.Background()

	const n = 40
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := d.UploadWithLock(ctx, UploadRequest{Path: "/counter.json"}, func(existing []byte) ([]byte, error) {
				var c struct{ N int }
				if len(existing) > 0 {
					if err := json.Unmarshal(existing, &c); err != nil {
						return nil, err
					}
				}
				c.N++
				return json.Marshal(c)
			}); err != nil {
				t.Errorf("UploadWithLock: %v", err)
				return
			}
		}()
	}
	wg.Wait()

	got, err := d.SimpleDownload(ctx, "/counter.json")
	if err != nil {
		t.Fatalf("SimpleDownload: %v", err)
	}
	var c struct{ N int }
	if err := json.Unmarshal(got, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.N != n {
		t.Errorf("expected %d, got %d (lost updates)", n, c.N)
	}
}

// TestDiskUploadWithLock_DiskLockerConcurrentCounter is the same lost-update
// test but using the disk-based locker, which serializes across processes.
func TestDiskUploadWithLock_DiskLockerConcurrentCounter(t *testing.T) {
	dir := t.TempDir()
	d := newTestDisk(t, locks.NewDiskLocker(dir))
	ctx := context.Background()

	const n = 30
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := d.UploadWithLock(ctx, UploadRequest{Path: "/counter.json"}, func(existing []byte) ([]byte, error) {
				var c struct{ N int }
				if len(existing) > 0 {
					if err := json.Unmarshal(existing, &c); err != nil {
						return nil, err
					}
				}
				c.N++
				return json.Marshal(c)
			}); err != nil {
				t.Errorf("UploadWithLock: %v", err)
				return
			}
		}()
	}
	wg.Wait()

	got, err := d.SimpleDownload(ctx, "/counter.json")
	if err != nil {
		t.Fatalf("SimpleDownload: %v", err)
	}
	var c struct{ N int }
	if err := json.Unmarshal(got, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.N != n {
		t.Errorf("expected %d, got %d (lost updates)", n, c.N)
	}
}
