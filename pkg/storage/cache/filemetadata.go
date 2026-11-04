// Copyright 2018-2023 CERN
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

package cache

import "github.com/opencloud-eu/reva/v2/pkg/store"

type fileMetadataCache struct {
	cacheStore
	shared bool
}

// NewFileMetadataCache creates a new FileMetadataCache
func NewFileMetadataCache(cfg Config) FileMetadataCache {
	c := &fileMetadataCache{shared: isSharedMetadataStore(cfg.Store)}
	c.s = getStore(cfg)
	c.database = cfg.Database
	c.table = cfg.Table
	c.ttl = cfg.TTL

	return c
}

func isSharedMetadataStore(storeType string) bool {
	switch storeType {
	case store.TypeEtcd, store.TypeRedis, store.TypeRedisSentinel, store.TypeNatsJS, store.TypeNatsJSKV:
		// all processes share these stores, so their entries do not go stale
		return true
	default:
		return false
	}
}

// RemoveMetadata removes a reference from the metadata cache
func (c *fileMetadataCache) RemoveMetadata(path string) error {
	return c.Delete(path)
}

// Invalidate drops an entry that another process can have made stale. It does nothing
// for shared stores, because they see the writes of the other processes.
func (c *fileMetadataCache) Invalidate(path string) error {
	if c.shared {
		return nil
	}
	return c.Delete(path)
}
