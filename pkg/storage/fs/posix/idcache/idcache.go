// Copyright 2018-2021 CERN
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

package idcache

import (
	"context"
	"encoding/base32"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v3/pkg/appctx"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
)

type IDCache struct {
	kv jetstream.KeyValue
}

// NewStoreIDCache returns a new StoreIDCache
func NewStoreIDCache(kv jetstream.KeyValue) (*IDCache, error) {
	return &IDCache{
		kv: kv,
	}, nil
}

// Delete removes an entry from the cache
func (c *IDCache) Delete(ctx context.Context, spaceID, nodeID string) error {
	var rerr error
	v, err := retry(ctx, func() (jetstream.KeyValueEntry, error) {
		return c.kv.Get(ctx, cacheKey(spaceID, nodeID))
	})
	if err == nil {
		rerr = retryErr(ctx, func() error {
			return c.kv.Purge(ctx, reverseCacheKey(string(v.Value())))
		})
	}

	err = retryErr(ctx, func() error { return c.kv.Purge(ctx, cacheKey(spaceID, nodeID)) })
	if err != nil {
		return err
	}
	return rerr
}

// DeleteByPath removes an entry from the cache
func (c *IDCache) DeleteByPath(ctx context.Context, path string) error {
	baseKey := reverseCacheKey(path)

	spaceID, nodeID, err := c.GetByPath(ctx, path)
	if err != nil {
		if _, ok := err.(errtypes.NotFound); !ok {
			return err
		}
		appctx.GetLogger(ctx).Error().Err(err).Str("record", path).Msg("could not get spaceID and nodeID from cache")
	} else {
		err := retryErr(ctx, func() error {
			return c.kv.Purge(ctx, baseKey)
		})
		if err != nil && err != jetstream.ErrKeyNotFound {
			appctx.GetLogger(ctx).Error().Err(err).Str("record", baseKey).Str("spaceID", spaceID).Str("nodeID", nodeID).Msg("could not purge from cache")
		}

		err = retryErr(ctx, func() error {
			return c.kv.Purge(ctx, cacheKey(spaceID, nodeID))
		})
		if err != nil && err != jetstream.ErrKeyNotFound {
			appctx.GetLogger(ctx).Error().Err(err).Str("record", cacheKey(spaceID, nodeID)).Str("spaceID", spaceID).Str("nodeID", nodeID).Msg("could not purge from cache")
		}
	}

	watcher, err := retry(ctx, func() (jetstream.KeyWatcher, error) { return c.kv.Watch(ctx, baseKey+".>") })
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Stop() }()

	for update := range watcher.Updates() {
		if update == nil {
			break
		}
		key := update.Key()
		spaceID, nodeID, err := c.getByReverseCacheKey(ctx, key)
		if err != nil {
			appctx.GetLogger(ctx).Error().Err(err).Str("record", key).Msg("could not get spaceID and nodeID from cache")
			continue
		}

		err = retryErr(ctx, func() error {
			return c.kv.Purge(ctx, key)
		})
		if err != nil && err != jetstream.ErrKeyNotFound {
			appctx.GetLogger(ctx).Error().Err(err).Str("record", key).Str("spaceID", spaceID).Str("nodeID", nodeID).Msg("could not purge from cache")
		}

		err = retryErr(ctx, func() error {
			return c.kv.Purge(ctx, cacheKey(spaceID, nodeID))
		})
		if err != nil && err != jetstream.ErrKeyNotFound {
			appctx.GetLogger(ctx).Error().Err(err).Str("record", cacheKey(spaceID, nodeID)).Str("spaceID", spaceID).Str("nodeID", nodeID).Msg("could not purge from cache")
		}
	}
	return nil
}

// DeletePath removes only the path entry from the cache
func (c *IDCache) DeletePath(ctx context.Context, path string) error {
	return retryErr(ctx, func() error {
		return c.kv.Purge(ctx, reverseCacheKey(path))
	})
}

// Set adds a new entry to the cache
// ReservePath claims path for a node that is about to be created, before it exists on disk. It
// only writes the path entry if there is none, so it never overwrites the entry of another node.
// If another node holds the path, it reports that node and the revision of its entry instead,
// see TakeOverPath. An entry that already points at the node counts as reserved.
func (c *IDCache) ReservePath(ctx context.Context, spaceID, nodeID, path string) (heldBySpace, heldByNode string, revision uint64, err error) {
	key := cacheKey(spaceID, nodeID)
	rk := reverseCacheKey(path)
	for range 3 {
		_, err = c.kv.Create(ctx, rk, []byte(key))
		if err == nil {
			return "", "", 0, c.setForward(ctx, key, path)
		}
		if !isWrongRevision(err) {
			return "", "", 0, err
		}
		e, err := c.kv.Get(ctx, rk)
		if err == jetstream.ErrKeyNotFound {
			// removed in between, try again
			continue
		}
		if err != nil {
			return "", "", 0, err
		}
		if string(e.Value()) == key {
			return "", "", 0, c.setForward(ctx, key, path)
		}
		heldBySpace, heldByNode, err = decodeCacheKey(string(e.Value()))
		if err != nil {
			return "", "", 0, err
		}
		return heldBySpace, heldByNode, e.Revision(), nil
	}
	return "", "", 0, errtypes.Aborted("the path entry kept changing while reserving " + path)
}

