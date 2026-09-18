// Copyright 2018-2022 CERN
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

package receivedsharecache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/mtimesyncedcache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// name is the Tracer name used to identify this instrumentation library.
const tracerName = "receivedsharecache"

// Cache stores the list of received shares and their states
// It functions as an in-memory cache with a persistence layer
// The storage is sharded by user
type Cache struct {
	lockMap sync.Map

	ReceivedSpaces mtimesyncedcache.Map[string, *Spaces]

	storage  metadata.Storage
	lockable metadata.LockingStorage
	ttl      time.Duration
}

// Spaces holds the received shares of one user per space
type Spaces struct {
	Spaces map[string]*Space

	etag string
}

// Space holds the received shares of one user in one space
type Space struct {
	States map[string]*State
}

// State holds the state information of a received share
type State struct {
	State      collaboration.ShareState
	MountPoint *provider.Reference
	Hidden     bool
}

// New returns a new Cache instance. Optional metadata.Options (e.g. a shared
// Locker) may be supplied to control how persisted writes are serialized; when
// the storage is a LockingStorage the default locker is used.
func New(s metadata.Storage, ttl time.Duration, opts ...metadata.Option) Cache {
	var lockable metadata.LockingStorage
	if ls, ok := s.(metadata.LockingStorage); ok {
		lockable = ls
	}
	return Cache{
		ReceivedSpaces: mtimesyncedcache.Map[string, *Spaces]{},
		storage:        s,
		lockable:       lockable,
		ttl:            ttl,
		lockMap:        sync.Map{},
	}
}

func (c *Cache) lockUser(userID utils.FilenameEncoder) func() {
	v, _ := c.lockMap.LoadOrStore(userID.SafeFilename(), &sync.Mutex{})
	lock := v.(*sync.Mutex)

	lock.Lock()
	return func() { lock.Unlock() }
}

