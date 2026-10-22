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
// granted to it by virtue of its status as an Intergovernmental Organization.

package decomposedfs

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/pkg/errors"

	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

// MarkProcessing toggles the processing marker on a resource for a single upload session.
func (fs *Decomposedfs) MarkProcessing(ctx context.Context, ref *provider.Reference, processing bool, sessionID string) error {
	resolved, err := fs.lu.NodeFromResource(ctx, ref)
	if err != nil {
		return err
	}
	if !resolved.Exists {
		return errtypes.NotFound(ref.String())
	}

	n, unlock, err := node.LockAndReadNode(ctx, fs.lu, resolved.SpaceID, resolved.ID, "", false, resolved.SpaceRoot, false)
	if err != nil {
		return err
	}
	defer func() {
		if err := unlock(); err != nil {
			appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not close mark-processing lock")
		}
	}()

	if !processing {
		if !n.IsProcessing(ctx) {
			return nil
		}
		id, _ := n.ProcessingID(ctx)
		if id != sessionID {
			return nil
		}
		return n.RemoveXattr(ctx, prefixes.StatusPrefix)
	}

	if n.IsProcessing(ctx) {
		return errtypes.ResourceProcessing(ref.String())
	}
	return n.SetXattrsWithContext(ctx, node.Attributes{
		prefixes.StatusPrefix: []byte(node.ProcessingStatus + sessionID),
	})
}

// CommitUpload writes the staged bytes from source to the resource at ref.
// sessionID identifies the upload that PrepareUpload wrote onto the node.
// Caller owns source.Body and must close it after CommitUpload returns.
//
// The bytes are written against the real node, so drivers that keep the content
// in the node itself (posix) can replace it atomically. If a newer upload took
// over the node in the meantime, the bytes go to the revision that upload created
// for this session instead. Without such a revision (versioning disabled) they are
// discarded: nothing references them anymore.
func (fs *Decomposedfs) CommitUpload(ctx context.Context, ref *provider.Reference, sessionID string, source storage.UploadSource) error {
	if source.Body == nil && source.Path == "" {
		return errtypes.BadRequest("Decomposedfs: source has neither body nor path")
	}
	if sessionID == "" {
		return errtypes.BadRequest("Decomposedfs: sessionID is empty")
	}

	n, err := fs.lu.NodeFromResource(ctx, ref)
	if err != nil {
		appctx.GetLogger(ctx).Error().Err(err).Msg("CommitUpload: unexpected NodeFromResource failure")
		return errtypes.InternalError("CommitUpload: node lookup failed unexpectedly")
	}
	if !n.Exists {
		return errtypes.NotFound(ref.String())
	}
	n.SpaceRoot, err = node.ReadNode(ctx, fs.lu, n.SpaceID, n.SpaceID, "", false, nil, false)
	if err != nil {
		return err
	}

	unlock, err := fs.lu.MetadataBackend().Lock(n)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: could not lock node")
	}
	defer func() {
		_ = unlock()
	}()

	// re-read the blob id while holding the lock, a newer upload may have taken over.
	// read the attribute itself: the posix lookup's ReadBlobIDAndSizeAttr reports no blob id
	blobID, err := fs.lu.MetadataBackend().Get(ctx, n, prefixes.BlobIDAttr)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: could not read blob id")
	}
	n.BlobID = string(blobID)

	target := n
	if n.BlobID != sessionID {
		revision, revisionUnlock, err := fs.uploadRevision(ctx, n, sessionID)
		if err != nil {
			return err
		}
		if revision == nil {
			appctx.GetLogger(ctx).Info().Str("nodeid", n.ID).Str("sessionid", sessionID).Msg("upload was superseded and has no revision, discarding its bytes")
			return nil
		}
		defer func() {
			_ = revisionUnlock()
		}()
		target = revision
	}

	if source.Path != "" {
		err = fs.tp.WriteBlob(target, source.Path)
	} else {
		err = fs.tp.WriteBlobFromReader(target, source.Body, source.Length)
	}
	if err != nil {
		if derr := fs.tp.DeleteBlob(target); derr != nil {
			appctx.GetLogger(ctx).Error().Err(derr).Str("nodeid", n.ID).Str("blobid", sessionID).Msg("could not clean up orphaned blob after failed write")
		}
		return errors.Wrap(err, "Decomposedfs: failed to write blob")
	}

	now := time.Now()
	if p, err := n.Parent(ctx); err != nil {
		appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not read parent for etag propagation")
	} else {
		_ = p.SetTMTime(ctx, &now)
		if err := fs.tp.Propagate(ctx, p, 0); err != nil {
			appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not propagate etag change")
		}
	}

	return nil
}

// uploadRevision returns the locked revision of n that holds the upload sessionID,
// or nil if there is none.
func (fs *Decomposedfs) uploadRevision(ctx context.Context, n *node.Node, sessionID string) (*node.Node, metadata.UnlockFunc, error) {
	revPrefix := fs.lu.VersionPath(n.SpaceID, n.ID, "")
	revisions, err := filepath.Glob(fs.lu.VersionPath(n.SpaceID, n.ID, "*"))
	if err != nil {
		return nil, nil, err
	}
	for _, rev := range revisions {
		if strings.HasSuffix(rev, ".mpk") || strings.HasSuffix(rev, ".mlock") {
			continue
		}
		revisionID := n.ID + node.RevisionIDDelimiter + strings.TrimPrefix(rev, revPrefix)
		blobID, err := fs.lu.MetadataBackend().Get(ctx, node.NewBaseNode(n.SpaceID, revisionID, fs.lu), prefixes.BlobIDAttr)
		if err != nil || string(blobID) != sessionID {
			continue
		}
		revision, unlock, err := node.LockAndReadNode(ctx, fs.lu, n.SpaceID, revisionID, "", false, n.SpaceRoot, false)
		if err != nil {
			return nil, nil, err
		}
		return revision, unlock, nil
	}
	return nil, nil, nil
}
