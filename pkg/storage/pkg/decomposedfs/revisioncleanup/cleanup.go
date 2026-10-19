// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

// Package revisioncleanup deletes revisions of a node, independently of the storage
// driver that holds them.
//
// # How the storage drivers plug in
//
// All decomposedfs based drivers (decomposed, decomposeds3, posix) share the
// Decomposedfs struct, which implements the CS3 API, the upload sessions and
// RestoreRevision. What differs per driver is the on-disk layout, which each
// driver supplies as a node.Tree through aspects.Aspects:
//
//   - decomposed and decomposeds3 use decomposedfs/tree.Tree. A revision is an
//     empty node file next to the node, '<node path>.REV.<timestamp>', whose
//     metadata references the content as a blob in the blobstore (BlobIDAttr).
//     The two drivers differ only in the blobstore (local disk or S3).
//   - posix uses fs/posix/tree.Tree. A revision is a full copy of the content in
//     the '.oc-nodes' directory of the space (lookup.VersionPath), the posix
//     blobstore does not hold any separate blobs.
//
// In both cases the revision key is '<nodeid>.REV.<RFC3339Nano mtime>', with an
// optional '.<n>' suffix when CreateRevision had to rename a colliding revision.
//
// See pkg/storage/pkg/decomposedfs/README.md for a more detailed overview of the
// driver structure.
//
// The shared code only knows node.Tree. Listing revisions is driver specific
// (node.Tree.ListRevisionsUnchecked), but deleting one is not: Node.DeleteRevisionUnchecked handles
// both layouts, and node.Tree.DeleteBlob deletes the blob in whichever blobstore the
// driver uses. PurgeRevisionUnchecked combines the two, once for all drivers.
// Neither ListRevisionsUnchecked nor PurgeRevisionUnchecked check permissions: this is system
// housekeeping, which runs e.g. in async postprocessing where there is no suitable
// user in the context.

package revisioncleanup

import (
	"context"
	"strings"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

// BlobDeleter is the part of node.Tree needed to purge a revision.
type BlobDeleter interface {
	DeleteBlob(n *node.Node) error
}

// PurgeRevisionUnchecked physically removes the revision of n with the given key, including its
// blob, metadata and lockfile, without checking permissions. Callers that act on behalf
// of a user must check permissions themselves.
//
// It works for all decomposedfs based drivers:
//   - decomposed, decomposeds3: a revision is the empty node file '<node path>.REV.<timestamp>'
//     with its metadata and lockfile, plus the blob holding the content, referenced by
//     BlobIDAttr in the revision's metadata.
//   - posix: a revision is a full copy of the content in the '.oc-nodes' directory of the
//     space, with its metadata and lockfile. Revisions created from changes on disk by
//     assimilation have the same layout. The posix blobstore does not hold separate blobs,
//     its Delete is a no-op.
//
// Node.DeleteRevisionUnchecked removes everything but the blob, under the node's metadata lock
// (which is reentrant, callers may hold it), and returns the blob id, so that the blob can be
// deleted here. It is a no-op for a revision that does not exist (anymore), e.g. when it was purged
// concurrently.
//
// The key is validated first (node.ValidateRevisionKey), as it is turned into the version that
// Node.DeleteRevisionUnchecked expects, and an empty version would make
// Node.DeleteRevisionUnchecked revert or even purge the node itself instead of deleting a revision.
//
// The blob of a revision is never shared with the current version or other revisions:
// every upload gets a new blob id, CreateRevision moves the reference of the previous
// blob to the revision, and RestoreRevision moves it back while removing the revision
// without deleting its blob. So deleting it here cannot affect anything else.
//
// Decomposedfs.deleteRevisionFile, used by the DeleteFileVersions API and the DeleteRevision
// event, does the same without the key validation. It is left as it is to keep that code
// identical to upstream.
func PurgeRevisionUnchecked(ctx context.Context, bd BlobDeleter, n *node.Node, revisionKey string) error {
	if err := node.ValidateRevisionKey(n, revisionKey); err != nil {
		return err
	}
	blobID, err := n.DeleteRevisionUnchecked(ctx, strings.TrimPrefix(revisionKey, n.ID+node.RevisionIDDelimiter))
	if err != nil {
		return err
	}
	if blobID == "" {
		// zero byte revision, or no such revision: there is no blob
		return nil
	}
	// DeleteBlob only needs the blob coordinates, not a full node
	return bd.DeleteBlob(&node.Node{BaseNode: node.BaseNode{SpaceID: n.SpaceID}, BlobID: blobID})
}
