// Package dirstore defines the interface for directory entry storage in decomposedfs.
//
// Decomposedfs represents a hierarchical filesystem where each node (file or directory)
// is identified by a UUID. Directory entries map human-readable names to node IDs,
// analogous to POSIX directory entries mapping filenames to inodes.
//
// Implementations must be safe for concurrent use.
package dirstore

import "context"

// DirEntry represents a single entry in a directory.
// It maps a human-readable name to the ID of the child node.
type DirEntry struct {
	// Name is the human-readable name of the child node as it appears in the directory.
	Name string
	// NodeID is the UUID of the child node within the space.
	NodeID string
}

// DirStore abstracts the storage of directory entries.
//
// All operations are scoped to a space identified by spaceID. Within a space,
// directories are identified by their node UUID (parentID), and entries within
// a directory are identified by their human-readable name.
type DirStore interface {
	// Link creates a directory entry mapping name to childID under parentID in the given space.
	// If an entry for name already exists and points to a different node, Link returns
	// errtypes.AlreadyExists. If the entry already points to childID, Link is a no-op.
	Link(ctx context.Context, spaceID, parentID, name, childID string) error

	// Unlink removes the directory entry for name under parentID in the given space.
	// If the entry does not exist, Unlink is a no-op.
	Unlink(ctx context.Context, spaceID, parentID, name string) error

	// Move renames a directory entry within or across parent directories in the given space.
	// If the source entry does not exist, Move returns an error.
	// If the destination entry already exists, it is overwritten.
	Move(ctx context.Context, spaceID, oldParentID, oldName, newParentID, newName string) error

	// List returns all directory entries under parentID in the given space.
	// The order of entries is undefined.
	// If the directory does not exist or is empty, List returns an empty slice and no error.
	List(ctx context.Context, spaceID, parentID string) ([]DirEntry, error)
}
