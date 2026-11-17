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
	"errors"
	"fmt"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
	"github.com/rs/zerolog/log"
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
// a path. The upload is conditional on the etag that was read and retried when
// another writer got in between, so concurrent writers never lose updates. The
// optional lock (see WithLocker) serialises the writers that share it, which
// avoids most of those retries.
type LockingStorage interface {
	Storage

	// UploadWithLock reads the current content of req.Path, passes it to fn
	// (nil if the file does not exist) and persists the bytes fn returns,
	// unless the file changed in the meantime, in which case it starts over.
	// fn may therefore run more than once and must only depend on its
	// argument. If fn returns nil the write is skipped.
	UploadWithLock(ctx context.Context, req UploadRequest, fn func(existing []byte) ([]byte, error)) (*UploadResponse, error)
}

// config holds the resolved options shared by the storage backends.
type config struct {
	locker locks.Locker
}

// Option configures a storage backend.
type Option func(*config)

// WithLocker sets the lock strategy used by UploadWithLock. Without it the
// storage does not lock: UploadWithLock then relies on etag-conditional
// uploads and retries alone.
func WithLocker(l locks.Locker) Option {
	return func(c *config) {
		if l != nil {
			c.locker = l
		}
	}
}

// applyOptions resolves the given options into a config. nil options are
// skipped and the locker defaults to a NoopLocker.
func applyOptions(opts []Option) *config {
	c := &config{}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.locker == nil {
		c.locker = locks.NewNoopLocker()
	}
	return c
}

// LockConfig holds the lock configuration of the metadata based managers.
// Embed it with `mapstructure:",squash"`.
type LockConfig struct {
	// LockBackend selects how writers are serialised: "none" (default) relies
	// on etag-conditional uploads and retries alone, "disk" uses flock on
	// files in LockDir and "nats" a NATS JetStream key-value bucket. "disk"
	// and "nats" serialise writers across replicas (for "disk", LockDir must
	// be on a volume all replicas share) and avoid most retries.
	LockBackend string `mapstructure:"lock_backend"`
	// LockDir is the directory for the lock files of lock_backend "disk".
	LockDir string `mapstructure:"lock_dir"`
	// LockNats configures lock_backend "nats".
	LockNats LockNatsConfig `mapstructure:"lock_nats"`
}

// LockNatsConfig configures the NATS connection and bucket of lock_backend
// "nats".
type LockNatsConfig struct {
	Nodes []string `mapstructure:"nodes"`
	// Bucket is the key-value bucket for the locks, defaults to
	// "metadata-locks".
	Bucket string `mapstructure:"bucket"`
	// TTL is how many seconds a lock lives if its holder does not release
	// it, defaults to 30.
	TTL                  int    `mapstructure:"ttl"`
	AuthUsername         string `mapstructure:"auth_username"`
	AuthPassword         string `mapstructure:"auth_password"`
	TLSEnabled           bool   `mapstructure:"tls_enabled"`
	TLSInsecure          bool   `mapstructure:"tls_insecure"`
	TLSRootCACertificate string `mapstructure:"tls_root_ca_certificate"`
}

const defaultNatsLockBucket = "metadata-locks"

// LockerOption translates the lock configuration into a storage option. ""
// and "none" return nil (no locking).
func LockerOption(c LockConfig) (Option, error) {
	switch c.LockBackend {
	case "", "none":
		return nil, nil
	case "disk":
		if c.LockDir == "" {
			return nil, fmt.Errorf("lock_backend %q requires lock_dir", c.LockBackend)
		}
		return WithLocker(locks.NewDiskLocker(c.LockDir)), nil
	case "nats":
		l, err := newNatsLocker(c.LockNats)
		if err != nil {
			return nil, err
		}
		return WithLocker(l), nil
	default:
		return nil, fmt.Errorf("unknown lock_backend %q (want \"none\", \"disk\" or \"nats\")", c.LockBackend)
	}
}

func newNatsLocker(c LockNatsConfig) (*locks.NatsLocker, error) {
	if len(c.Nodes) == 0 {
		return nil, errors.New("lock_backend \"nats\" requires lock_nats.nodes")
	}
	if c.Bucket == "" {
		c.Bucket = defaultNatsLockBucket
	}
	logger := log.Logger
	js, err := cache.NewNatsJetStream(cache.Config{
		Nodes:                c.Nodes,
		Database:             c.Bucket,
		AuthUsername:         c.AuthUsername,
		AuthPassword:         c.AuthPassword,
		TLSEnabled:           c.TLSEnabled,
		TLSInsecure:          c.TLSInsecure,
		TLSRootCACertificate: c.TLSRootCACertificate,
	}, &logger,
		// The lock is optional, so a NATS outage must not take the service
		// down: never give up reconnecting, which also means the connection
		// never closes for good and the SIGTERM ClosedHandler never fires.
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, err
	}
	return NewNatsLockerFromJetStream(js, c.Bucket, time.Duration(c.TTL)*time.Second)
}

// NewNatsLockerFromJetStream creates or updates the lock bucket and returns a
// NatsLocker on it.
func NewNatsLockerFromJetStream(js jetstream.JetStream, bucket string, ttl time.Duration) (*locks.NatsLocker, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kv, err := js.CreateOrUpdateKeyValue(ctx, locks.NatsLockBucketConfig(bucket, ttl))
	if err != nil {
		return nil, fmt.Errorf("could not set up nats lock bucket %q: %w", bucket, err)
	}
	return locks.NewNatsLocker(kv), nil
}

var (
	_ LockingStorage = (*CS3)(nil)
	_ LockingStorage = (*Disk)(nil)
)
