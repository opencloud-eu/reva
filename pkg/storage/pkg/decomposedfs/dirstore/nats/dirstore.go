package nats

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/dirstore"
	"github.com/pkg/errors"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	walStateCommitted = "committed"
	walStatePending   = "pending"
)

type walEntry struct {
	Op          string `msgpack:"op"`
	SpaceID     string `msgpack:"space_id"`
	OldParentID string `msgpack:"old_parent_id"`
	OldName     string `msgpack:"old_name"`
	NewParentID string `msgpack:"new_parent_id"`
	NewName     string `msgpack:"new_name"`
	ChildID     string `msgpack:"child_id"`
	State       string `msgpack:"state"`
	CreatedAt   int64  `msgpack:"created_at"`
}

// DirStore implements tree.DirStore using NATS KV.
type DirStore struct {
	kv  jetstream.KeyValue // directory entries: {spaceID}.{parentID}.{name} -> childID
	wal jetstream.KeyValue // wal entries:       wal.{txid} -> msgpack(walEntry)
}

// New returns a new NATS DirStore.
func New(kv jetstream.KeyValue, wal jetstream.KeyValue) *DirStore {
	return &DirStore{kv: kv, wal: wal}
}

// Recover replays any pending WAL entries. Call once on startup before serving requests.
func (d *DirStore) Recover(ctx context.Context) error {
	watcher, err := d.wal.Watch(ctx, "wal.*", jetstream.IgnoreDeletes())
	if err != nil {
		return errors.Wrap(err, "nats dirstore: could not watch wal")
	}
	defer watcher.Stop()

	for entry := range watcher.Updates() {
		if entry == nil {
			break
		}
		var w walEntry
		if err := msgpack.Unmarshal(entry.Value(), &w); err != nil {
			return errors.Wrapf(err, "nats dirstore: could not unmarshal wal entry %s", entry.Key())
		}
		if w.State == walStateCommitted {
			continue
		}
		// pending — complete the move
		if err := d.applyMove(ctx, w); err != nil {
			return errors.Wrapf(err, "nats dirstore: could not recover wal entry %s", entry.Key())
		}
		if err := d.wal.Delete(ctx, entry.Key()); err != nil {
			return errors.Wrapf(err, "nats dirstore: could not delete wal entry %s", entry.Key())
		}
	}
	return nil
}

// Link creates a directory entry mapping name → childID under spaceID+parentID.
func (d *DirStore) Link(_ context.Context, spaceID, parentID, name, childID string) error {
	_, err := d.kv.Put(context.Background(), d.key(spaceID, parentID, name), []byte(childID))
	if err != nil {
		return errors.Wrap(err, "nats dirstore: could not link child entry")
	}
	return nil
}

// Unlink removes the directory entry for name under spaceID+parentID.
func (d *DirStore) Unlink(_ context.Context, spaceID, parentID, name string) error {
	err := d.kv.Delete(context.Background(), d.key(spaceID, parentID, name))
	if err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return errors.Wrap(err, "nats dirstore: could not unlink child entry")
	}
	return nil
}

// Move atomically renames a directory entry within or across parents using a WAL.
func (d *DirStore) Move(ctx context.Context, spaceID, oldParentID, oldName, newParentID, newName string) error {
	// read child id from source
	entry, err := d.kv.Get(ctx, d.key(spaceID, oldParentID, oldName))
	if err != nil {
		return errors.Wrap(err, "nats dirstore: could not get source entry")
	}
	childID := string(entry.Value())

	w := walEntry{
		Op:          "move",
		SpaceID:     spaceID,
		OldParentID: oldParentID,
		OldName:     oldName,
		NewParentID: newParentID,
		NewName:     newName,
		ChildID:     childID,
		State:       walStatePending,
		CreatedAt:   time.Now().UnixNano(),
	}

	// write WAL entry
	txid := uuid.New().String()
	walKey := "wal." + txid
	b, err := msgpack.Marshal(w)
	if err != nil {
		return errors.Wrap(err, "nats dirstore: could not marshal wal entry")
	}
	if _, err := d.wal.Put(ctx, walKey, b); err != nil {
		return errors.Wrap(err, "nats dirstore: could not write wal entry")
	}

	// apply the move
	if err := d.applyMove(ctx, w); err != nil {
		return errors.Wrap(err, "nats dirstore: could not apply move")
	}

	// delete WAL entry — move is complete
	if err := d.wal.Delete(ctx, walKey); err != nil {
		return errors.Wrap(err, "nats dirstore: could not delete wal entry")
	}

	return nil
}

// List returns all directory entries under spaceID+parentID.
func (d *DirStore) List(ctx context.Context, spaceID, parentID string) ([]dirstore.DirEntry, error) {
	watcher, err := d.kv.Watch(ctx, d.prefix(spaceID, parentID)+"*", jetstream.IgnoreDeletes())
	if err != nil {
		return nil, errors.Wrap(err, "nats dirstore: could not create watcher")
	}
	defer watcher.Stop()

	var entries []dirstore.DirEntry
	for entry := range watcher.Updates() {
		if entry == nil {
			break
		}
		name := strings.TrimPrefix(entry.Key(), d.prefix(spaceID, parentID))
		entries = append(entries, dirstore.DirEntry{
			Name:   name,
			NodeID: string(entry.Value()),
		})
	}
	return entries, nil
}

func (d *DirStore) applyMove(ctx context.Context, w walEntry) error {
	if _, err := d.kv.Put(ctx, d.key(w.SpaceID, w.NewParentID, w.NewName), []byte(w.ChildID)); err != nil {
		return errors.Wrap(err, "nats dirstore: could not put destination entry")
	}
	if err := d.kv.Delete(ctx, d.key(w.SpaceID, w.OldParentID, w.OldName)); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return errors.Wrap(err, "nats dirstore: could not delete source entry")
	}
	return nil
}

func (d *DirStore) key(spaceID, parentID, name string) string {
	return d.prefix(spaceID, parentID) + name
}

func (d *DirStore) prefix(spaceID, parentID string) string {
	return spaceID + "." + parentID + "."
}
