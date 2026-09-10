// Copyright 2018-2022 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package metadata

import (
	"context"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
)

// UploadRequest represents an upload request and its options
type UploadRequest struct {
	Path    string
	Content []byte

	IfMatchEtag       string
	IfNoneMatch       []string
	IfUnmodifiedSince time.Time
	MTime             time.Time
}

// UploadResponse represents a upload response
type UploadResponse struct {
	Etag   string
	FileID string // only for cs3 storage
}

// DownloadRequest represents a download request and its options
type DownloadRequest struct {
	Path        string
	IfNoneMatch []string
}

// DownloadResponse represents a download response and its options
type DownloadResponse struct {
	Content []byte

	Etag  string
	Mtime time.Time
}

// Storage is the interface to maintain metadata in a storage
type Storage interface {
	Backend() string

	Init(ctx context.Context, name string) (err error)
	Upload(ctx context.Context, req UploadRequest) (*UploadResponse, error)
	Download(ctx context.Context, req DownloadRequest) (*DownloadResponse, error)
	SimpleUpload(ctx context.Context, uploadpath string, content []byte) error
	SimpleDownload(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
	Stat(ctx context.Context, path string) (*provider.ResourceInfo, error)

	ReadDir(ctx context.Context, path string) ([]string, error)
	ListDir(ctx context.Context, path string) ([]*provider.ResourceInfo, error)

	CreateSymlink(ctx context.Context, oldname, newname string) error
	ResolveSymlink(ctx context.Context, name string) (string, error)

	MakeDirIfNotExist(ctx context.Context, name string) error
}

// LockingStorage is a Storage that can perform an atomic read-modify-write on
// a path. The write is serialized by an exclusive lock held across the entire
// read→mutate→write cycle, which makes it safe for concurrent writers without
// relying on etag-based compare-and-swap retries.
type LockingStorage interface {
	Storage

	// UploadWithLock atomically reads the current content of req.Path, passes
	// it to fn (nil if the file does not exist), and persists the bytes fn
	// returns. If fn returns nil the write is skipped. The exclusive lock for
	// req.Path is held for the whole operation, so no other writer can
	// interleave between the read and the write.
	UploadWithLock(ctx context.Context, req UploadRequest, fn func(existing []byte) ([]byte, error)) (*UploadResponse, error)
}

// config holds the resolved options shared by the storage backends.
type config struct {
	locker locks.Locker
}

// Option configures a storage backend.
type Option func(*config)

// WithLocker overrides the lock strategy used for atomic read-modify-write
// operations. When not supplied the disk-based locker is used, which is safe
// both for single-replica deployments (a local flock) and multi-replica
// deployments sharing an RWX volume.
func WithLocker(l locks.Locker) Option {
	return func(c *config) {
		if l != nil {
			c.locker = l
		}
	}
}

// applyOptions resolves the given options into a config. The locker is left
// unset here; each backend applies its own appropriate default (the disk
// backend roots a disk locker at its data dir, while the cs3 backend uses a
// base-agnostic locker). Callers may override it with WithLocker.
func applyOptions(opts []Option) *config {
	c := &config{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
