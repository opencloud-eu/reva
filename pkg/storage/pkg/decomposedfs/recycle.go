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

package decomposedfs

import (
	"context"
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

type DecomposedfsTrashbin struct {
	fs *Decomposedfs
}

// Setup the trashbin
func (tb *DecomposedfsTrashbin) Setup(fs storage.FS) error {
	if _, ok := fs.(*Decomposedfs); !ok {
		return errors.New("invalid filesystem")
	}
	tb.fs = fs.(*Decomposedfs)
	return nil
}

// Recycle items are stored inside the node folder and start with the uuid of the deleted node.
// The `.T.` indicates it is a trash item and what follows is the timestamp of the deletion.
// The deleted file is kept in the same location/dir as the original node. This prevents deletes
// from triggering cross storage moves when the trash is accidentally stored on another partition,
// because the admin mounted a different partition there.
// For an efficient listing of deleted nodes the decomposedfs storage driver maintains a 'trash' folder
// with symlinks to trash files for every storagespace.

// ListRecycle returns the list of available recycle items
// ref -> the space (= resourceid), key -> deleted node id, relativePath = relative to key
func (tb *DecomposedfsTrashbin) ListRecycle(ctx context.Context, spaceID string, key, relativePath string) ([]*provider.RecycleItem, error) {
	_, span := tracer.Start(ctx, "ListRecycle")
	defer span.End()
	sublog := appctx.GetLogger(ctx).With().Str("spaceid", spaceID).Str("key", key).Str("relative_path", relativePath).Logger()

	if key == "" && relativePath == "" {
		return tb.listTrashRoot(ctx, spaceID)
	}

	// build a list of trash items relative to the given trash root and path
	items := make([]*provider.RecycleItem, 0)

	trashRootPath := filepath.Join(tb.getRecycleRoot(spaceID), lookup.Pathify(key, 4, 2))
	originalPath, id, timeSuffix, err := readTrashLink(trashRootPath)
	originalNode := node.NewBaseNode(spaceID, id+node.TrashIDDelimiter+timeSuffix, tb.fs.lu)
	if err != nil {
		sublog.Error().Err(err).Str("trashRoot", trashRootPath).Msg("error reading trash link")
		return nil, err
	}

	origin := ""
	raw, err := tb.fs.lu.MetadataBackend().All(ctx, originalNode)
	if err != nil {
		return items, err
	}
	attrs := node.Attributes(raw)
	// lookup origin path in extended attributes
	origin = attrs.String(prefixes.TrashOriginAttr)
	if origin == "" {
		sublog.Error().Err(err).Str("spaceid", spaceID).Msg("could not read origin path, skipping")
		return nil, err
	}

	// all deleted items have the same deletion time
	var deletionTime *types.Timestamp
	if parsed, err := time.Parse(time.RFC3339Nano, timeSuffix); err == nil {
		deletionTime = &types.Timestamp{
			Seconds: uint64(parsed.Unix()),
			// TODO nanos
		}
	} else {
		sublog.Error().Err(err).Msg("could not parse time format, ignoring")
	}

	var size uint64
	if relativePath == "" {
		// this is the case when we want to directly list a file in the trashbin
		typeInt, err := attrs.Int64(prefixes.TypeAttr)
		if err != nil {
			return items, err
		}
		switch provider.ResourceType(typeInt) {
		case provider.ResourceType_RESOURCE_TYPE_FILE:
			size, err = attrs.UInt64(prefixes.BlobsizeAttr)
			if err != nil {
				return items, err
			}
		case provider.ResourceType_RESOURCE_TYPE_CONTAINER:
			size, err = attrs.UInt64(prefixes.TreesizeAttr)
			if err != nil {
				return items, err
			}
		}
		item := &provider.RecycleItem{
			Type:         provider.ResourceType(typeInt),
			Size:         uint64(size),
			Key:          filepath.Join(key, relativePath),
			DeletionTime: deletionTime,
			Ref: &provider.Reference{
				Path: filepath.Join(origin, relativePath),
			},
		}
		items = append(items, item)
		return items, err
	}

	// we have to read the names and stat the path to follow the symlinks
	childrenPath := filepath.Join(originalPath, relativePath)
	childrenDir, err := os.Open(childrenPath)
	if err != nil {
		return nil, err
	}

	names, err := childrenDir.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		nodeID, err := node.ReadChildNodeFromLink(ctx, filepath.Join(childrenPath, name))
		if err != nil {
			sublog.Error().Err(err).Str("name", name).Msg("could not read child node, skipping")
			continue
		}
		childNode := node.NewBaseNode(spaceID, nodeID, tb.fs.lu)

		// reset size
		size = 0

		raw, err := tb.fs.lu.MetadataBackend().All(ctx, childNode)
		if err != nil {
			sublog.Error().Err(err).Str("name", name).Msg("could not read metadata, skipping")
			continue
		}
		attrs := node.Attributes(raw)
		typeInt, err := attrs.Int64(prefixes.TypeAttr)
		if err != nil {
			sublog.Error().Err(err).Str("name", name).Msg("could not read node type, skipping")
			continue
		}
		switch provider.ResourceType(typeInt) {
		case provider.ResourceType_RESOURCE_TYPE_FILE:
			size, err = attrs.UInt64(prefixes.BlobsizeAttr)
			if err != nil {
				sublog.Error().Err(err).Str("name", name).Msg("invalid blob size, skipping")
				continue
			}
		case provider.ResourceType_RESOURCE_TYPE_CONTAINER:
			size, err = attrs.UInt64(prefixes.TreesizeAttr)
			if err != nil {
				sublog.Error().Err(err).Str("name", name).Msg("invalid tree size, skipping")
				continue
			}
		case provider.ResourceType_RESOURCE_TYPE_INVALID:
			sublog.Error().Err(err).Str("name", name).Str("resolvedChildPath", filepath.Join(childrenPath, name)).Msg("invalid node type, skipping")
			continue
		}

		item := &provider.RecycleItem{
			Type:         provider.ResourceType(typeInt),
			Size:         size,
			Key:          filepath.Join(key, relativePath, name),
			DeletionTime: deletionTime,
			Ref: &provider.Reference{
				Path: filepath.Join(origin, relativePath, name),
			},
		}
		items = append(items, item)
	}
	return items, nil
}

