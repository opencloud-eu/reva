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

package migration

import (
	"context"
	"errors"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"

	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
)

// fakeMigration is a configurable stub that satisfies the migration interface.
type fakeMigration struct {
	name    string
	version int
	err     error // non-nil → Migrate() returns this error
	called  bool
}

func (f *fakeMigration) Name() string        { return f.name }
func (f *fakeMigration) Version() int        { return f.version }
func (f *fakeMigration) Initialize(_ config) {}
func (f *fakeMigration) Migrate() error      { f.called = true; return f.err }

var _ = Describe("RunMigrations / loadState / saveState", func() {
	var (
		tmpdir string
		stor   metadata.Storage
		m      Migrations
		log    zerolog.Logger

		// saved & restored around each test so our fakeMigrations don't leak
		originalMigrations []migration
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "migration-state-test-*")
		Expect(err).NotTo(HaveOccurred())

		stor, err = metadata.NewDiskStorage(tmpdir)
		Expect(err).NotTo(HaveOccurred())

		log = zerolog.Nop()
		m = Migrations{
			config: config{
				logger:  log,
				storage: stor,
			},
		}

		originalMigrations = migrations
		migrations = nil
	})

	AfterEach(func() {
		migrations = originalMigrations
		os.RemoveAll(tmpdir)
	})

	// ── loadState ──────────────────────────────────────────────────────────────

	Describe("loadState", func() {
		Context("when no state file exists yet", func() {
			It("returns version 0 without error (fresh deployment)", func() {
				Expect(m.loadState(context.Background())).To(Succeed())
				Expect(m.state.version).To(Equal(0))
			})
		})

		Context("when the state file contains valid JSON", func() {
			BeforeEach(func() {
				ctx := context.Background()
				Expect(stor.MakeDirIfNotExist(ctx, "migrations")).To(Succeed())
				Expect(stor.SimpleUpload(ctx, stateFile, []byte(`{"version":3}`))).To(Succeed())
			})

			It("restores the persisted version", func() {
				Expect(m.loadState(context.Background())).To(Succeed())
				Expect(m.state.version).To(Equal(3))
			})
		})

		Context("when the state file contains invalid JSON", func() {
			BeforeEach(func() {
				ctx := context.Background()
				Expect(stor.MakeDirIfNotExist(ctx, "migrations")).To(Succeed())
				Expect(stor.SimpleUpload(ctx, stateFile, []byte(`not-json`))).To(Succeed())
			})

			It("returns an error", func() {
				Expect(m.loadState(context.Background())).To(HaveOccurred())
			})
		})
	})

	// ── saveState ──────────────────────────────────────────────────────────────

	Describe("saveState", func() {
		It("persists the current version and can be round-tripped by loadState", func() {
			m.state.version = 7
			Expect(m.saveState(context.Background())).To(Succeed())

			m2 := Migrations{config: config{logger: log, storage: stor}}
			Expect(m2.loadState(context.Background())).To(Succeed())
			Expect(m2.state.version).To(Equal(7))
		})
	})

	// ── RunMigrations ──────────────────────────────────────────────────────────

	Describe("RunMigrations", func() {
		Context("when there are no registered migrations", func() {
			It("completes without error and leaves version at 0", func() {
				m.RunMigrations()
				Expect(m.state.version).To(Equal(0))
			})
		})

		Context("when no migration has been applied yet", func() {
			var (
				mig1 *fakeMigration
				mig2 *fakeMigration
			)

			BeforeEach(func() {
				mig1 = &fakeMigration{name: "first", version: 1}
				mig2 = &fakeMigration{name: "second", version: 2}
				migrations = []migration{mig1, mig2}
			})

			It("runs all migrations in order and saves the latest version", func() {
				m.RunMigrations()
				Expect(mig1.called).To(BeTrue())
				Expect(mig2.called).To(BeTrue())
				Expect(m.state.version).To(Equal(2))
			})
		})

		Context("when the state is already at version 1", func() {
			var (
				mig1 *fakeMigration
				mig2 *fakeMigration
			)

			BeforeEach(func() {
				// Persist version 1 so loadState inside RunMigrations picks it up.
				m.state.version = 1
				Expect(m.saveState(context.Background())).To(Succeed())
				m.state.version = 0 // reset; RunMigrations will reload from storage

				mig1 = &fakeMigration{name: "first", version: 1}
				mig2 = &fakeMigration{name: "second", version: 2}
				migrations = []migration{mig1, mig2}
			})

			It("skips the already-applied migration and only runs the pending one", func() {
				m.RunMigrations()
				Expect(mig1.called).To(BeFalse())
				Expect(mig2.called).To(BeTrue())
				Expect(m.state.version).To(Equal(2))
			})
		})

		Context("when a migration returns an error", func() {
			var (
				mig1 *fakeMigration
				mig2 *fakeMigration
			)

			BeforeEach(func() {
				mig1 = &fakeMigration{name: "broken", version: 1, err: errors.New("boom")}
				mig2 = &fakeMigration{name: "later", version: 2}
				migrations = []migration{mig1, mig2}
			})

			It("stops after the failing migration and does not run subsequent ones", func() {
				m.RunMigrations()
				Expect(mig1.called).To(BeTrue())
				Expect(mig2.called).To(BeFalse())
				// version must not advance past the failed migration
				Expect(m.state.version).To(Equal(0))
			})
		})

		Context("when all migrations are already applied (state == highest version)", func() {
			var mig *fakeMigration

			BeforeEach(func() {
				// Persist version 5 so loadState inside RunMigrations picks it up.
				m.state.version = 5
				Expect(m.saveState(context.Background())).To(Succeed())
				m.state.version = 0

				mig = &fakeMigration{name: "done", version: 5}
				migrations = []migration{mig}
			})

			It("skips every migration", func() {
				m.RunMigrations()
				Expect(mig.called).To(BeFalse())
			})
		})
	})
})
