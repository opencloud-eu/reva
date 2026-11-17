// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rogpeppe/go-internal/lockedfile"
)

// LockFileSuffix is appended to the lock key to form the sidecar lock file
// path. It mirrors the convention used by the decomposedfs storage driver so
// that lock files are written next to the data file they protect.
const LockFileSuffix = ".mlock"

// DiskLocker is a Locker backed by advisory file locks (flock on Linux). Each
// key maps to a sidecar file <base>/<key>.mlock; taking the lock opens that
// file for writing, which blocks until any other holder closes it. This
// serializes writers across processes sharing the same filesystem (e.g. an RWX
// volume in Kubernetes), making read-modify-write cycles atomic cluster-wide.
type DiskLocker struct {
	base string
}

// NewDiskLocker returns a ready-to-use DiskLocker rooted at base. Sidecar lock
// files are created under base, next to the data files they protect.
func NewDiskLocker(base string) *DiskLocker {
	return &DiskLocker{base: base}
}

// Lock opens the sidecar lock file for key and takes an exclusive lock on it,
// blocking until it is acquired or ctx is cancelled. The returned function
// closes the file, releasing the lock.
func (d *DiskLocker) Lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(d.base, filepath.Join("/", key)+LockFileSuffix)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, err
	}

	// flock cannot be interrupted, so wait for it in a goroutine. If ctx is
	// cancelled first, that goroutine releases the lock as soon as it gets it.
	type result struct {
		f   *lockedfile.File
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := lockedfile.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
		done <- result{f, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		return func() { _ = r.f.Close() }, nil
	case <-ctx.Done():
		go func() {
			if r := <-done; r.err == nil {
				_ = r.f.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
