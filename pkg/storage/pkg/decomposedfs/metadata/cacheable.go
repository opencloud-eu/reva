// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"strings"

	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
)

// isCacheable reports whether attribs, as read from storage, can be cached for n.
//
// A read can come back incomplete without an error, e.g. on NFS right after the
// attributes were changed, or while a node is still being created. Caching such a
// read would hide the attributes that are on disk until the cache entry expires, so
// we only cache reads that look like a complete node: every node has an id, and
// every node but a space root has a parent id. An empty read is never cached, a read
// that carries no attributes of ours at all is not a node we wrote and is cached as
// before.
func isCacheable(n MetadataNode, attribs map[string][]byte) bool {
	if len(attribs) == 0 {
		return false
	}

	ours := false
	for k := range attribs {
		if strings.HasPrefix(k, prefixes.OcPrefix) {
			ours = true
			break
		}
	}
	if !ours {
		return true
	}

	id := string(attribs[prefixes.IDAttr])
	if id == "" {
		return false
	}
	if id == n.GetSpaceID() {
		// space roots have no parent
		return true
	}
	return len(attribs[prefixes.ParentidAttr]) > 0
}
