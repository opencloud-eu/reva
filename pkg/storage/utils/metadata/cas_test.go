// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
)

// fakeCASStorage is an in-memory casStorage that honours If-Match and
// If-None-Match "*" atomically, like the CS3 storage provider does. Its etag is
// a version counter. afterDownload, if set, runs after every Download and can
// simulate a concurrent writer.
type fakeCASStorage struct {
	mu            sync.Mutex
	content       []byte
	exists        bool
	version       int
	downloads     int
	afterDownload func(f *fakeCASStorage)
}

func (f *fakeCASStorage) Download(_ context.Context, req DownloadRequest) (*DownloadResponse, error) {
	f.mu.Lock()
	f.downloads++
	exists, content, etag := f.exists, f.content, strconv.Itoa(f.version)
	hook := f.afterDownload
	f.mu.Unlock()
	if hook != nil {
		hook(f)
	}
	if !exists {
		return nil, errtypes.NotFound(req.Path)
	}
	return &DownloadResponse{Content: content, Etag: etag}, nil
}

func (f *fakeCASStorage) Upload(_ context.Context, req UploadRequest) (*UploadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploadLocked(req)
}

func (f *fakeCASStorage) uploadLocked(req UploadRequest) (*UploadResponse, error) {
	for _, tag := range req.IfNoneMatch {
		if tag == "*" && f.exists {
			return nil, errtypes.AlreadyExists(req.Path)
		}
	}
	if req.IfMatchEtag != "" && (!f.exists || req.IfMatchEtag != strconv.Itoa(f.version)) {
		return nil, errtypes.PreconditionFailed("etag mismatch")
	}
	f.content = req.Content
	f.exists = true
	f.version++
	return &UploadResponse{Etag: strconv.Itoa(f.version)}, nil
}

func increment(existing []byte) ([]byte, error) {
	n := 0
	if len(existing) > 0 {
		var err error
		if n, err = strconv.Atoi(string(existing)); err != nil {
			return nil, err
		}
	}
	return []byte(strconv.Itoa(n + 1)), nil
}

// TestCASUpload_ConcurrentCounterWithoutLock shows that the etag-conditional
// upload alone prevents lost updates, i.e. no lock is needed for correctness.
func TestCASUpload_ConcurrentCounterWithoutLock(t *testing.T) {
	s := &fakeCASStorage{}
	const n = 30
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if _, err := casUpload(context.Background(), s, UploadRequest{Path: "/counter"}, increment); err != nil {
				t.Errorf("casUpload: %v", err)
			}
		})
	}
	wg.Wait()

	if got := string(s.content); got != strconv.Itoa(n) {
		t.Errorf("expected counter %d, got %s (lost updates)", n, got)
	}
}

// TestCASUpload_RetriesWhenChangedConcurrently injects a write between the
// download and the upload and checks that fn is re-run on the fresh content.
func TestCASUpload_RetriesWhenChangedConcurrently(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exists bool
	}{
		{"existing file", true},
		{"new file", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeCASStorage{exists: tc.exists, content: []byte("0")}
			injected := false
			s.afterDownload = func(f *fakeCASStorage) {
				if injected {
					return
				}
				injected = true
				f.mu.Lock()
				defer f.mu.Unlock()
				_, _ = f.uploadLocked(UploadRequest{Content: []byte("10")})
			}

			calls := 0
			res, err := casUpload(context.Background(), s, UploadRequest{Path: "/counter"}, func(existing []byte) ([]byte, error) {
				calls++
				return increment(existing)
			})
			if err != nil {
				t.Fatalf("casUpload: %v", err)
			}
			if calls != 2 {
				t.Errorf("expected fn to run twice, ran %d times", calls)
			}
			if got := string(s.content); got != "11" {
				t.Errorf("expected the concurrent write to be kept (11), got %s", got)
			}
			if res.Etag != strconv.Itoa(s.version) {
				t.Errorf("expected etag %d, got %s", s.version, res.Etag)
			}
		})
	}
}

func TestCASUpload_CreateOnlyRejectsExisting(t *testing.T) {
	s := &fakeCASStorage{exists: true, content: []byte("1")}
	_, err := casUpload(context.Background(), s, UploadRequest{Path: "/f", IfNoneMatch: []string{"*"}}, func([]byte) ([]byte, error) {
		t.Error("fn must not be called for an existing file")
		return nil, nil
	})
	var ae errtypes.AlreadyExists
	if !errors.As(err, &ae) {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
	if s.downloads != 1 {
		t.Errorf("expected no retry, got %d downloads", s.downloads)
	}
}

func TestCASUpload_NilSkipsWrite(t *testing.T) {
	s := &fakeCASStorage{exists: true, content: []byte("1"), version: 7}
	res, err := casUpload(context.Background(), s, UploadRequest{Path: "/f"}, func([]byte) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatalf("casUpload: %v", err)
	}
	if s.version != 7 || res.Etag != "7" {
		t.Errorf("expected no write and etag 7, got version %d etag %s", s.version, res.Etag)
	}
}

func TestCASUpload_GivesUpAfterMaxAttempts(t *testing.T) {
	defer func(b func(int) time.Duration) { casBackoff = b }(casBackoff)
	casBackoff = func(int) time.Duration { return 0 }
	s := &fakeCASStorage{exists: true, content: []byte("0")}
	s.afterDownload = func(f *fakeCASStorage) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.version++ // somebody else always wins
	}
	_, err := casUpload(context.Background(), s, UploadRequest{Path: "/f"}, increment)
	var pf errtypes.PreconditionFailed
	if !errors.As(err, &pf) {
		t.Fatalf("expected a wrapped PreconditionFailed, got %v", err)
	}
	if s.downloads != casMaxAttempts {
		t.Errorf("expected %d attempts, got %d", casMaxAttempts, s.downloads)
	}
}

