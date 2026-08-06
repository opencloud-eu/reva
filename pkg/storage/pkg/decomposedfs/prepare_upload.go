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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/pkg/errors"

	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

// PrepareUpload finalizes node metadata after bytes are received, before postprocessing.
// CommitUpload is called after postprocessing completes.
//
// The previous version is snapshotted through the tree's CreateRevision, like the legacy
// upload path does: posix copies the current content, decomposedfs only the metadata.
func (fs *Decomposedfs) PrepareUpload(ctx context.Context, ref *provider.Reference, sessionID string, info storage.UploadInfo) (*storage.PrepareUploadResult, error) {
	ctx, span := tracer.Start(ctx, "PrepareUpload")
	defer span.End()

	n, err := fs.lu.NodeFromResource(ctx, ref)
	if err != nil {
		appctx.GetLogger(ctx).Error().Err(err).Msg("PrepareUpload: unexpected NodeFromResource failure")
		return nil, errtypes.InternalError("PrepareUpload: node lookup failed unexpectedly")
	}
	if !n.Exists {
		return nil, errtypes.NotFound(ref.String())
	}
	n.SpaceRoot, err = node.ReadNode(ctx, fs.lu, n.SpaceID, n.SpaceID, "", false, nil, false)
	if err != nil {
		return nil, err
	}

	if err := n.CheckLock(ctx); err != nil {
		return nil, err
	}

	// scope to space owner GID for posix deployments; no-op with NullMapper
	if spaceGID, ok := ctx.Value(CtxKeySpaceGID).(uint32); ok {
		unscope, err := fs.um.ScopeUserByIds(-1, int(spaceGID))
		if err != nil {
			return nil, errors.Wrap(err, "failed to scope user")
		}
		if unscope != nil {
			defer func() { _ = unscope() }()
		}
	}

	unlock, err := fs.lu.MetadataBackend().Lock(n)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := unlock(); err != nil {
			appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not close lock")
		}
	}()

	var (
		sizeDiff       int64
		versionCreated bool
		versionNode    *node.BaseNode
		oldAttrs       node.Attributes
		oldMtime       time.Time
		committed      bool
	)

	defer func() {
		if committed {
			return
		}
		if versionCreated {
			removeRevision(ctx, fs.lu, versionNode)
		}
		if info.NodeExisted && oldAttrs != nil {
			// mtime goes in the same batch: we still hold the metadata lock and
			// SetMTime would try to retake it
			if err := fs.lu.TimeManager().OverrideMtime(ctx, n, &oldAttrs, oldMtime); err != nil {
				appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not restore node mtime during rollback")
			}
			if err := n.SetXattrsWithContext(ctx, oldAttrs); err != nil {
				appctx.GetLogger(ctx).Error().Err(err).Str("nodeid", n.ID).Msg("could not restore node xattrs during rollback")
			}
		}
	}()

	var (
		old         *node.Node
		overwrite   bool
		oldBlobsize int64
	)
	if info.NodeExisted {
		old, err = node.ReadNode(ctx, fs.lu, n.SpaceID, n.ID, "", false, nil, false)
		if err != nil {
			return nil, errors.Wrap(err, "PrepareUpload: failed to read existing node")
		}
		overwrite = old.BlobID != ""
		oldBlobsize = old.Blobsize
	}

	// also for new files: the coordinator's check fails open without GetQuota
	// permission, and CheckQuota guards disk space too
	if _, err := node.CheckQuota(ctx, n.SpaceRoot, overwrite, uint64(oldBlobsize), uint64(info.Size)); err != nil {
		return nil, err
	}

	if info.NodeExisted {
		oldMtime, err = old.GetMTime(ctx)
		if err != nil {
			return nil, err
		}
		oldEtag, err := node.CalculateEtag(old.ID, oldMtime)
		if err != nil {
			return nil, err
		}

		if info.IfMatch != "" && info.IfMatch != oldEtag {
			return nil, errtypes.Aborted("etag mismatch")
		}
		if info.IfNoneMatch != "" {
			if info.IfNoneMatch == "*" {
				return nil, errtypes.Aborted("etag mismatch, resource exists")
			}
			for _, tag := range strings.Split(info.IfNoneMatch, ",") {
				if tag == oldEtag {
					return nil, errtypes.Aborted("etag mismatch")
				}
			}
		}
		if !info.IfUnmodifiedSince.IsZero() && oldMtime.After(info.IfUnmodifiedSince) {
			return nil, errtypes.Aborted("if-unmodified-since mismatch")
		}

		// capture full node xattrs for rollback before any write
		oldAttrs, err = fs.lu.MetadataBackend().All(ctx, n)
		if err != nil {
			return nil, err
		}

		if !fs.o.DisableVersioning {
			timestamp := oldMtime.UTC().Format(time.RFC3339Nano)
			versionPath, err := fs.tp.CreateRevision(ctx, n, timestamp)
			if err != nil {
				return nil, err
			}
			versionNode = node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+timestamp, fs.lu)
			versionCreated = true
			if err := os.Chtimes(versionPath, oldMtime, oldMtime); err != nil {
				return nil, errtypes.InternalError(fmt.Sprintf("failed to set mtime on version node: %s", err))
			}
		}

		sizeDiff = info.Size - old.Blobsize
	} else {
		if c, ok := fs.lu.(node.IDCacher); ok {
			if err := c.CacheID(ctx, n.SpaceID, n.ID, filepath.Join(n.ParentPath(), n.Name)); err != nil {
				appctx.GetLogger(ctx).Error().Err(err).Msg("failed to cache id")
			}
		}
		sizeDiff = info.Size
	}

	attrs := node.Attributes{}
	attrs.SetString(prefixes.IDAttr, n.ID)
	attrs.SetInt64(prefixes.TypeAttr, int64(provider.ResourceType_RESOURCE_TYPE_FILE))
	attrs.SetString(prefixes.ParentidAttr, n.ParentID)
	attrs.SetString(prefixes.NameAttr, n.Name)
	attrs.SetString(prefixes.BlobIDAttr, sessionID)
	attrs.SetInt64(prefixes.BlobsizeAttr, info.Size)
	attrs[prefixes.ChecksumPrefix+"sha1"] = info.Checksums.SHA1
	attrs[prefixes.ChecksumPrefix+"md5"] = info.Checksums.MD5
	attrs[prefixes.ChecksumPrefix+"adler32"] = info.Checksums.Adler32

	mtime := time.Now()
	if !info.MTime.IsZero() {
		mtime = info.MTime
	}
	if err := fs.lu.TimeManager().OverrideMtime(ctx, n, &attrs, mtime); err != nil {
		return nil, errors.Wrap(err, "failed to set mtime")
	}

	if err := n.SetXattrsWithContext(ctx, attrs); err != nil {
		return nil, errors.Wrap(err, "could not write metadata")
	}

	if err := fs.tp.Propagate(ctx, n, sizeDiff); err != nil {
		return nil, errors.Wrap(err, "could not propagate size change")
	}
	committed = true

	return &storage.PrepareUploadResult{VersionCreated: versionCreated, SizeDiff: sizeDiff}, nil
}

