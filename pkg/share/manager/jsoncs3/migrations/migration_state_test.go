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
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

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

// countingMigration is a migration stub that invokes a callback on every
// Migrate() call, allowing callers to observe how many times it ran.
type countingMigration struct {
	name    string
	version int
	onCall  func()
}

func (c *countingMigration) Name() string        { return c.name }
func (c *countingMigration) Version() int        { return c.version }
func (c *countingMigration) Initialize(_ config) {}
func (c *countingMigration) Migrate() error      { c.onCall(); return nil }

// marshalLockData is a test helper that JSON-encodes a lockData value.
func marshalLockData(d lockData) ([]byte, error) {
	return json.Marshal(d)
}

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

		// Replicate the directory setup that jsoncs3 Manager.initialize() does
		// so that tests driving storage directly behave like production.
		Expect(stor.MakeDirIfNotExist(context.Background(), "migrations")).To(Succeed())

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

	// ── Locking ────────────────────────────────────────────────────────────────

	Describe("distributed lock", func() {
		// Use a very short poll interval so the tests complete quickly.
		var origPollInterval time.Duration
		BeforeEach(func() {
			origPollInterval = lockPollInterval
			lockPollInterval = 10 * time.Millisecond

			m.instanceID = "instance-a"
		})
		AfterEach(func() {
			lockPollInterval = origPollInterval
		})

		Context("acquireLock", func() {
			It("succeeds and returns a non-empty etag when no lock exists", func() {
				etag, err := m.acquireLock(context.Background())
				Expect(err).NotTo(HaveOccurred())
				Expect(etag).NotTo(BeEmpty())
			})

			It("blocks until the first holder releases the lock", func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				// Instance A acquires the lock.
				_, err := m.acquireLock(ctx)
				Expect(err).NotTo(HaveOccurred())

				// Instance B tries to acquire in the background.
				m2 := Migrations{config: config{logger: log, storage: stor}, instanceID: "instance-b"}
				acquired := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					_, err := m2.acquireLock(ctx)
					Expect(err).NotTo(HaveOccurred())
					close(acquired)
				}()

				// B must not acquire while A still holds the lock.
				Consistently(acquired, 50*time.Millisecond, 10*time.Millisecond).ShouldNot(BeClosed())

				// A releases; B should now acquire.
				m.releaseLock(context.Background())
				Eventually(acquired, 500*time.Millisecond, 10*time.Millisecond).Should(BeClosed())
			})

			It("immediately takes over a stale lock", func() {
				// Write a lock file whose timestamp is well past lockTTL.
				staleData, err := marshalLockData(lockData{
					Timestamp:  time.Now().Add(-2 * lockTTL),
					InstanceID: "crashed-instance",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(stor.SimpleUpload(context.Background(), lockFile, staleData)).To(Succeed())

				// acquireLock should detect the stale lock and take it over without waiting.
				done := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					etag, err := m.acquireLock(context.Background())
					Expect(err).NotTo(HaveOccurred())
					Expect(etag).NotTo(BeEmpty())
					close(done)
				}()
				Eventually(done, 500*time.Millisecond, 10*time.Millisecond).Should(BeClosed())
			})
		})

		Context("releaseLock", func() {
			It("removes the lock file so a second caller can acquire immediately", func() {
				ctx := context.Background()
				_, err := m.acquireLock(ctx)
				Expect(err).NotTo(HaveOccurred())

				m.releaseLock(ctx)

				// A second instance should acquire without delay.
				m2 := Migrations{config: config{logger: log, storage: stor}, instanceID: "instance-b"}
				done := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					_, err := m2.acquireLock(ctx)
					Expect(err).NotTo(HaveOccurred())
					close(done)
				}()
				Eventually(done, 500*time.Millisecond, 10*time.Millisecond).Should(BeClosed())
			})
		})

		Context("RunMigrations with two concurrent instances", func() {
			It("runs each pending migration exactly once", func() {
				var callCount atomic.Int32
				mig := &countingMigration{name: "once", version: 1, onCall: func() { callCount.Add(1) }}
				migrations = []migration{mig}

				m2 := Migrations{config: config{logger: log, storage: stor}, instanceID: "instance-b"}

				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); m.RunMigrations() }()
				go func() { defer wg.Done(); m2.RunMigrations() }()
				wg.Wait()

				Expect(callCount.Load()).To(Equal(int32(1)))
			})
		})
	})
})