// TakeOverPath points the path entry at the node if it is still at revision, see ReservePath.
// It reports false if the entry changed in between.
func (c *IDCache) TakeOverPath(ctx context.Context, spaceID, nodeID, path string, revision uint64) (bool, error) {
	key := cacheKey(spaceID, nodeID)
	_, err := c.kv.Update(ctx, reverseCacheKey(path), []byte(key), revision)
	switch {
	case err == nil:
		return true, c.setForward(ctx, key, path)
	case isWrongRevision(err):
		return false, nil
	default:
		return false, err
	}
}

// Release removes the entries of a node that was reserved for path but never created. The
// path entry is only removed while it still points at the node, so the entry of another node
// that got the path in the meantime stays.
func (c *IDCache) Release(ctx context.Context, spaceID, nodeID, path string) error {
	key := cacheKey(spaceID, nodeID)
	rk := reverseCacheKey(path)
	e, err := retry(ctx, func() (jetstream.KeyValueEntry, error) { return c.kv.Get(ctx, rk) })
	switch {
	case err == nil && string(e.Value()) == key:
		if err := c.kv.Purge(ctx, rk, jetstream.LastRevision(e.Revision())); err != nil && !isWrongRevision(err) {
			return err
		}
	case err != nil && err != jetstream.ErrKeyNotFound:
		return err
	}
	return retryErr(ctx, func() error { return c.kv.Purge(ctx, key) })
}

func (c *IDCache) setForward(ctx context.Context, key, path string) error {
	_, err := retry(ctx, func() (uint64, error) {
		return c.kv.Put(ctx, key, []byte(path))
	})
	return err
}

// isWrongRevision reports whether a conditional write failed because the key had another revision
func isWrongRevision(err error) bool {
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	var apiErr *jetstream.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
}

func (c *IDCache) Set(ctx context.Context, spaceID, nodeID, val string) error {
	_, err := retry(ctx, func() (uint64, error) {
		return c.kv.Put(ctx, cacheKey(spaceID, nodeID), []byte(val))
	})
	if err != nil {
		return err
	}

	_, err = retry(ctx, func() (uint64, error) {
		return c.kv.Put(ctx, reverseCacheKey(val), []byte(cacheKey(spaceID, nodeID)))
	})
	return err
}

// Get returns the value for a given key
func (c *IDCache) Get(ctx context.Context, spaceID, nodeID string) (string, error) {
	record, err := retry(ctx, func() (jetstream.KeyValueEntry, error) {
		return c.kv.Get(ctx, cacheKey(spaceID, nodeID))
	})
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return "", errtypes.NotFound("record not found in cache")
		}
		return "", err
	}
	return string(record.Value()), nil
}

func (c *IDCache) getByReverseCacheKey(ctx context.Context, reverseKey string) (string, string, error) {
	record, err := retry(ctx, func() (jetstream.KeyValueEntry, error) {
		return c.kv.Get(ctx, reverseKey)
	})
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return "", "", errtypes.NotFound("record not found in cache")
		}
		return "", "", err
	}
	return decodeCacheKey(string(record.Value()))
}

func decodeCacheKey(key string) (string, string, error) {
	decoded, err := base32.StdEncoding.DecodeString(key)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(decoded), "!", 2)
	if len(parts) != 2 {
		return "", "", errtypes.InternalError("invalid cache record")
	}
	return parts[0], parts[1], nil
}

// GetByPath returns the key for a given value
func (c *IDCache) GetByPath(ctx context.Context, path string) (string, string, error) {
	return c.getByReverseCacheKey(ctx, reverseCacheKey(path))
}

func cacheKey(spaceid, nodeID string) string {
	return base32.StdEncoding.EncodeToString([]byte(spaceid + "!" + nodeID))
}

func reverseCacheKey(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	encoded := make([]string, len(parts))
	for i, p := range parts {
		encoded[i] = base32.StdEncoding.EncodeToString([]byte(p))
	}

	return strings.Join(encoded, ".")
}

func retry[T any](ctx context.Context, f func() (T, error)) (T, error) {
	var v T
	var err error
	b := backoff.NewExponentialBackOff()
	for range 5 {
		v, err = f()
		if err == nil || err == jetstream.ErrKeyNotFound {
			return v, err
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			time.Sleep(b.NextBackOff())
		}

		appctx.GetLogger(ctx).Error().Err(err).Msg("error in jetstream kv operation, retrying")
	}
	return v, err
}

func retryErr(ctx context.Context, f func() error) error {
	var err error
	b := backoff.NewExponentialBackOff()
	for range 5 {
		err = f()
		if err == nil || err == jetstream.ErrKeyNotFound {
			return err
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			time.Sleep(b.NextBackOff())
		}

		appctx.GetLogger(ctx).Error().Err(err).Msg("error in jetstream kv operation, retrying")
	}
	return err
}