// removeRevision deletes a revision PrepareUpload created, with its metadata, while the
// caller holds the node lock (Node.DeleteRevision would try to take it again). Its blob
// is kept: the revision shares it with the node, whose old metadata is being restored.
func removeRevision(ctx context.Context, lu node.PathLookup, revisionNode *node.BaseNode) {
	log := appctx.GetLogger(ctx).With().Str("revisionid", revisionNode.ID).Logger()
	if err := os.Remove(revisionNode.InternalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Error().Err(err).Msg("could not remove revision during rollback")
	}
	if err := lu.MetadataBackend().Purge(ctx, revisionNode); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Error().Err(err).Msg("could not purge revision metadata during rollback")
	}
	if err := os.Remove(lu.MetadataBackend().LockfilePath(revisionNode)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Error().Err(err).Msg("could not remove revision lockfile during rollback")
	}
}

// RollbackUpload reverts the node state written by PrepareUpload after a failed or aborted
// postprocessing run. It restores the previous revision and reverts the optimistic size
// propagation. Unlike upstream it never purges an existing node: without a revision (versioning
// disabled) the previous metadata cannot be restored, so it returns an error instead. On posix
// this also keeps the file itself, which still holds the previous content until CommitUpload.
func (fs *Decomposedfs) RollbackUpload(ctx context.Context, ref *provider.Reference, sessionID string, nodeExisted bool, sizeDiff int64) error {
	n, err := fs.lu.NodeFromResource(ctx, ref)
	if err != nil {
		return fmt.Errorf("RollbackUpload: node lookup failed: %w", err)
	}
	if !n.Exists {
		return nil // nothing was written yet
	}
	n.SpaceRoot, err = node.ReadNode(ctx, fs.lu, n.SpaceID, n.SpaceID, "", false, nil, false)
	if err != nil {
		return err
	}

	curProcessingID, err := n.ProcessingID(ctx)
	if err != nil {
		return fmt.Errorf("RollbackUpload: could not read processing ID: %w", err)
	}
	if curProcessingID != sessionID {
		return nil
	}

	if nodeExisted {
		reverted, err := n.RevertLatestRevision(ctx)
		if err != nil {
			return err
		}
		if !reverted {
			return errtypes.PreconditionFailed("RollbackUpload: no revision to restore for node " + n.ID)
		}
	} else {
		// the upload created this node, so undo it. Purge, not trash: the file never
		// became visible, and Purge needs no Delete permission
		if err := n.Purge(ctx); err != nil {
			return fmt.Errorf("RollbackUpload: could not purge node: %w", err)
		}
	}

	if sizeDiff != 0 {
		if err := fs.tp.Propagate(ctx, n, -sizeDiff); err != nil {
			appctx.GetLogger(ctx).Error().Err(err).Msg("RollbackUpload: could not revert propagate")
		}
	}
	return nil
}
