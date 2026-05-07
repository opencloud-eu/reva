// Copyright 2026 OpenCloud GmbH
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

package migration

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"

	gatewayv1beta1 "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/share"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
	"github.com/rs/zerolog"
)

const stateFile = "migrations/state.json"

type migration interface {
	Name() string
	Version() int
	Initialize(config)
	Migrate() error
}

// persistedState is the on-disk representation of the migration state.
type persistedState struct {
	Version int `json:"version"`
}

type state struct {
	version int
}

// MigrationConfig holds all caller-supplied options for a migration run.
// It is intentionally a plain struct so that new fields can be added without
// changing function signatures throughout the call chain.
type MigrationConfig struct {
	ServiceAccountID     string
	ServiceAccountSecret string
	ProviderRegistryAddr string
}

type config struct {
	logger               zerolog.Logger
	gatewaySelector      pool.Selectable[gatewayv1beta1.GatewayAPIClient]
	storage              metadata.Storage
	serviceAccountID     string
	serviceAccountSecret string
	providerRegistryAddr string
	manager              share.Manager
	loader               share.LoadableManager
}

type Migrations struct {
	config
	state state
}

var migrations []migration

// registerMigration is only supposed to be call from init(), which runs sequentially
// so we don't need ot protect migrations with a lock
func registerMigration(m migration) {
	migrations = append(migrations, m)
}

func New(logger zerolog.Logger,
	gatewaySelector pool.Selectable[gatewayv1beta1.GatewayAPIClient],
	storage metadata.Storage,
	cfg MigrationConfig,
	manager share.Manager,
	loader share.LoadableManager,
) Migrations {

	slices.SortFunc(migrations, func(a, b migration) int {
		return cmp.Compare(a.Version(), b.Version())
	})

	return Migrations{
		config{
			logger:               logger.With().Str("jsoncs3", "migrations").Logger(),
			gatewaySelector:      gatewaySelector,
			storage:              storage,
			serviceAccountID:     cfg.ServiceAccountID,
			serviceAccountSecret: cfg.ServiceAccountSecret,
			providerRegistryAddr: cfg.ProviderRegistryAddr,
			manager:              manager,
			loader:               loader,
		},
		state{},
	}
}

// loadState reads the persisted migration version from storage. If no state
// file exists yet (fresh deployment) it returns version 0 without error.
func (m *Migrations) loadState(ctx context.Context) error {
	data, err := m.storage.SimpleDownload(ctx, stateFile)
	if err != nil {
		if _, ok := err.(errtypes.IsNotFound); ok {
			m.state = state{version: 0}
			return nil
		}
		return err
	}
	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return err
	}
	m.state = state{version: ps.Version}
	return nil
}

// saveState writes the current migration version to storage so that already-
// applied migrations are not re-run on the next server start.
func (m *Migrations) saveState(ctx context.Context) error {
	data, err := json.Marshal(persistedState{Version: m.state.version})
	if err != nil {
		return err
	}
	return m.storage.SimpleUpload(ctx, stateFile, data)
}

func (m *Migrations) RunMigrations() {
	ctx := context.Background()

	if err := m.loadState(ctx); err != nil {
		m.logger.Error().Err(err).Msg("failed to load migration state; skipping migrations")
		return
	}

	m.logger.Info().Int("current state", m.state.version).Msg("checking migrations")

	for _, mig := range migrations {
		if mig.Version() > m.state.version {
			m.logger.Info().Str("migration", mig.Name()).Int("version", mig.Version()).Msg("running migration")
			mig.Initialize(m.config)
			if err := mig.Migrate(); err != nil {
				m.logger.Error().Err(err).Str("migration", mig.Name()).Msg("migration failed; stopping")
				return
			}
			m.state.version = mig.Version()
			if err := m.saveState(ctx); err != nil {
				m.logger.Error().Err(err).Msg("failed to save migration state; stopping")
				return
			}
		} else {
			m.logger.Info().Str("migration", mig.Name()).Int("version", mig.Version()).Msg("skipping migration")
		}
	}
}
