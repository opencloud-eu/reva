// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
)

const (
	// DefaultNatsLockTTL is how long a lock lives if its holder never
	// releases it, e.g. because it crashed.
	DefaultNatsLockTTL = 30 * time.Second

	// natsLockPollInterval bounds how long a waiter sleeps between attempts
	// when it misses the watch event for a released lock.
	natsLockPollInterval = time.Second

	natsUnlockTimeout = 5 * time.Second
)

// NatsLocker is a Locker backed by a NATS JetStream key-value bucket. Taking
// the lock creates the key for the lock (Create only succeeds if the key does
// not exist), releasing it deletes the key, but only if it still holds our
// revision. Waiters watch the key and retry when it is deleted.
//
// The bucket is configured with a TTL (see NatsLockBucketConfig), so the lock
// of a holder that crashed expires on its own. A holder that stalls for longer
// than the TTL loses its lock while it still works. That is fine for the
// metadata storages: they upload conditionally on the etag they read, so the
// lock only avoids retries and is not needed for correctness. For the same
// reason Lock does not fail when NATS is unavailable: it logs the error and
// proceeds without a lock. While the connection is down, that happens after
// the JetStream request timeout (5s by default), so writes get slower but do
// not fail. The connection created by metadata.LockerOption keeps reconnecting
// forever.
type NatsLocker struct {
	kv    jetstream.KeyValue
	owner []byte
}

// NatsLockBucketConfig returns the key-value bucket config NatsLocker
// expects: in memory, one revision per key, and keys (including the markers
// left by deletes and expiries) removed after ttl.
func NatsLockBucketConfig(bucket string, ttl time.Duration) jetstream.KeyValueConfig {
	if ttl <= 0 {
		ttl = DefaultNatsLockTTL
	}
	return jetstream.KeyValueConfig{
		Bucket:         bucket,
		Description:    "locks for read-modify-write cycles on metadata files",
		History:        1,
		TTL:            ttl,
		LimitMarkerTTL: ttl,
		Storage:        jetstream.MemoryStorage,
	}
}

// NewNatsLocker returns a NatsLocker using kv, which should be created with
// NatsLockBucketConfig.
func NewNatsLocker(kv jetstream.KeyValue) *NatsLocker {
	host, _ := os.Hostname()
	return &NatsLocker{
		kv:    kv,
		owner: fmt.Appendf(nil, "%s/%d/%s", host, os.Getpid(), rand.Text()),
	}
}

// natsKey maps a storage path to a valid key. Keys may only contain
// [-/_=.a-zA-Z0-9] and '.' separates tokens, so we use unpadded base64url.
func natsKey(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

// Lock blocks until the lock for key is acquired or ctx is cancelled.
func (n *NatsLocker) Lock(ctx context.Context, key string) (func(), error) {
	k := natsKey(key)
	var w jetstream.KeyWatcher
	defer func() {
		if w != nil {
			_ = w.Stop()
		}
	}()

	for {
		rev, err := n.kv.Create(ctx, k, n.owner)
		switch {
		case err == nil:
			return func() { n.unlock(ctx, key, k, rev) }, nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case !errors.Is(err, jetstream.ErrKeyExists):
			return n.proceedUnlocked(ctx, key, err), nil
		}

		if w == nil {
			// Watch before waiting, then try again right away: the lock may
			// have been released before the watcher was set up.
			w, err = n.kv.Watch(ctx, k, jetstream.UpdatesOnly())
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return n.proceedUnlocked(ctx, key, err), nil
			}
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case _, ok := <-w.Updates():
			if !ok {
				w = nil // watcher closed, set up a new one
			}
		case <-time.After(natsLockPollInterval):
		}
	}
}

// unlock deletes the lock key, unless it no longer holds our revision because
// the lock expired and somebody else took it.
func (n *NatsLocker) unlock(ctx context.Context, key, k string, rev uint64) {
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), natsUnlockTimeout)
	defer cancel()
	if err := n.kv.Delete(uctx, k, jetstream.LastRevision(rev)); err != nil {
		appctx.GetLogger(ctx).Debug().Err(err).Str("key", key).Msg("could not release nats lock, it will expire")
	}
}

func (n *NatsLocker) proceedUnlocked(ctx context.Context, key string, err error) func() {
	appctx.GetLogger(ctx).Warn().Err(err).Str("key", key).Msg("could not take nats lock, proceeding without it")
	return func() {}
}
