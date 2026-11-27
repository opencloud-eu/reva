package spaceidindex

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/renameio/v2"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/mtimesyncedcache"
	"github.com/pkg/errors"
	"github.com/rogpeppe/go-internal/lockedfile"
	"github.com/vmihailenco/msgpack/v5"
)

// Index holds space id indexes
type Index struct {
	root  string
	name  string
	cache mtimesyncedcache.Cache[string, map[string]string]
}

// writeLock is the in-process lock of one index file. refs counts the writers that hold it or wait for it.
type writeLock struct {
	mu   sync.Mutex
	refs int
}

var (
	writeLocksMu sync.Mutex
	writeLocks   = map[string]*writeLock{} // index path -> lock, only while a writer holds it or waits for it
)

// lockIndex serializes the writers of an index file within this process, so that at most one of them at a time
// waits for the file lock: several flock requests of one client waiting for the same file can be very slow on some
// shared filesystems (ceph-fuse). It returns the unlock function, which removes the entry again when no other
// writer of this process holds it or waits for it.
func lockIndex(indexPath string) (unlock func()) {
	writeLocksMu.Lock()
	l, ok := writeLocks[indexPath]
	if !ok {
		l = &writeLock{}
		writeLocks[indexPath] = l
	}
	l.refs++
	writeLocksMu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()

		writeLocksMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(writeLocks, indexPath)
		}
		writeLocksMu.Unlock()
	}
}

// New returns a new index instance
func New(root, name string) *Index {
	return &Index{
		root: root,
		name: name,
	}
}

// Init initializes the index and makes sure it can be used
func (i *Index) Init() error {
	// Make sure to work on an existing tree
	return os.MkdirAll(filepath.Join(i.root, i.name), 0700)
}

// Load returns the content of an index
func (i *Index) Load(index string) (map[string]string, error) {
	indexPath := filepath.Join(i.root, i.name, index+".mpk")
	fi, err := os.Stat(indexPath)
	if err != nil {
		return nil, err
	}
	return i.readSpaceIndex(indexPath, i.name+":"+index, fi.ModTime())
}

// Indexes returns the names of the indexes, sorted, e.g. the space types of an index by type.
func (i *Index) Indexes() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(i.root, i.name))
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".mpk"); ok && e.Type().IsRegular() {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// Add adds an entry to an index
// Consider calling AddAll() when trying to add multiple entries as every Add call has to lock the index
func (i *Index) Add(index, key string, value string) error {
	return i.updateIndex(index, map[string]string{key: value}, []string{})
}

// AddAll adds multiple entries to the index
func (i *Index) AddAll(index string, m map[string]string) error {
	return i.updateIndex(index, m, []string{})
}

// Remove removes an entry from the index
func (i *Index) Remove(index, key string) error {
	return i.updateIndex(index, map[string]string{}, []string{key})
}

func (i *Index) updateIndex(index string, addLinks map[string]string, removeLinks []string) (err error) {
	indexPath := filepath.Join(i.root, i.name, index+".mpk")

	unlock := lockIndex(indexPath)
	defer unlock()

	// acquire writelock on a separate lock file, like the node metadata does. The index itself is replaced
	// atomically and never truncated and rewritten in place: on a shared filesystem (e.g. the CephFS kernel client)
	// a writer on another node could otherwise read up to a stale size and drop entries, and a process that dies
	// between truncating and writing would leave an empty or partial index behind.
	l, err := lockedfile.OpenFile(indexPath+".mlock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return errors.Wrap(err, "unable to lock index to write")
	}
	defer func() {
		rerr := l.Close()

		// if err is non nil we do not overwrite that
		if err == nil {
			err = rerr
		}
	}()

	// Read current state
	links := map[string]string{}
	msgBytes, err := os.ReadFile(indexPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// no index yet
	case err != nil:
		return errors.Wrap(err, "unable to read index")
	case len(msgBytes) > 0:
		err = msgpack.Unmarshal(msgBytes, &links)
		if err != nil {
			return errors.Wrap(err, "unable to parse index")
		}
	}

	// set new metadata
	for key, val := range addLinks {
		links[key] = val
	}
	for _, key := range removeLinks {
		delete(links, key)
	}

	// Write new metadata to a temporary file next to the index, fsync it and rename it over the index
	d, err := msgpack.Marshal(links)
	if err != nil {
		return errors.Wrap(err, "unable to marshal index")
	}
	err = renameio.WriteFile(indexPath, d, 0600, renameio.WithTempDir(filepath.Dir(indexPath)))
	if err != nil {
		return errors.Wrap(err, "unable to write index")
	}
	return nil
}

func (i *Index) readSpaceIndex(indexPath, cacheKey string, mtime time.Time) (map[string]string, error) {
	return i.cache.LoadOrStore(cacheKey, mtime, func() (map[string]string, error) {
		// Acquire a read log on the index file
		f, err := lockedfile.Open(indexPath)
		if err != nil {
			return nil, errors.Wrap(err, "unable to lock index to read")
		}
		defer func() {
			rerr := f.Close()

			// if err is non nil we do not overwrite that
			if err == nil {
				err = rerr
			}
		}()

		// Read current state
		msgBytes, err := io.ReadAll(f)
		if err != nil {
			return nil, errors.Wrap(err, "unable to read index")
		}
		links := map[string]string{}
		if len(msgBytes) > 0 {
			err = msgpack.Unmarshal(msgBytes, &links)
			if err != nil {
				return nil, errors.Wrap(err, "unable to parse index")
			}
		}
		return links, nil
	})
}