// Add adds a new entry to the cache
func (c *Cache) Add(ctx context.Context, userID utils.FilenameEncoder, spaceID string, rs *collaboration.ReceivedShare) error {
	userIDKey := userID.SafeFilename()
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Grab lock")
	unlock := c.lockUser(userID)
	span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))
	defer unlock()

	if _, ok := c.ReceivedSpaces.Load(userIDKey); !ok {
		err := c.syncWithLock(ctx, userID)
		if err != nil {
			return err
		}
	}

	ctx, span = appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Add")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey), attribute.String("cs3.spaceid", spaceID))

	log := appctx.GetLogger(ctx).With().
		Str("hostname", os.Getenv("HOSTNAME")).
		Str("userIDKey", userIDKey).
		Str("spaceID", spaceID).Logger()

	var err error
	if c.lockable != nil {
		err = c.atomicPersist(ctx, userIDKey, func(existing *Spaces) (*Spaces, error) {
			if existing.Spaces[spaceID] == nil {
				existing.Spaces[spaceID] = &Space{States: map[string]*State{}}
			}
			existing.Spaces[spaceID].States[rs.Share.Id.GetOpaqueId()] = &State{
				State:      rs.State,
				MountPoint: rs.MountPoint,
				Hidden:     rs.Hidden,
			}
			return existing, nil
		})
	} else {
		c.initializeIfNeeded(userIDKey, spaceID)
		rss, _ := c.ReceivedSpaces.Load(userIDKey)
		receivedSpace := rss.Spaces[spaceID]
		if receivedSpace.States == nil {
			receivedSpace.States = map[string]*State{}
		}
		receivedSpace.States[rs.Share.Id.GetOpaqueId()] = &State{
			State:      rs.State,
			MountPoint: rs.MountPoint,
			Hidden:     rs.Hidden,
		}
		err = c.persist(ctx, userID)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, fmt.Sprintf("persisting added received share failed: %s", err.Error()))
		log.Error().Err(err).Msg("persisting added received share failed")
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// atomicPersist performs a locked read-modify-write of the user's received.json.
// fn receives the current on-storage state (a zero Spaces when the file does not
// yet exist) and returns the state to persist; returning nil aborts the write.
// On success the in-memory cache is refreshed in place from the written bytes so
// that the etag and content stay consistent with the storage.
func (c *Cache) atomicPersist(ctx context.Context, userIDKey string, fn func(existing *Spaces) (*Spaces, error)) error {
	_, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "atomicPersist")
	defer span.End()

	jsonPath := userJSONPath(userIDKey)
	var written []byte
	res, err := c.lockable.UploadWithLock(ctx, metadata.UploadRequest{Path: jsonPath}, func(existing []byte) ([]byte, error) {
		var rss *Spaces
		if len(existing) > 0 {
			rss = &Spaces{}
			if err := json.Unmarshal(existing, rss); err != nil {
				return nil, err
			}
			if rss.Spaces == nil {
				rss.Spaces = map[string]*Space{}
			}
		} else {
			rss = &Spaces{Spaces: map[string]*Space{}}
		}
		newState, err := fn(rss)
		if err != nil || newState == nil {
			return nil, err
		}
		b, err := json.Marshal(newState)
		if err != nil {
			return nil, err
		}
		written = b
		return b, nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Refresh the in-memory cache in place (mutate the existing *Spaces, do not
	// replace it) so callers holding a reference observe the update.
	rss, _ := c.ReceivedSpaces.LoadOrStore(userIDKey, &Spaces{Spaces: map[string]*Space{}})
	if len(written) > 0 {
		var fresh Spaces
		if err := json.Unmarshal(written, &fresh); err != nil {
			return err
		}
		rss.Spaces = fresh.Spaces
	}
	rss.etag = res.Etag

	span.SetStatus(codes.Ok, "")
	return nil
}

// Get returns one entry from the cache
func (c *Cache) Get(ctx context.Context, userID utils.FilenameEncoder, spaceID, shareID string) (*State, error) {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Grab lock")
	userIDKey := userID.SafeFilename()
	unlock := c.lockUser(userID)
	span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))
	defer unlock()

	err := c.syncWithLock(ctx, userID)
	if err != nil {
		return nil, err
	}
	rss, ok := c.ReceivedSpaces.Load(userIDKey)
	if !ok || rss.Spaces[spaceID] == nil {
		return nil, nil
	}
	return rss.Spaces[spaceID].States[shareID], nil
}

// Remove removes an entry from the cache
func (c *Cache) Remove(ctx context.Context, userID utils.FilenameEncoder, spaceID, shareID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Grab lock")
	userIDKey := userID.SafeFilename()
	unlock := c.lockUser(userID)
	span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))
	defer unlock()

	ctx, span = appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Add")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey), attribute.String("cs3.spaceid", spaceID))

	log := appctx.GetLogger(ctx).With().
		Str("hostname", os.Getenv("HOSTNAME")).
		Str("userIDKey", userIDKey).
		Str("spaceID", spaceID).Logger()

	var err error
	if c.lockable != nil {
		err = c.atomicPersist(ctx, userIDKey, func(existing *Spaces) (*Spaces, error) {
			if receivedSpace := existing.Spaces[spaceID]; receivedSpace != nil {
				delete(receivedSpace.States, shareID)
				if len(receivedSpace.States) == 0 {
					delete(existing.Spaces, spaceID)
				}
			}
			return existing, nil
		})
	} else {
		c.initializeIfNeeded(userIDKey, spaceID)
		rss, _ := c.ReceivedSpaces.Load(userIDKey)
		receivedSpace := rss.Spaces[spaceID]
		if receivedSpace.States == nil {
			receivedSpace.States = map[string]*State{}
		}
		delete(receivedSpace.States, shareID)
		if len(receivedSpace.States) == 0 {
			delete(rss.Spaces, spaceID)
		}
		err = c.persist(ctx, userID)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, fmt.Sprintf("persisting removed received share failed: %s", err.Error()))
		log.Error().Err(err).Msg("persisting removed received share failed")
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// List returns a list of received shares for a given user
// The return list is guaranteed to be thread-safe
func (c *Cache) List(ctx context.Context, userID utils.FilenameEncoder) (map[string]*Space, error) {
	userIDKey := userID.SafeFilename()
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Grab lock")
	unlock := c.lockUser(userID)
	span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))
	defer unlock()

	err := c.syncWithLock(ctx, userID)
	if err != nil {
		return nil, err
	}

	spaces := map[string]*Space{}
	rss, _ := c.ReceivedSpaces.Load(userIDKey)
	for spaceID, space := range rss.Spaces {
		spaceCopy := &Space{
			States: map[string]*State{},
		}
		for shareID, state := range space.States {
			spaceCopy.States[shareID] = &State{
				State:      state.State,
				MountPoint: state.MountPoint,
				Hidden:     state.Hidden,
			}
		}
		spaces[spaceID] = spaceCopy
	}
	return spaces, nil
}

