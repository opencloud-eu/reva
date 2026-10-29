// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata_test

import (
	"context"
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
)

var _ = Describe("Metrics", func() {
	backends := []struct {
		name string
		// table separates the caches of backends sharing a directory
		new func(tmpdir, table string) metadata.Backend
	}{
		{"messagepack", func(tmpdir, table string) metadata.Backend {
			return metadata.NewMessagePackBackend(cache.Config{Database: tmpdir, Table: table})
		}},
		{"xattrs", func(tmpdir, table string) metadata.Backend {
			return metadata.NewXattrsBackend(cache.Config{Database: tmpdir, Table: table})
		}},
		{"hybrid", func(tmpdir, table string) metadata.Backend {
			return metadata.NewHybridBackend(1024, func(n metadata.MetadataNode) string {
				return n.InternalPath() + ".meta"
			}, cache.Config{Database: tmpdir, Table: table})
		}},
	}

	for _, b := range backends {
		Describe(b.name, func() {
			var (
				ctx     = context.Background()
				tmpdir  string
				backend metadata.Backend
				n       *testNode
			)

			metric := func(name string, labels ...string) float64 {
				v, err := helpers.MetricValue(name, append([]string{"backend", b.name}, labels...)...)
				Expect(err).ToNot(HaveOccurred())
				return v
			}

			BeforeEach(func() {
				var err error
				tmpdir, err = os.MkdirTemp(os.TempDir(), "MetricsTest-")
				Expect(err).ToNot(HaveOccurred())
				backend = b.new(tmpdir, "")
				n = &testNode{spaceID: "123", id: "456", path: path.Join(tmpdir, "file")}
				Expect(os.WriteFile(n.InternalPath(), nil, 0600)).To(Succeed())
			})

			AfterEach(func() {
				Expect(os.RemoveAll(tmpdir)).To(Succeed())
			})

			It("counts setting several attributes as one write", func() {
				writes := metric("reva_metadata_writes_total")

				err := backend.SetMultiple(ctx, n, map[string][]byte{"user.foo": []byte("bar"), "user.baz": []byte("qux")})
				Expect(err).ToNot(HaveOccurred())

				Expect(metric("reva_metadata_writes_total")).To(Equal(writes + 1))
			})

			It("counts the cache hits, which don't read from the filesystem", func() {
				Expect(backend.Set(ctx, n, "user.foo", []byte("bar"))).To(Succeed())
				reads := metric("reva_metadata_reads_total")
				hits := metric("reva_metadata_cache_lookups_total", "result", "hit")

				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("bar")))

				Expect(metric("reva_metadata_reads_total")).To(Equal(reads))
				Expect(metric("reva_metadata_cache_lookups_total", "result", "hit")).To(Equal(hits + 1))
			})

			It("counts the cache misses and the reads they cause", func() {
				Expect(backend.Set(ctx, n, "user.foo", []byte("bar"))).To(Succeed())
				reads := metric("reva_metadata_reads_total")
				misses := metric("reva_metadata_cache_lookups_total", "result", "miss")

				val, err := b.new(tmpdir, "empty").Get(ctx, n, "user.foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal([]byte("bar")))

				Expect(metric("reva_metadata_reads_total")).To(Equal(reads + 1))
				Expect(metric("reva_metadata_cache_lookups_total", "result", "miss")).To(Equal(misses + 1))
			})

			It("records how long a lock has been waited for and held", func() {
				waits := metric("reva_metadata_lock_wait_seconds", "space_root", "false")
				holds := metric("reva_metadata_lock_held_seconds", "space_root", "false")

				unlock, err := backend.Lock(n)
				Expect(err).ToNot(HaveOccurred())
				Expect(metric("reva_metadata_lock_wait_seconds", "space_root", "false")).To(Equal(waits + 1))
				Expect(metric("reva_metadata_lock_held_seconds", "space_root", "false")).To(Equal(holds))

				Expect(unlock()).To(Succeed())
				Expect(metric("reva_metadata_lock_held_seconds", "space_root", "false")).To(Equal(holds + 1))
			})

			It("records the locks of space roots separately", func() {
				root := &testNode{spaceID: "789", id: "789", path: path.Join(tmpdir, "root")}
				Expect(os.WriteFile(root.InternalPath(), nil, 0600)).To(Succeed())
				rootWaits := metric("reva_metadata_lock_wait_seconds", "space_root", "true")
				otherWaits := metric("reva_metadata_lock_wait_seconds", "space_root", "false")

				unlock, err := backend.Lock(root)
				Expect(err).ToNot(HaveOccurred())
				Expect(unlock()).To(Succeed())

				Expect(metric("reva_metadata_lock_wait_seconds", "space_root", "true")).To(Equal(rootWaits + 1))
				Expect(metric("reva_metadata_lock_wait_seconds", "space_root", "false")).To(Equal(otherWaits))
			})

			It("counts the locks being waited for", func() {
				unlock, err := backend.Lock(n)
				Expect(err).ToNot(HaveOccurred())
				waiting := metric("reva_metadata_lock_waiting")

				locked := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					// another node of the same file, which doesn't hold the lock
					other := &testNode{spaceID: n.spaceID, id: n.id, path: n.path}
					unlock, err := backend.Lock(other)
					Expect(err).ToNot(HaveOccurred())
					close(locked)
					Expect(unlock()).To(Succeed())
				}()

				Eventually(func() float64 { return metric("reva_metadata_lock_waiting") }).Should(Equal(waiting + 1))
				Consistently(locked, "100ms").ShouldNot(BeClosed())
				Expect(unlock()).To(Succeed())
				Eventually(locked).Should(BeClosed())
				Expect(metric("reva_metadata_lock_waiting")).To(Equal(waiting))
			})
		})
	}
})