// readTrashLink returns path, nodeID and timestamp
func readTrashLink(path string) (string, string, string, error) {
	link, err := os.Readlink(path)
	if err != nil {
		return "", "", "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", "", err
	}
	// ../../../../../nodes/e5/6c/75/a8/-d235-4cbb-8b4e-48b6fd0f2094.T.2022-02-16T14:38:11.769917408Z
	// TODO use filepath.Separator to support windows
	link = strings.ReplaceAll(link, "/", "")
	// ..........nodese56c75a8-d235-4cbb-8b4e-48b6fd0f2094.T.2022-02-16T14:38:11.769917408Z
	if link[0:15] != "..........nodes" || link[51:54] != node.TrashIDDelimiter {
		return "", "", "", errtypes.InternalError("malformed trash link")
	}
	return resolved, link[15:51], link[54:], nil
}

func (tb *DecomposedfsTrashbin) listTrashRoot(ctx context.Context, spaceID string) ([]*provider.RecycleItem, error) {
	log := appctx.GetLogger(ctx)
	trashRoot := tb.getRecycleRoot(spaceID)
	items := []*provider.RecycleItem{}
	subTrees, err := filepath.Glob(trashRoot + "/*")
	if err != nil {
		return nil, err
	}

	numWorkers := tb.fs.o.MaxConcurrency
	if len(subTrees) < numWorkers {
		numWorkers = len(subTrees)
	}

	work := make(chan string, len(subTrees))
	results := make(chan *provider.RecycleItem, len(subTrees))

	g, ctx := errgroup.WithContext(ctx)

	// Distribute work
	g.Go(func() error {
		defer close(work)
		for _, itemPath := range subTrees {
			select {
			case work <- itemPath:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Spawn workers that'll concurrently work the queue
	for i := 0; i < numWorkers; i++ {
		g.Go(func() error {
			for subTree := range work {
				matches, err := filepath.Glob(subTree + "/*/*/*/*")
				if err != nil {
					return err
				}

				for _, itemPath := range matches {
					// TODO can we encode this in the path instead of reading the link?
					nodePath, nodeID, timeSuffix, err := readTrashLink(itemPath)
					if err != nil {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Msg("error reading trash link, skipping")
						continue
					}

					baseNode := node.NewBaseNode(spaceID, nodeID+node.TrashIDDelimiter+timeSuffix, tb.fs.lu)

					md, err := os.Stat(nodePath)
					if err != nil {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Str("node_path", nodePath).Msg("could not stat trash item, skipping")
						continue
					}

					raw, err := tb.fs.lu.MetadataBackend().All(ctx, baseNode)
					if err != nil {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Str("node_path", nodePath).Msg("could not get extended attributes, skipping")
						continue
					}
					attrs := node.Attributes(raw)

					typeInt, err := attrs.Int64(prefixes.TypeAttr)
					if err != nil {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Str("node_path", nodePath).Msg("could not get node type, skipping")
						continue
					}
					if provider.ResourceType(typeInt) == provider.ResourceType_RESOURCE_TYPE_INVALID {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Str("node_path", nodePath).Msg("invalid node type, skipping")
						continue
					}

					item := &provider.RecycleItem{
						Type: provider.ResourceType(typeInt),
						Size: uint64(md.Size()),
						Key:  nodeID,
					}
					if deletionTime, err := time.Parse(time.RFC3339Nano, timeSuffix); err == nil {
						item.DeletionTime = &types.Timestamp{
							Seconds: uint64(deletionTime.Unix()),
							// TODO nanos
						}
					} else {
						log.Error().Err(err).Str("trashRoot", trashRoot).Str("item", itemPath).Str("spaceid", spaceID).Str("nodeid", nodeID).Str("dtime", timeSuffix).Msg("could not parse time format, ignoring")
					}

					// lookup origin path in extended attributes
					if attr, ok := attrs[prefixes.TrashOriginAttr]; ok {
						item.Ref = &provider.Reference{Path: string(attr)}
					} else {
						log.Error().Str("trashRoot", trashRoot).Str("item", itemPath).Str("spaceid", spaceID).Str("nodeid", nodeID).Str("dtime", timeSuffix).Msg("could not read origin path")
					}

					select {
					case results <- item:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return nil
		})
	}

	// Wait for things to settle down, then close results chan
	go func() {
		_ = g.Wait() // error is checked later
		close(results)
	}()

	// Collect results
	for ri := range results {
		items = append(items, ri)
	}
	return items, nil
}

var nodeIDRegep = regexp.MustCompile(`.*/nodes/([^.]*).*`)
var nodeFullIDRegep = regexp.MustCompile(`.*/nodes/(.*)`)

// TODO refactor the returned params into Node properties? would make all the path transformations go away...
func (tb *DecomposedfsTrashbin) readRecycleItem(ctx context.Context, spaceID, key, path string) (recycleNode *node.Node, trashItem string, origin string, err error) {
	_, span := tracer.Start(ctx, "readRecycleItem")
	defer span.End()
	logger := appctx.GetLogger(ctx)

	if key == "" {
		return nil, "", "", errtypes.InternalError("key is empty")
	}

	trashItem = filepath.Join(tb.fs.lu.InternalRoot(), "spaces", lookup.Pathify(spaceID, 1, 2), "trash", lookup.Pathify(key, 4, 2))
	resolvedTrashRootNodePath, err := filepath.EvalSymlinks(trashItem)
	trashedNodeId := nodeFullIDRegep.ReplaceAllString(resolvedTrashRootNodePath, "$1")
	trashedNodeId = strings.ReplaceAll(trashedNodeId, "/", "")

	if err != nil {
		return
	}
	recycleNodePath, err := filepath.EvalSymlinks(filepath.Join(resolvedTrashRootNodePath, path))
	if err != nil {
		return
	}
	nodeID := nodeFullIDRegep.ReplaceAllString(recycleNodePath, "$1")
	nodeID = strings.ReplaceAll(nodeID, "/", "")

	recycleNode = node.New(spaceID, nodeID, "", "", 0, "", provider.ResourceType_RESOURCE_TYPE_INVALID, nil, tb.fs.lu)
	recycleNode.SpaceRoot, err = node.ReadNode(ctx, tb.fs.lu, spaceID, spaceID, false, nil, false)
	if err != nil {
		return
	}
	raw, err := tb.fs.lu.MetadataBackend().All(ctx, recycleNode)
	if err != nil {
		return
	}
	attrs := node.Attributes(raw)

	typeInt, err := attrs.Int64(prefixes.TypeAttr)
	if provider.ResourceType(typeInt) == provider.ResourceType_RESOURCE_TYPE_FILE {
		// lookup blobID in extended attributes
		recycleNode.BlobID = attrs.String(prefixes.BlobIDAttr)
		if recycleNode.BlobID == "" {
			return
		}

		// lookup blobSize in extended attributes
		if recycleNode.Blobsize, err = attrs.Int64(prefixes.BlobsizeAttr); err != nil {
			return
		}
	}

	// lookup parent id in extended attributes
	recycleNode.ParentID = attrs.String(prefixes.ParentidAttr)
	if recycleNode.ParentID == "" {
		return
	}

	// lookup name in extended attributes
	recycleNode.Name = attrs.String(prefixes.NameAttr)
	if recycleNode.Name == "" {
		return
	}

	// get origin node, is relative to space root
	origin = "/"

	// lookup origin path in extended attributes
	rootNode := node.NewBaseNode(spaceID, trashedNodeId, tb.fs.lu)
	if attrBytes, err := tb.fs.lu.MetadataBackend().Get(ctx, rootNode, prefixes.TrashOriginAttr); err == nil {
		origin = filepath.Join(string(attrBytes), path)
	} else {
		logger.Error().Err(err).Str("trashItem", trashItem).Str("deletedNodePath", recycleNodePath).Msg("could not read origin path, restoring to /")
	}

	return
}

func (tb *DecomposedfsTrashbin) removeNode(ctx context.Context, n *node.Node) error {
	path := n.InternalPath()
	logger := appctx.GetLogger(ctx)

	if n.IsDir(ctx) {
		item, err := tb.fs.tp.ListFolder(ctx, n)
		if err != nil {
			logger.Error().Err(err).Str("path", path).Msg("error listing folder")
		} else {
			for _, child := range item {
				if err := tb.removeNode(ctx, child); err != nil {
					return err
				}
			}
		}
	}

	// delete the actual node
	if err := utils.RemoveItem(path); err != nil {
		logger.Error().Err(err).Str("path", path).Msg("error purging node")
		return err
	}

	if err := tb.fs.lu.MetadataBackend().Purge(ctx, n); err != nil {
		logger.Error().Err(err).Str("path", tb.fs.lu.MetadataBackend().MetadataPath(n)).Msg("error purging node metadata")
		return err
	}

	// delete blob from blobstore
	if n.BlobID != "" {
		if err := tb.fs.tp.DeleteBlob(n); err != nil {
			logger.Error().Err(err).Str("blobID", n.BlobID).Msg("error purging nodes blob")
			return err
		}
	}

	// delete revisions
	originalNodeID := nodeIDRegep.ReplaceAllString(n.InternalPath(), "$1")
	revs, err := filepath.Glob(originalNodeID + node.RevisionIDDelimiter + "*")
	if err != nil {
		logger.Error().Err(err).Str("path", n.InternalPath()+node.RevisionIDDelimiter+"*").Msg("glob failed badly")
		return err
	}
	for _, rev := range revs {
		if tb.fs.lu.MetadataBackend().IsMetaFile(rev) {
			continue
		}

		revID := nodeFullIDRegep.ReplaceAllString(rev, "$1")
		revID = strings.ReplaceAll(revID, "/", "")
		revNode := node.NewBaseNode(n.SpaceID, revID, tb.fs.lu)

		bID, _, err := tb.fs.lu.ReadBlobIDAndSizeAttr(ctx, revNode, nil)
		if err != nil {
			logger.Error().Err(err).Str("revision", rev).Msg("error reading blobid attribute")
			return err
		}

		if err := utils.RemoveItem(rev); err != nil {
			logger.Error().Err(err).Str("revision", rev).Msg("error removing revision node")
			return err
		}

		if bID != "" {
			if err := tb.fs.tp.DeleteBlob(&node.Node{
				BaseNode: node.BaseNode{
					SpaceID: n.SpaceID,
				}, BlobID: bID}); err != nil {
				logger.Error().Err(err).Str("revision", rev).Str("blobID", bID).Msg("error removing revision node blob")
				return err
			}
		}

	}

	return nil
}

// RestoreRecycleItem restores the specified item
func (tb *DecomposedfsTrashbin) RestoreRecycleItem(ctx context.Context, spaceID string, key, relativePath string, restoreRef *provider.Reference) error {
	_, span := tracer.Start(ctx, "RestoreRecycleItem")
	defer span.End()

	var targetNode *node.Node
	if restoreRef != nil {
		tn, err := tb.fs.lu.NodeFromResource(ctx, restoreRef)
		if err != nil {
			return err
		}

		targetNode = tn
	}

	trashNode, parent, restoreFunc, err := tb.restoreRecycleItemFunc(ctx, spaceID, key, relativePath, targetNode)
	if err != nil {
		return err
	}

	// check permissions of deleted node
	rp, err := tb.fs.p.AssembleTrashPermissions(ctx, trashNode)
	switch {
	case err != nil:
		return err
	case !rp.RestoreRecycleItem:
		if rp.Stat {
			return errtypes.PermissionDenied(key)
		}
		return errtypes.NotFound(key)
	}

	// Set space owner in context
	storagespace.ContextSendSpaceOwnerID(ctx, trashNode.SpaceOwnerOrManager(ctx))

	// check we can write to the parent of the restore reference
	pp, err := tb.fs.p.AssemblePermissions(ctx, parent)
	switch {
	case err != nil:
		return err
	case !pp.InitiateFileUpload:
		// share receiver cannot restore to a shared resource to which she does not have write permissions.
		if rp.Stat {
			return errtypes.PermissionDenied(key)
		}
		return errtypes.NotFound(key)
	}

	// Run the restore func
	return restoreFunc()
}

// restoreRecycleItemFunc returns a node and a function to restore it from the trash.
func (tb *DecomposedfsTrashbin) restoreRecycleItemFunc(ctx context.Context, spaceid, key, relativePath string, targetNode *node.Node) (*node.Node, *node.Node, func() error, error) {
	_, span := tracer.Start(ctx, "restoreRecycleItemFunc")
	defer span.End()
	logger := appctx.GetLogger(ctx)

	trashNode, trashItem, origin, err := tb.readRecycleItem(ctx, spaceid, key, relativePath)
	if err != nil {
		return nil, nil, nil, err
	}

	targetRef := &provider.Reference{
		ResourceId: &provider.ResourceId{SpaceId: spaceid, OpaqueId: spaceid},
		Path:       utils.MakeRelativePath(origin),
	}

	if targetNode == nil {
		targetNode, err = tb.fs.lu.NodeFromResource(ctx, targetRef)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if err := targetNode.CheckLock(ctx); err != nil {
		return nil, nil, nil, err
	}

	parentNode, err := targetNode.Parent(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	fn := func() error {
		if targetNode.Exists {
			return errtypes.AlreadyExists("origin already exists")
		}

		parts := strings.SplitN(trashNode.ID, node.TrashIDDelimiter, 2)
		originalId := parts[0]
		restoreNode := node.NewBaseNode(targetNode.SpaceID, originalId, tb.fs.lu)

		// add the entry for the parent dir
		err = os.Symlink("../../../../../"+lookup.Pathify(originalId, 4, 2), filepath.Join(targetNode.ParentPath(), targetNode.Name))
		if err != nil {
			return err
		}

		// attempt to rename only if we're not in a subfolder
		if trashNode.ID != restoreNode.ID {
			err = os.Rename(trashNode.InternalPath(), restoreNode.InternalPath())
			if err != nil {
				return err
			}
			err = tb.fs.lu.MetadataBackend().Rename(trashNode, restoreNode)
			if err != nil {
				return err
			}
		}

		targetNode.Exists = true

		attrs := node.Attributes{}
		attrs.SetString(prefixes.NameAttr, targetNode.Name)
		// set ParentidAttr to restorePath's node parent id
		attrs.SetString(prefixes.ParentidAttr, targetNode.ParentID)

		if err = tb.fs.lu.MetadataBackend().SetMultiple(ctx, restoreNode, map[string][]byte(attrs), true); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not update recycle node")
		}

		// delete item link in trash
		deletePath := trashItem
		if relativePath != "" && relativePath != "/" {
			resolvedTrashRoot, err := filepath.EvalSymlinks(trashItem)
			if err != nil {
				return errors.Wrap(err, "Decomposedfs: could not resolve trash root")
			}
			deletePath = filepath.Join(resolvedTrashRoot, relativePath)
			if err = os.Remove(deletePath); err != nil {
				logger.Error().Err(err).Str("trashItem", trashItem).Str("deletePath", deletePath).Str("relativePath", relativePath).Msg("error deleting trash item")
			}
		} else {
			if err = utils.RemoveItem(deletePath); err != nil {
				logger.Error().Err(err).Str("trashItem", trashItem).Str("deletePath", deletePath).Str("relativePath", relativePath).Msg("error recursively deleting trash item")
			}
		}

		var sizeDiff int64
		if trashNode.IsDir(ctx) {
			treeSize, err := trashNode.GetTreeSize(ctx)
			if err != nil {
				return err
			}
			sizeDiff = int64(treeSize)
		} else {
			sizeDiff = trashNode.Blobsize
		}
		return tb.fs.tp.Propagate(ctx, targetNode, sizeDiff)
	}
	return trashNode, parentNode, fn, nil
}

// PurgeRecycleItem purges the specified item, all its children and all their revisions
func (tb *DecomposedfsTrashbin) PurgeRecycleItem(ctx context.Context, spaceID, key, relativePath string) error {
	_, span := tracer.Start(ctx, "PurgeRecycleItem")
	defer span.End()

	rn, purgeFunc, err := tb.purgeRecycleItemFunc(ctx, spaceID, key, relativePath)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			return errtypes.NotFound(key)
		}
		return err
	}

	// check permissions of deleted node
	rp, err := tb.fs.p.AssembleTrashPermissions(ctx, rn)
	switch {
	case err != nil:
		return err
	case !rp.PurgeRecycle:
		if rp.Stat {
			return errtypes.PermissionDenied(key)
		}
		return errtypes.NotFound(key)
	}

	// Run the purge func
	return purgeFunc()
}

// EmptyRecycle empties the trash
func (tb *DecomposedfsTrashbin) EmptyRecycle(ctx context.Context, spaceID string) error {
	_, span := tracer.Start(ctx, "EmptyRecycle")
	defer span.End()

	items, err := tb.ListRecycle(ctx, spaceID, "", "")
	if err != nil {
		return err
	}

	for _, i := range items {
		if err := tb.PurgeRecycleItem(ctx, spaceID, i.Key, ""); err != nil {
			return err
		}
	}
	// TODO what permission should we check? we could check the root node of the user? or the owner permissions on his home root node?
	// The current impl will wipe your own trash. or when no user provided the trash of 'root'
	return os.RemoveAll(tb.getRecycleRoot(spaceID))
}

// purgeRecycleItemFunc returns a node and a function to purge it from the trash
func (tb *DecomposedfsTrashbin) purgeRecycleItemFunc(ctx context.Context, spaceid, key string, path string) (*node.Node, func() error, error) {
	_, span := tracer.Start(ctx, "purgeRecycleItemFunc")
	defer span.End()
	logger := appctx.GetLogger(ctx)

	recycleNode, trashItem, _, err := tb.readRecycleItem(ctx, spaceid, key, path)
	if err != nil {
		return nil, nil, err
	}

	fn := func() error {

		if err := tb.removeNode(ctx, recycleNode); err != nil {
			return err
		}

		// delete item link in trash
		if path != "" && path != "/" {
			return nil
		}
		if err = utils.RemoveItem(trashItem); err != nil {
			logger.Error().Err(err).Str("trashItem", trashItem).Msg("error deleting trash item")
			return err
		}

		return nil
	}

	return recycleNode, fn, nil
}
func (tb *DecomposedfsTrashbin) getRecycleRoot(spaceID string) string {
	return filepath.Join(tb.fs.o.Root, "spaces", lookup.Pathify(spaceID, 1, 2), "trash")
}