func (c *Cache) syncWithLock(ctx context.Context, userID utils.FilenameEncoder) error {
	userIDKey := userID.SafeFilename()
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Sync")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))

	log := appctx.GetLogger(ctx).With().Str("userIDKey", userIDKey).Logger()

	c.initializeIfNeeded(userIDKey, "")

	jsonPath := userJSONPath(userIDKey)
	span.AddEvent("updating cache")
	//  - update cached list of created shares for the user in memory if changed
	rss, _ := c.ReceivedSpaces.Load(userIDKey)
	dlres, err := c.storage.Download(ctx, metadata.DownloadRequest{
		Path:        jsonPath,
		IfNoneMatch: []string{rss.etag},
	})
	switch err.(type) {
	case nil:
		span.AddEvent("updating local cache")
	case errtypes.NotFound:
		span.SetStatus(codes.Ok, "")
		return nil
	case errtypes.NotModified:
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		span.SetStatus(codes.Error, fmt.Sprintf("Failed to download the received share: %s", err.Error()))
		log.Error().Err(err).Msg("Failed to download the received share")
		return err
	}

	newSpaces := &Spaces{}
	err = json.Unmarshal(dlres.Content, newSpaces)
	if err != nil {
		span.SetStatus(codes.Error, fmt.Sprintf("Failed to unmarshal the received share: %s", err.Error()))
		log.Error().Err(err).Msg("Failed to unmarshal the received share")
		return err
	}
	newSpaces.etag = dlres.Etag

	c.ReceivedSpaces.Store(userIDKey, newSpaces)
	span.SetStatus(codes.Ok, "")
	return nil
}

// persist persists the data for one user to the storage
func (c *Cache) persist(ctx context.Context, userID utils.FilenameEncoder) error {
	userIDKey := userID.SafeFilename()
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Persist")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid.key", userIDKey))

	rss, ok := c.ReceivedSpaces.Load(userIDKey)
	if !ok {
		span.SetStatus(codes.Ok, "no received shares")
		return nil
	}

	createdBytes, err := json.Marshal(rss)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	jsonPath := userJSONPath(userIDKey)
	if err := c.storage.MakeDirIfNotExist(ctx, path.Dir(jsonPath)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	ur := metadata.UploadRequest{
		Path:        jsonPath,
		Content:     createdBytes,
		IfMatchEtag: rss.etag,
	}
	// when there is no etag in memory make sure the file has not been created on the server, see https://www.rfc-editor.org/rfc/rfc9110#field.if-match
	// > If the field value is "*", the condition is false if the origin server has a current representation for the target resource.
	if rss.etag == "" {
		ur.IfNoneMatch = []string{"*"}
	}

	res, err := c.storage.Upload(ctx, ur)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	rss.etag = res.Etag

	span.SetStatus(codes.Ok, "")
	return nil
}

func userJSONPath(userID string) string {
	return filepath.Join("/users", userID, "received.json")
}

func (c *Cache) initializeIfNeeded(userID, spaceID string) {
	rss, _ := c.ReceivedSpaces.LoadOrStore(userID, &Spaces{Spaces: map[string]*Space{}})
	if spaceID != "" && rss.Spaces[spaceID] == nil {
		rss.Spaces[spaceID] = &Space{}
		c.ReceivedSpaces.Store(userID, rss)
	}
}
