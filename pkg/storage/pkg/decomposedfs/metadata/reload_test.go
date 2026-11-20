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
)

// Two backends with caches of their own stand for two processes on the same storage,
// e.g. the server and a command run next to it, each with a cache local to the process.
var _ = Describe("Reload", func() {
	newBackends := map[string]func(cfg cache.Config) metadata.Backend{
		"MessagePackBackend": func(cfg cache.Config) metadata.Backend {
			return metadata.NewMessagePackBackend(cfg)
		},
		"HybridBackend": func(cfg cache.Config) metadata.Backend {
			return metadata.NewHybridBackend(1024, func(n metadata.MetadataNode) string {
				return n.InternalPath() + ".mpk"
			}, cfg)
		},
	}

	for name, newBackend := range newBackends {
		Describe(name, func() {
			var (
				ctx    = context.Background()
				tmpdir string
				n      *testNode

				server, command metadata.Backend
			)

			BeforeEach(func() {
				var err error
				tmpdir, err = os.MkdirTemp(os.TempDir(), "ReloadTest-")
				Expect(err).ToNot(HaveOccurred())
				DeferCleanup(func() { _ = os.RemoveAll(tmpdir) })

				server = newBackend(cache.Config{Database: tmpdir, Table: "server"})
				command = newBackend(cache.Config{Database: tmpdir, Table: "command"})

				n = &testNode{spaceID: "123", id: "456", path: path.Join(tmpdir, "file")}
				Expect(os.WriteFile(n.InternalPath(), nil, 0600)).To(Succeed())

				Expect(server.Set(ctx, n, "user.foo", []byte("old"))).To(Succeed())
				// the command reads, and caches, the node
				attribs, err := command.All(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("old")))

				Expect(server.Set(ctx, n, "user.foo", []byte("new"))).To(Succeed())
			})

			It("reads what another process changed after the node was cached", func() {
				// the cache of the command is stale
				val, err := command.Get(ctx, n, "user.foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal([]byte("old")))

				attribs, err := command.Reload(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("new")))
			})

			It("updates the cache", func() {
				_, err := command.Reload(ctx, n)
				Expect(err).ToNot(HaveOccurred())

				val, err := command.Get(ctx, n, "user.foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal([]byte("new")))
			})

			It("doesn't return what another process removed", func() {
				Expect(server.Remove(ctx, n, "user.foo")).To(Succeed())

				attribs, err := command.Reload(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).ToNot(HaveKey("user.foo"))
			})

			It("works while the caller holds the lock of the node", func() {
				unlock, err := command.Lock(n)
				Expect(err).ToNot(HaveOccurred())
				defer func() { _ = unlock() }()

				attribs, err := command.Reload(ctx, n)
				Expect(err).ToNot(HaveOccurred())
				Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("new")))
			})
		})
	}
})
