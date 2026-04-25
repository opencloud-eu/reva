package filesystem

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/dirstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/pkg/errors"
	"go-micro.dev/v4/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/dirstore/filesystem")
}

// DirStore implements tree.DirStore using the local filesystem with symlinks.
type DirStore struct {
	root    string
	idCache store.Store
}

// New returns a new filesystem DirStore.
func New(root string, cache store.Store) *DirStore {
	return &DirStore{root: root, idCache: cache}
}

// parentPath returns the directory path for a given parentID node.
func (d *DirStore) parentPath(spaceID, parentID string) string {
	return filepath.Join(d.root, "spaces", lookup.Pathify(spaceID, 1, 2), "nodes", lookup.Pathify(parentID, 4, 2))
}

// Link creates a symlink named `name` in the parent directory pointing to the child node.
func (d *DirStore) Link(ctx context.Context, spaceID, parentID, name, childID string) error {
	_, span := tracer.Start(ctx, "Link")
	defer span.End()
	relativeNodePath := filepath.Join("../../../../../", lookup.Pathify(childID, 4, 2))
	childNameLink := filepath.Join(d.parentPath(spaceID, parentID), name)
	if err := os.Symlink(relativeNodePath, childNameLink); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return errors.Wrap(err, "filesystem dirstore: could not symlink child entry")
		}
		existing, rerr := os.Readlink(childNameLink)
		if rerr != nil {
			return errors.Wrap(rerr, "filesystem dirstore: could not read existing symlink")
		}
		if existing == relativeNodePath {
			return nil // idempotent, same target
		}
		return errtypes.AlreadyExists(name)
	}
	return nil
}

// Unlink removes the symlink named `name` from the parent directory.
func (d *DirStore) Unlink(ctx context.Context, spaceID, parentID, name string) error {
	_, span := tracer.Start(ctx, "Unlink")
	defer span.End()
	childNameLink := filepath.Join(d.parentPath(spaceID, parentID), name)

	// remove entry from cache immediately to avoid inconsistencies
	defer func() { _ = d.idCache.Delete(childNameLink) }()

	if err := os.Remove(childNameLink); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Wrap(err, "filesystem dirstore: could not remove symlink child entry")
	}
	return nil
}

// Move renames a directory entry, handling both same-parent renames and cross-parent moves.
func (d *DirStore) Move(ctx context.Context, spaceID, oldParentID, oldName, newParentID, newName string) error {
	_, span := tracer.Start(ctx, "Move")
	defer span.End()
	src := filepath.Join(d.parentPath(spaceID, oldParentID), oldName)
	dst := filepath.Join(d.parentPath(spaceID, newParentID), newName)

	// remove cache entry in any case to avoid inconsistencies
	defer func() { _ = d.idCache.Delete(src) }()

	if err := os.Rename(src, dst); err != nil {
		return errors.Wrap(err, "filesystem dirstore: could not move child entry")
	}
	return nil
}

// List reads the directory entries for parentID by reading symlinks.
func (d *DirStore) List(ctx context.Context, spaceID, parentID string) ([]dirstore.DirEntry, error) {
	_, span := tracer.Start(ctx, "List")
	defer span.End()
	dir := d.parentPath(spaceID, parentID)
	f, err := os.Open(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "filesystem dirstore: error opening directory")
	}
	defer f.Close()

	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, errors.Wrap(err, "filesystem dirstore: error reading directory")
	}

	entries := make([]dirstore.DirEntry, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)

		nodeID := d.getNodeIDFromCache(ctx, path)
		if nodeID == "" {
			nodeID, err = node.ReadChildNodeFromLink(ctx, path)
			if err != nil {
				return nil, errors.Wrapf(err, "filesystem dirstore: could not read symlink %s", path)
			}
			d.storeNodeIDInCache(ctx, path, nodeID)
		}

		entries = append(entries, dirstore.DirEntry{
			Name:   name,
			NodeID: nodeID,
		})
	}
	return entries, nil
}

func (d *DirStore) getNodeIDFromCache(ctx context.Context, path string) string {
	_, span := tracer.Start(ctx, "getNodeIDFromCache")
	defer span.End()
	recs, err := d.idCache.Read(path)
	if err == nil && len(recs) > 0 {
		return string(recs[0].Value)
	}
	return ""
}

func (d *DirStore) storeNodeIDInCache(ctx context.Context, path string, nodeID string) error {
	_, span := tracer.Start(ctx, "storeNodeIDInCache")
	defer span.End()
	return d.idCache.Write(&store.Record{
		Key:   path,
		Value: []byte(nodeID),
	})
}
