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

package cs3

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/json/persistence"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

type db struct {
	mtime        time.Time
	publicShares persistence.PublicShares
}

type cs3 struct {
	initialized bool
	s           metadata.Storage
	lockable    metadata.LockingStorage

	db db
}

// New returns a new Cache instance. When the storage is a LockingStorage it is
// used to serialise read-modify-write cycles across replicas; otherwise Update
// falls back to an unlocked read-modify-write.
func New(s metadata.Storage) persistence.Persistence {
	var lockable metadata.LockingStorage
	if ls, ok := s.(metadata.LockingStorage); ok {
		lockable = ls
	}
	return &cs3{
		s:        s,
		lockable: lockable,
		db: db{
			publicShares: persistence.PublicShares{},
		},
	}
}

func (p *cs3) Init(ctx context.Context) error {
	if p.initialized {
		return nil
	}

	err := p.s.Init(ctx, "jsoncs3-public-share-manager-metadata")
	if err != nil {
		return err
	}
	p.initialized = true

	return nil
}

func (p *cs3) Read(ctx context.Context) (persistence.PublicShares, error) {
	if !p.initialized {
		return nil, fmt.Errorf("not initialized")
	}

	info, err := p.s.Stat(ctx, "publicshares.json")
	if err != nil {
		if _, ok := err.(errtypes.NotFound); ok {
			return p.db.publicShares, nil // Nothing to sync against
		}
		return nil, err
	}

	if utils.TSToTime(info.Mtime).After(p.db.mtime) {
		readBytes, err := p.s.SimpleDownload(ctx, "publicshares.json")
		if err != nil {
			return nil, err
		}
		p.db.publicShares = persistence.PublicShares{}
		if err := json.Unmarshal(readBytes, &p.db.publicShares); err != nil {
			return nil, err
		}
		p.db.mtime = utils.TSToTime(info.Mtime)
	}
	return p.db.publicShares, nil
}

func (p *cs3) Write(ctx context.Context, db persistence.PublicShares) error {
	if !p.initialized {
		return fmt.Errorf("not initialized")
	}
	dbAsJSON, err := json.Marshal(db)
	if err != nil {
		return err
	}

	_, err = p.s.Upload(ctx, metadata.UploadRequest{
		Content:           dbAsJSON,
		Path:              "publicshares.json",
		IfUnmodifiedSince: p.db.mtime,
	})
	return err
}

// Update atomically reads the current publicshares.json, applies fn, and writes
// the result back while holding a lock on the file. When the storage supports
// locking (LockingStorage) this is fully atomic across replicas; otherwise it
// degrades to an unlocked read-modify-write.
func (p *cs3) Update(ctx context.Context, fn func(current persistence.PublicShares) (persistence.PublicShares, error)) error {
	if !p.initialized {
		return fmt.Errorf("not initialized")
	}

	const path = "publicshares.json"

	if p.lockable != nil {
		_, err := p.lockable.UploadWithLock(ctx, metadata.UploadRequest{Path: path}, func(existing []byte) ([]byte, error) {
			current := persistence.PublicShares{}
			if len(existing) > 0 {
				if err := json.Unmarshal(existing, &current); err != nil {
					return nil, err
				}
			}
			next, err := fn(current)
			if err != nil {
				return nil, err
			}
			return json.Marshal(next)
		})
		return err
	}

	// Fallback: no lock support, plain read-modify-write.
	current, err := p.Read(ctx)
	if err != nil {
		return err
	}
	next, err := fn(current)
	if err != nil {
		return err
	}
	return p.Write(ctx, next)
}
