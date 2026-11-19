// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata_test

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/xattr"
	"github.com/rogpeppe/go-internal/lockedfile"

	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v3/pkg/storage/utils/filelocks"
)

// The hybrid backend (posix) locks to read the metadata of a node that isn't cached. The
// writer and the reader have caches of their own, so that the reader reads from disk.
var _ = Describe("WithReadOnly", func() {
	var (
		ctx    = context.Background()
		tmpdir string
		n      *testNode

		writer, reader metadata.HybridBackend
		lockfile       string
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp(os.TempDir(), "ReadOnlyTest-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tmpdir) })

		metadataPath := func(metadata.MetadataNode) string { return filepath.Join(tmpdir, ".oc-nodes") }
		writer = metadata.NewHybridBackend(1024, metadataPath, cache.Config{Database: tmpdir, Table: "writer"})
		reader = metadata.NewHybridBackend(1024, metadataPath, cache.Config{Database: tmpdir, Table: "reader"})

		n = &testNode{spaceID: "123", id: "456", path: path.Join(tmpdir, "file")}
		Expect(os.WriteFile(n.InternalPath(), nil, 0600)).To(Succeed())
		Expect(writer.Set(ctx, n, "user.foo", []byte("bar"))).To(Succeed())

		// a node that was never locked, e.g. a file created on disk and assimilated by posix
		lockfile = writer.LockfilePath(n)
		Expect(os.RemoveAll(filepath.Dir(lockfile))).To(Succeed())
	})

	It("is false for other contexts", func() {
		Expect(metadata.IsReadOnly(ctx)).To(BeFalse())
		Expect(metadata.IsReadOnly(metadata.WithReadOnly(ctx))).To(BeTrue())
	})

	It("reads without creating the lock file", func() {
		attribs, err := reader.All(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("bar")))

		attribs, err = reader.Reload(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("bar")))

		Expect(filepath.Dir(lockfile)).ToNot(BeADirectory())
		Expect(n.LockHeld()).To(BeFalse())
	})

	It("creates the lock file for other reads", func() {
		_, err := reader.All(ctx, n)
		Expect(err).ToNot(HaveOccurred())
		Expect(lockfile).To(BeAnExistingFile())
	})

	// A read without the lock may see a write halfway through, and must not replace what the
	// writer cached meanwhile. The attribute is changed on disk behind the backends' back, so
	// that the second read shows whether the first one was cached.
	It("doesn't cache what it read without the lock", func() {
		_, err := reader.All(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(xattr.Set(n.InternalPath(), "user.foo", []byte("baz"))).To(Succeed())

		attribs, err := reader.All(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("baz")))
	})

	It("caches what it read under the lock", func() {
		unlock, err := writer.Lock(n)
		Expect(err).ToNot(HaveOccurred())
		Expect(unlock()).To(Succeed())

		_, err = reader.All(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(xattr.Set(n.InternalPath(), "user.foo", []byte("baz"))).To(Succeed())

		attribs, err := reader.All(metadata.WithReadOnly(ctx), n)
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(HaveKeyWithValue("user.foo", []byte("bar")))
	})

	It("still waits for the lock when the lock file exists", func() {
		unlock, err := writer.Lock(n)
		Expect(err).ToNot(HaveOccurred())
		// the reader uses a node of its own, as a process of its own would
		readerNode := &testNode{spaceID: n.spaceID, id: n.id, path: n.path}

		done := make(chan error, 1)
		go func() {
			_, err := reader.Reload(metadata.WithReadOnly(ctx), readerNode)
			done <- err
		}()
		Consistently(done, 200*time.Millisecond).ShouldNot(Receive())

		Expect(unlock()).To(Succeed())
		Eventually(done, 5*time.Second).Should(Receive(BeNil()))
		Expect(readerNode.LockHeld()).To(BeFalse())
	})

	// The xattrs backend only locks to read when listing the attributes failed, and tries
	// again under the lock. Listing the attributes of a missing file always fails.
	Describe("the locked retry of the xattrs backend", func() {
		var (
			backend  metadata.XattrsBackend
			missing  *testNode
			lockfile string
		)

		BeforeEach(func() {
			backend = metadata.NewXattrsBackend(cache.Config{Database: tmpdir, Table: "xattrs"})
			missing = &testNode{spaceID: "123", id: "789", path: path.Join(tmpdir, "missing")}
			lockfile = missing.path + filelocks.LockFileSuffix
		})

		It("doesn't create the lock file", func() {
			_, err := backend.All(metadata.WithReadOnly(ctx), missing)
			Expect(err).To(HaveOccurred())
			Expect(lockfile).ToNot(BeAnExistingFile())
			Expect(missing.LockHeld()).To(BeFalse())
		})

		It("creates the lock file for other reads", func() {
			_, err := backend.All(ctx, missing)
			Expect(err).To(HaveOccurred())
			Expect(lockfile).To(BeAnExistingFile())
		})

		It("still waits for the lock when the lock file exists", func() {
			held, err := lockedfile.OpenFile(lockfile, os.O_CREATE|os.O_WRONLY, 0600)
			Expect(err).ToNot(HaveOccurred())

			done := make(chan error, 1)
			go func() {
				_, err := backend.All(metadata.WithReadOnly(ctx), missing)
				done <- err
			}()
			Consistently(done, 200*time.Millisecond).ShouldNot(Receive())

			Expect(held.Close()).To(Succeed())
			Eventually(done, 5*time.Second).Should(Receive(HaveOccurred()))
			Expect(missing.LockHeld()).To(BeFalse())
		})
	})
})
