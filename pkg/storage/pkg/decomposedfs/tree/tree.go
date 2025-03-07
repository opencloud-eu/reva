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

package tree

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree/propagator"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go-micro.dev/v4/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("github.com/cs3org/reva/pkg/storage/utils/decomposedfs/tree")
}

// Blobstore defines an interface for storing blobs in a blobstore
type Blobstore interface {
	Upload(node *node.Node, source string) error
	Download(node *node.Node) (io.ReadCloser, error)
	Delete(node *node.Node) error
}

// Tree manages a hierarchical tree
type Tree struct {
	lookup      node.PathLookup
	blobstore   Blobstore
	propagator  propagator.Propagator
	permissions permissions.Permissions

	options *options.Options

	idCache store.Store
}

// PermissionCheckFunc defined a function used to check resource permissions
type PermissionCheckFunc func(rp *provider.ResourcePermissions) bool

// New returns a new instance of Tree
func New(lu node.PathLookup, bs Blobstore, o *options.Options, p permissions.Permissions, cache store.Store, log *zerolog.Logger) *Tree {
	return &Tree{
		lookup:      lu,
		blobstore:   bs,
		options:     o,
		permissions: p,
		idCache:     cache,
		propagator:  propagator.New(lu, o, log),
	}
}

// Setup prepares the tree structure
func (t *Tree) Setup() error {
	// create data paths for internal layout
	dataPaths := []string{
		filepath.Join(t.options.Root, "spaces"),
		// notes contain symlinks from nodes/<u-u-i-d>/uploads/<uploadid> to ../../uploads/<uploadid>
		// better to keep uploads on a fast / volatile storage before a workflow finally moves them to the nodes dir
		filepath.Join(t.options.Root, "uploads"),
	}
	for _, v := range dataPaths {
		err := os.MkdirAll(v, 0700)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetMD returns the metadata of a node in the tree
func (t *Tree) GetMD(ctx context.Context, n *node.Node) (os.FileInfo, error) {
	_, span := tracer.Start(ctx, "GetMD")
	defer span.End()
	md, err := os.Stat(n.InternalPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errtypes.NotFound(n.ID)
		}
		return nil, errors.Wrap(err, "tree: error stating "+n.ID)
	}

	return md, nil
}

// TouchFile creates a new empty file
func (t *Tree) TouchFile(ctx context.Context, n *node.Node, markprocessing bool, mtime string) error {
	_, span := tracer.Start(ctx, "TouchFile")
	defer span.End()
	if n.Exists {
		if markprocessing {
			return n.SetXattr(ctx, prefixes.StatusPrefix, []byte(node.ProcessingStatus))
		}

		return errtypes.AlreadyExists(n.ID)
	}

	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	n.SetType(provider.ResourceType_RESOURCE_TYPE_FILE)

	nodePath := n.InternalPath()
	if err := os.MkdirAll(filepath.Dir(nodePath), 0700); err != nil {
		return errors.Wrap(err, "Decomposedfs: error creating node")
	}
	_, err := os.Create(nodePath)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: error creating node")
	}

	attributes := n.NodeMetadata(ctx)
	if markprocessing {
		attributes[prefixes.StatusPrefix] = []byte(node.ProcessingStatus)
	}
	if mtime != "" {
		if err := n.SetMtimeString(ctx, mtime); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not set mtime")
		}
	} else {
		now := time.Now()
		if err := n.SetMtime(ctx, &now); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not set mtime")
		}
	}
	err = n.SetXattrsWithContext(ctx, attributes, true)
	if err != nil {
		return err
	}

	// link child name to parent if it is new
	childNameLink := filepath.Join(n.ParentPath(), n.Name)
	var link string
	link, err = os.Readlink(childNameLink)
	if err == nil && link != "../"+n.ID {
		if err = os.Remove(childNameLink); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not remove symlink child entry")
		}
	}
	if errors.Is(err, fs.ErrNotExist) || link != "../"+n.ID {
		relativeNodePath := filepath.Join("../../../../../", lookup.Pathify(n.ID, 4, 2))
		if err = os.Symlink(relativeNodePath, childNameLink); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not symlink child entry")
		}
	}

	return t.Propagate(ctx, n, 0)
}