func TestCASUpload_BacksOffAndStopsOnCancel(t *testing.T) {
	for n := 1; n <= casMaxAttempts; n++ {
		if d := casBackoff(n); d < casMinBackoff/2 || d >= casMaxBackoff*3/2 {
			t.Fatalf("backoff %d = %s out of range", n, d)
		}
	}

	s := &fakeCASStorage{exists: true, content: []byte("0")}
	s.afterDownload = func(f *fakeCASStorage) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.version++ // somebody else always wins
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := casUpload(ctx, s, UploadRequest{Path: "/f"}, increment)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if s.downloads >= casMaxAttempts {
		t.Errorf("expected the backoff to stop the retries early, got %d downloads", s.downloads)
	}
}

func TestStorages_DefaultToNoopLocker(t *testing.T) {
	cs3, err := NewCS3Storage("gw", "provider", "id", "idp", "key")
	if err != nil {
		t.Fatalf("NewCS3Storage: %v", err)
	}
	if _, ok := cs3.locker.(locks.NoopLocker); !ok {
		t.Errorf("cs3: expected a NoopLocker by default, got %T", cs3.locker)
	}
	if _, ok := NewCS3("gw", "provider").locker.(locks.NoopLocker); !ok {
		t.Errorf("NewCS3: expected a NoopLocker by default")
	}

	disk, err := NewDiskStorage(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewDiskStorage: %v", err)
	}
	if _, ok := disk.locker.(locks.NoopLocker); !ok {
		t.Errorf("disk: expected a NoopLocker by default, got %T", disk.locker)
	}
}

func TestNewDiskStorage_HonoursExplicitLocker(t *testing.T) {
	l := locks.NewDiskLocker(t.TempDir())
	s, err := NewDiskStorage(t.TempDir(), WithLocker(l))
	if err != nil {
		t.Fatalf("NewDiskStorage: %v", err)
	}
	if s.locker != l {
		t.Errorf("expected the supplied disk locker, got %T", s.locker)
	}
}

func TestLockerOption(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		backend, lockDir string
		wantErr          bool
		want             any // expected locker type after applyOptions
	}{
		{"", "", false, locks.NoopLocker{}},
		{"none", "", false, locks.NoopLocker{}},
		{"memory", "", true, nil},
		{"disk", dir, false, &locks.DiskLocker{}},
		{"disk", "", true, nil},
		{"nats", "", true, nil}, // no lock_nats.nodes
		{"bogus", "", true, nil},
	} {
		t.Run(tc.backend+"/"+tc.lockDir, func(t *testing.T) {
			opt, err := LockerOption(LockConfig{LockBackend: tc.backend, LockDir: tc.lockDir})
			if (err != nil) != tc.wantErr {
				t.Fatalf("LockerOption(%q, %q) error = %v, wantErr %v", tc.backend, tc.lockDir, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if (tc.backend == "" || tc.backend == "none") && opt != nil {
				t.Errorf("expected a nil option for %q", tc.backend)
			}
			got := applyOptions([]Option{opt}).locker
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tc.want) {
				t.Errorf("expected %T, got %T", tc.want, got)
			}
		})
	}
}

// TestCS3UploadWithLock_DiskLocker checks that a cs3 storage configured with
// lock_backend "disk" takes its locks on files in lock_dir.
func TestCS3UploadWithLock_DiskLocker(t *testing.T) {
	dir := t.TempDir()
	opt, err := LockerOption(LockConfig{LockBackend: "disk", LockDir: dir})
	if err != nil {
		t.Fatalf("LockerOption: %v", err)
	}
	s, err := NewCS3Storage("gw", "provider", "id", "idp", "key", opt)
	if err != nil {
		t.Fatalf("NewCS3Storage: %v", err)
	}
	// fail the download so no provider is needed; the lock is taken before
	_, _ = s.UploadWithLock(context.Background(), UploadRequest{Path: "users/u1/created.json"}, func([]byte) ([]byte, error) { return nil, nil })

	if _, err := os.Stat(filepath.Join(dir, "users", "u1", "created.json"+locks.LockFileSuffix)); err != nil {
		t.Errorf("expected a lock file in lock_dir: %v", err)
	}
}
