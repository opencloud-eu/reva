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

// Lock opens the sidecar lock file for key and takes an exclusive, blocking
// lock on it. The returned function closes the file, releasing the lock
// exactly once.
func (d *DiskLocker) Lock(_ context.Context, key string) (func(), error) {
	lockPath := filepath.Join(d.base, filepath.Join("/", key)+LockFileSuffix)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, err
	}
	f, err := lockedfile.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