// CreateDir creates a new directory entry in the tree
func (t *Tree) CreateDir(ctx context.Context, n *node.Node) (err error) {
	ctx, span := tracer.Start(ctx, "CreateDir")
	defer span.End()
	if n.Exists {
		return errtypes.AlreadyExists(n.ID) // path?
	}

	// create a directory node
	n.SetType(provider.ResourceType_RESOURCE_TYPE_CONTAINER)
	if n.ID == "" {
		n.ID = uuid.New().String()
	}

	err = t.createDirNode(ctx, n)
	if err != nil {
		return
	}

	// make child appear in listings
	relativeNodePath := filepath.Join("../../../../../", lookup.Pathify(n.ID, 4, 2))
	ctx, subspan := tracer.Start(ctx, "os.Symlink")
	err = os.Symlink(relativeNodePath, filepath.Join(n.ParentPath(), n.Name))
	subspan.End()
	if err != nil {
		// no better way to check unfortunately
		if !strings.Contains(err.Error(), "file exists") {
			return
		}

		// try to remove the node
		ctx, subspan = tracer.Start(ctx, "os.RemoveAll")
		e := os.RemoveAll(n.InternalPath())
		subspan.End()
		if e != nil {
			appctx.GetLogger(ctx).Debug().Err(e).Msg("cannot delete node")
		}
		return errtypes.AlreadyExists(err.Error())
	}
	return t.Propagate(ctx, n, 0)
}

// Move replaces the target with the source
func (t *Tree) Move(ctx context.Context, oldNode *node.Node, newNode *node.Node) (err error) {
	_, span := tracer.Start(ctx, "Move")
	defer span.End()
	if oldNode.SpaceID != newNode.SpaceID {
		// WebDAV RFC https://www.rfc-editor.org/rfc/rfc4918#section-9.9.4 says to use
		// > 502 (Bad Gateway) - This may occur when the destination is on another
		// > server and the destination server refuses to accept the resource.
		// > This could also occur when the destination is on another sub-section
		// > of the same server namespace.
		// but we only have a not supported error
		return errtypes.NotSupported("cannot move across spaces")
	}
	// if target exists delete it without trashing it
	if newNode.Exists {
		// TODO make sure all children are deleted
		if err := os.RemoveAll(newNode.InternalPath()); err != nil {
			return errors.Wrap(err, "Decomposedfs: Move: error deleting target node "+newNode.ID)
		}
	}

	// remove cache entry in any case to avoid inconsistencies
	defer func() { _ = t.idCache.Delete(filepath.Join(oldNode.ParentPath(), oldNode.Name)) }()

	// Always target the old node ID for xattr updates.
	// The new node id is empty if the target does not exist
	// and we need to overwrite the new one when overwriting an existing path.
	// are we just renaming (parent stays the same)?
	if oldNode.ParentID == newNode.ParentID {

		// parentPath := t.lookup.InternalPath(oldNode.SpaceID, oldNode.ParentID)
		parentPath := oldNode.ParentPath()

		// rename child
		err = os.Rename(
			filepath.Join(parentPath, oldNode.Name),
			filepath.Join(parentPath, newNode.Name),
		)
		if err != nil {
			return errors.Wrap(err, "Decomposedfs: could not rename child")
		}

		// update name attribute
		if err := oldNode.SetXattrString(ctx, prefixes.NameAttr, newNode.Name); err != nil {
			return errors.Wrap(err, "Decomposedfs: could not set name attribute")
		}

		return t.Propagate(ctx, newNode, 0)
	}

	// we are moving the node to a new parent, any target has been removed
	// bring old node to the new parent

	// rename child
	err = os.Rename(
		filepath.Join(oldNode.ParentPath(), oldNode.Name),
		filepath.Join(newNode.ParentPath(), newNode.Name),
	)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: could not move child")
	}

	// update target parentid and name
	attribs := node.Attributes{}
	attribs.SetString(prefixes.ParentidAttr, newNode.ParentID)
	attribs.SetString(prefixes.NameAttr, newNode.Name)
	if err := oldNode.SetXattrsWithContext(ctx, attribs, true); err != nil {
		return errors.Wrap(err, "Decomposedfs: could not update old node attributes")
	}

	// the size diff is the current treesize or blobsize of the old/source node
	var sizeDiff int64
	if oldNode.IsDir(ctx) {
		treeSize, err := oldNode.GetTreeSize(ctx)
		if err != nil {
			return err
		}
		sizeDiff = int64(treeSize)
	} else {
		sizeDiff = oldNode.Blobsize
	}

	// TODO inefficient because we might update several nodes twice, only propagate unchanged nodes?
	// collect in a list, then only stat each node once
	// also do this in a go routine ... webdav should check the etag async

	err = t.Propagate(ctx, oldNode, -sizeDiff)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: Move: could not propagate old node")
	}
	err = t.Propagate(ctx, newNode, sizeDiff)
	if err != nil {
		return errors.Wrap(err, "Decomposedfs: Move: could not propagate new node")
	}
	return nil
}

