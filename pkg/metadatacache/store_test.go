// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
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

package metadatacache_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/metadatacache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMetadatacache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metadatacache Suite")
}

func newStoreOnDir(dir string) *metadatacache.Store[string, map[string]string] {
	mds, err := metadata.NewDiskStorage(dir)
	Expect(err).NotTo(HaveOccurred())
	ctx := context.Background()
	Expect(mds.Init(ctx, "test")).To(Succeed())
	return metadatacache.New(metadatacache.Options[string, map[string]string]{
		Storage: mds,
		Path:    func(key string) string { return key + ".json" },
		Init:    func() map[string]string { return map[string]string{} },
	})
}

var _ = Describe("Store", func() {
	var (
		ctx   context.Context
		dir   string
		store *metadatacache.Store[string, map[string]string]
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		store = newStoreOnDir(dir)
	})

	Describe("Get", func() {
		It("returns not-found for an absent key", func() {
			unlock := store.Lock("alice")
			defer unlock()
			_, ok, err := store.Get(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Update", func() {
		It("creates an entry when createIfNotFound is true", func() {
			err := store.Update(ctx, "alice", true, func(m map[string]string) (map[string]string, bool, error) {
				m["k"] = "v"
				return m, true, nil
			})
			Expect(err).NotTo(HaveOccurred())

			unlock := store.Lock("alice")
			defer unlock()
			val, ok, err := store.Get(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(val).To(HaveKeyWithValue("k", "v"))
		})

		It("returns NotFound when createIfNotFound is false and key is absent", func() {
			err := store.Update(ctx, "bob", false, func(m map[string]string) (map[string]string, bool, error) {
				return m, true, nil
			})
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
		})

		It("does not persist when shouldPersist is false", func() {
			// Seed a value first.
			Expect(store.Update(ctx, "alice", true, func(m map[string]string) (map[string]string, bool, error) {
				m["a"] = "1"
				return m, true, nil
			})).To(Succeed())

			// Read-only update (shouldPersist=false).
			var captured map[string]string
			Expect(store.Update(ctx, "alice", false, func(m map[string]string) (map[string]string, bool, error) {
				captured = m
				return m, false, nil
			})).To(Succeed())
			Expect(captured).To(HaveKeyWithValue("a", "1"))
		})

		It("round-trips through Sync after Persist", func() {
			Expect(store.Update(ctx, "alice", true, func(m map[string]string) (map[string]string, bool, error) {
				m["hello"] = "world"
				return m, true, nil
			})).To(Succeed())

			// Second store on the same directory simulates a fresh process.
			store2 := newStoreOnDir(dir)
			unlock := store2.Lock("alice")
			defer unlock()
			val, ok, err := store2.Get(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(val).To(HaveKeyWithValue("hello", "world"))
		})

		It("is safe under concurrent updates to the same key (race detector)", func() {
			const goroutines = 20
			var wg sync.WaitGroup
			wg.Add(goroutines)
			for i := 0; i < goroutines; i++ {
				go func(n int) {
					defer wg.Done()
					_ = store.Update(ctx, "shared", true, func(m map[string]string) (map[string]string, bool, error) {
						key := fmt.Sprintf("g%d", n)
						m[key] = key
						return m, true, nil
					})
				}(i)
			}
			wg.Wait()

			unlock := store.Lock("shared")
			defer unlock()
			val, ok, err := store.Get(ctx, "shared")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(val).To(HaveLen(goroutines))
		})

		It("is safe under concurrent updates to different keys (race detector)", func() {
			keys := []string{"alice", "bob", "carol", "dave"}
			var wg sync.WaitGroup
			wg.Add(len(keys))
			for _, k := range keys {
				go func(key string) {
					defer wg.Done()
					_ = store.Update(ctx, key, true, func(m map[string]string) (map[string]string, bool, error) {
						m["x"] = key
						return m, true, nil
					})
				}(k)
			}
			wg.Wait()
		})
	})
})
