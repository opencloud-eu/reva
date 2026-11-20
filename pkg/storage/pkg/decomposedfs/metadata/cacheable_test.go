// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata_test

import (
	"context"
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/pkg/xattr"
)

// A read can come back incomplete without an error, e.g. on NFS right after the
// attributes were changed. The hybrid backend must not cache such a read.
var _ = Describe("incomplete reads", func() {
	newBackends := map[string]func(cfg cache.Config) metadata.Backend{
		"HybridBackend": func(cfg cache.Config) metadata.Backend {
			return metadata.NewHybridBackend(1024, func(n metadata.MetadataNode) string {
				return n.InternalPath() + ".mpk"
			}, cfg)
		},
	}

	for name, newBackend := range newBackends {
		Describe(name, func() {
			var (
				ctx     = context.Background()
				backend metadata.Backend
				n       *testNode
			)

			// setRaw writes attributes behind the back of the backend and its cache
			setRaw := func(key, val string) {
				ExpectWithOffset(1, xattr.Set(n.InternalPath(), key, []byte(val))).To(Succeed())
			}

			BeforeEach(func() {
				tmpdir, err := os.MkdirTemp(os.TempDir(), "CacheableTest-")
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() { _ = os.RemoveAll(tmpdir) })

				backend = newBackend(cache.Config{Database: tmpdir})
				n = &testNode{spaceID: "space", id: "node", path: path.Join(tmpdir, "file")}
				Expect(os.WriteFile(n.InternalPath(), nil, 0600)).To(Succeed())
			})

			It("caches a complete node", func() {
				setRaw(prefixes.IDAttr, "node")
				setRaw(prefixes.ParentidAttr, "parent")
				_, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())

				setRaw(prefixes.ParentidAttr, "changed behind the back of the cache")
				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue(prefixes.ParentidAttr, []byte("parent")))
			})

			It("caches a space root without a parent id", func() {
				setRaw(prefixes.IDAttr, "space")
				_, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())

				setRaw(prefixes.NameAttr, "changed behind the back of the cache")
				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).ToNot(HaveKey(prefixes.NameAttr))
			})

			It("does not cache a node without a parent id", func() {
				setRaw(prefixes.IDAttr, "node")
				setRaw(prefixes.MTimeAttr, "2026-11-19T17:02:13Z")
				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).ToNot(HaveKey(prefixes.ParentidAttr))

				setRaw(prefixes.ParentidAttr, "parent")
				attribs, err = backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue(prefixes.ParentidAttr, []byte("parent")))
			})

			It("does not cache a node without an id", func() {
				setRaw(prefixes.MTimeAttr, "2026-11-19T17:02:13Z")
				_, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())

				setRaw(prefixes.IDAttr, "node")
				setRaw(prefixes.ParentidAttr, "parent")
				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue(prefixes.IDAttr, []byte("node")))
			})

			It("drops the cached node when a write is followed by an incomplete read", func() {
				setRaw(prefixes.IDAttr, "node")
				setRaw(prefixes.ParentidAttr, "parent")
				_, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())

				// the write refreshes the cache from storage, which only shows part of the node
				Expect(xattr.Remove(n.InternalPath(), prefixes.ParentidAttr)).To(Succeed())
				Expect(backend.Set(ctx, n, prefixes.MetadataPrefix+"libre.graph.image.width", []byte("1280"))).To(Succeed())

				// once storage shows the whole node again we must not see the incomplete read
				setRaw(prefixes.ParentidAttr, "parent")
				attribs, err := backend.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue(prefixes.ParentidAttr, []byte("parent")))
				Expect(attribs).To(HaveKeyWithValue(prefixes.MetadataPrefix+"libre.graph.image.width", []byte("1280")))
			})
		})
	}
})