// ListFolder lists the content of a folder node
func (t *Tree) ListFolder(ctx context.Context, n *node.Node) ([]*node.Node, error) {
	ctx, span := tracer.Start(ctx, "ListFolder")
	defer span.End()
	dir := n.InternalPath()

	_, subspan := tracer.Start(ctx, "os.Open")
	f, err := os.Open(dir)
	subspan.End()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errtypes.NotFound(dir)
		}
		return nil, errors.Wrap(err, "tree: error listing "+dir)
	}
	defer f.Close()

	_, subspan = tracer.Start(ctx, "f.Readdirnames")
	names, err := f.Readdirnames(0)
	subspan.End()
	if err != nil {
		return nil, err
	}

	numWorkers := t.options.MaxConcurrency
	if len(names) < numWorkers {
		numWorkers = len(names)
	}
	work := make(chan string)
	results := make(chan *node.Node)

	g, ctx := errgroup.WithContext(ctx)

	// Distribute work
	g.Go(func() error {
		defer close(work)
		for _, name := range names {
			select {
			case work <- name:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Spawn workers that'll concurrently work the queue
	for i := 0; i < numWorkers; i++ {
		g.Go(func() error {
			var err error
			for name := range work {
				path := filepath.Join(dir, name)
				nodeID := getNodeIDFromCache(ctx, path, t.idCache)
				if nodeID == "" {
					nodeID, err = node.ReadChildNodeFromLink(ctx, path)
					if err != nil {
						return err
					}
					err = storeNodeIDInCache(ctx, path, nodeID, t.idCache)
					if err != nil {
						return err
					}
				}

				child, err := node.ReadNode(ctx, t.lookup, n.SpaceID, nodeID, false, n.SpaceRoot, true)
				if err != nil {
					return err
				}

				// prevent listing denied resources
				if !child.IsDenied(ctx) {
					if child.SpaceRoot == nil {
						child.SpaceRoot = n.SpaceRoot
					}
					select {
					case results <- child:
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

	retNodes := []*node.Node{}
	for n := range results {
		retNodes = append(retNodes, n)
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return retNodes, nil
}

// Delete deletes a node in the tree by moving it to the trash
func (t *Tree) Delete(ctx context.Context, n *node.Node) (err error) {
	_, span := tracer.Start(ctx, "Delete")
	defer span.End()
	path := filepath.Join(n.ParentPath(), n.Name)
	// remove entry from cache immediately to avoid inconsistencies
	defer func() { _ = t.idCache.Delete(path) }()

	if appctx.DeletingSharedResourceFromContext(ctx) {
		src := filepath.Join(n.ParentPath(), n.Name)
		return os.Remove(src)
	}

	// get the original path
	origin, err := t.lookup.Path(ctx, n, node.NoCheck)
	if err != nil {
		return
	}

	// set origin location in metadata
	nodePath := n.InternalPath()
	if err := n.SetXattrString(ctx, prefixes.TrashOriginAttr, origin); err != nil {
		return err
	}

	var sizeDiff int64
	if n.IsDir(ctx) {
		treesize, err := n.GetTreeSize(ctx)
		if err != nil {
			return err // TODO calculate treesize if it is not set
		}
		sizeDiff = -int64(treesize)
	} else {
		sizeDiff = -n.Blobsize
	}

	deletionTime := time.Now().UTC().Format(time.RFC3339Nano)

	// Prepare the trash
	trashLink := filepath.Join(t.options.Root, "spaces", lookup.Pathify(n.SpaceRoot.ID, 1, 2), "trash", lookup.Pathify(n.ID, 4, 2))
	if err := os.MkdirAll(filepath.Dir(trashLink), 0700); err != nil {
		// Roll back changes
		_ = n.RemoveXattr(ctx, prefixes.TrashOriginAttr, true)
		return err
	}

	// FIXME can we just move the node into the trash dir? instead of adding another symlink and appending a trash timestamp?
	// can we just use the mtime as the trash time?
	// TODO store a trashed by userid

	// first make node appear in the space trash
	// parent id and name are stored as extended attributes in the node itself
	err = os.Symlink("../../../../../nodes/"+lookup.Pathify(n.ID, 4, 2)+node.TrashIDDelimiter+deletionTime, trashLink)
	if err != nil {
		// Roll back changes
		_ = n.RemoveXattr(ctx, prefixes.TrashOriginAttr, true)
		return
	}

	// at this point we have a symlink pointing to a non existing destination, which is fine

	// rename the trashed node so it is not picked up when traversing up the tree and matches the symlink
	trashNode := node.NewBaseNode(n.SpaceID, n.ID+node.TrashIDDelimiter+deletionTime, t.lookup)
	trashPath := trashNode.InternalPath()
	err = os.Rename(nodePath, trashPath)
	if err != nil {
		// To roll back changes
		// TODO remove symlink
		// Roll back changes
		_ = n.RemoveXattr(ctx, prefixes.TrashOriginAttr, true)
		return
	}
	err = t.lookup.MetadataBackend().Rename(n, trashNode)
	if err != nil {
		_ = n.RemoveXattr(ctx, prefixes.TrashOriginAttr, true)
		_ = os.Rename(trashPath, nodePath)
		return
	}

	// Remove lock file if it exists
	_ = os.Remove(n.LockFilePath())

	// finally remove the entry from the parent dir
	if err = os.Remove(path); err != nil {
		// To roll back changes
		// TODO revert the rename
		// TODO remove symlink
		// Roll back changes
		_ = n.RemoveXattr(ctx, prefixes.TrashOriginAttr, true)
		return
	}

	return t.Propagate(ctx, n, sizeDiff)
}

// InitNewNode initializes a new node
func (t *Tree) InitNewNode(ctx context.Context, n *node.Node, fsize uint64) (metadata.UnlockFunc, error) {
	_, span := tracer.Start(ctx, "InitNewNode")
	defer span.End()
	// create folder structure (if needed)

	_, subspan := tracer.Start(ctx, "os.MkdirAll")
	err := os.MkdirAll(filepath.Dir(n.InternalPath()), 0700)
	subspan.End()
	if err != nil {
		return nil, err
	}

	// create and write lock new node metadata
	_, subspan = tracer.Start(ctx, "metadata.Lock")
	unlock, err := t.lookup.MetadataBackend().Lock(n)
	subspan.End()
	if err != nil {
		return nil, err
	}

	// we also need to touch the actual node file here it stores the mtime of the resource
	_, subspan = tracer.Start(ctx, "os.OpenFile")
	h, err := os.OpenFile(n.InternalPath(), os.O_CREATE|os.O_EXCL, 0600)
	subspan.End()
	if err != nil {
		return unlock, err
	}
	h.Close()

	_, subspan = tracer.Start(ctx, "node.CheckQuota")
	_, err = node.CheckQuota(ctx, n.SpaceRoot, false, 0, fsize)
	subspan.End()
	if err != nil {
		return unlock, err
	}

	// link child name to parent if it is new
	childNameLink := filepath.Join(n.ParentPath(), n.Name)
	relativeNodePath := filepath.Join("../../../../../", lookup.Pathify(n.ID, 4, 2))
	log := appctx.GetLogger(ctx).With().Str("childNameLink", childNameLink).Str("relativeNodePath", relativeNodePath).Logger()
	log.Info().Msg("initNewNode: creating symlink")

	_, subspan = tracer.Start(ctx, "os.Symlink")
	err = os.Symlink(relativeNodePath, childNameLink)
	subspan.End()
	if err != nil {
		log.Info().Err(err).Msg("initNewNode: symlink failed")
		if errors.Is(err, fs.ErrExist) {
			log.Info().Err(err).Msg("initNewNode: symlink already exists")
			return unlock, errtypes.AlreadyExists(n.Name)
		}
		return unlock, errors.Wrap(err, "Decomposedfs: could not symlink child entry")
	}
	log.Info().Msg("initNewNode: symlink created")

	return unlock, nil
}

// Propagate propagates changes to the root of the tree
func (t *Tree) Propagate(ctx context.Context, n *node.Node, sizeDiff int64) (err error) {
	return t.propagator.Propagate(ctx, n, sizeDiff)
}

// WriteBlob writes a blob to the blobstore
func (t *Tree) WriteBlob(node *node.Node, source string) error {
	return t.blobstore.Upload(node, source)
}

// ReadBlob reads a blob from the blobstore
func (t *Tree) ReadBlob(node *node.Node) (io.ReadCloser, error) {
	if node.BlobID == "" {
		// there is no blob yet - we are dealing with a 0 byte file
		return io.NopCloser(bytes.NewReader([]byte{})), nil
	}
	return t.blobstore.Download(node)
}

// DeleteBlob deletes a blob from the blobstore
func (t *Tree) DeleteBlob(node *node.Node) error {
	if node == nil {
		return fmt.Errorf("could not delete blob, nil node was given")
	}
	if node.BlobID == "" {
		return fmt.Errorf("could not delete blob, node with empty blob id was given")
	}

	return t.blobstore.Delete(node)
}

// BuildSpaceIDIndexEntry returns the entry for the space id index
func (t *Tree) BuildSpaceIDIndexEntry(spaceID, nodeID string) string {
	return "../../../spaces/" + lookup.Pathify(spaceID, 1, 2) + "/nodes/" + lookup.Pathify(spaceID, 4, 2)
}

// ResolveSpaceIDIndexEntry returns the node id for the space id index entry
func (t *Tree) ResolveSpaceIDIndexEntry(_, entry string) (string, string, error) {
	return ReadSpaceAndNodeFromIndexLink(entry)
}

// ReadSpaceAndNodeFromIndexLink reads a symlink and parses space and node id if the link has the correct format, eg:
// ../../spaces/4c/510ada-c86b-4815-8820-42cdf82c3d51/nodes/4c/51/0a/da/-c86b-4815-8820-42cdf82c3d51
// ../../spaces/4c/510ada-c86b-4815-8820-42cdf82c3d51/nodes/4c/51/0a/da/-c86b-4815-8820-42cdf82c3d51.T.2022-02-24T12:35:18.196484592Z
func ReadSpaceAndNodeFromIndexLink(link string) (string, string, error) {
	// ../../../spaces/sp/ace-id/nodes/sh/or/tn/od/eid
	// 0  1  2  3      4  5      6     7  8  9  10  11
	parts := strings.Split(link, string(filepath.Separator))
	if len(parts) != 12 || parts[0] != ".." || parts[1] != ".." || parts[2] != ".." || parts[3] != "spaces" || parts[6] != "nodes" {
		return "", "", errtypes.InternalError("malformed link")
	}
	return strings.Join(parts[4:6], ""), strings.Join(parts[7:12], ""), nil
}

// TODO check if node exists?
func (t *Tree) createDirNode(ctx context.Context, n *node.Node) (err error) {
	ctx, span := tracer.Start(ctx, "createDirNode")
	defer span.End()
	// create a directory node
	nodePath := n.InternalPath()
	if err := os.MkdirAll(nodePath, 0700); err != nil {
		return errors.Wrap(err, "Decomposedfs: error creating node")
	}

	attributes := n.NodeMetadata(ctx)
	attributes[prefixes.TreesizeAttr] = []byte("0") // initialize as empty, TODO why bother? if it is not set we could treat it as 0?
	if t.options.TreeTimeAccounting || t.options.TreeSizeAccounting {
		attributes[prefixes.PropagationAttr] = []byte("1") // mark the node for propagation
	}
	return n.SetXattrsWithContext(ctx, attributes, true)
}

func getNodeIDFromCache(ctx context.Context, path string, cache store.Store) string {
	_, span := tracer.Start(ctx, "getNodeIDFromCache")
	defer span.End()
	recs, err := cache.Read(path)
	if err == nil && len(recs) > 0 {
		return string(recs[0].Value)
	}
	return ""
}

func storeNodeIDInCache(ctx context.Context, path string, nodeID string, cache store.Store) error {
	_, span := tracer.Start(ctx, "storeNodeIDInCache")
	defer span.End()
	return cache.Write(&store.Record{
		Key:   path,
		Value: []byte(nodeID),
	})
}
