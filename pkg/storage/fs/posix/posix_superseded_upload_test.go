// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package posix_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	posixblobstore "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Two uploads to the same file overlap: B prepares while A is still processing.
// Whatever happens to B, A's bytes and metadata must be able to end up on the file.
var _ = Describe("a newer upload supersedes one still processing on posix", func() {
	var (
		env *posixhelpers.TestEnv
		ref *provider.Reference
	)

	oldContent := []byte("old content")
	contentA := []byte("content of upload A")
	contentB := []byte("content of upload B, longer")

	// an older version of the file, from before versioning was disabled
	olderVersion := []byte("older version")

	setup := func(config map[string]interface{}, withOlderVersion bool) {
		var err error
		env, err = posixhelpers.NewTestEnv(config)
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
			&provider.ResourcePermissions{Stat: true, InitiateFileUpload: true}, nil)

		// the test env mocks the blobstore; let it write through the real posix blobstore
		bs, err := posixblobstore.New(env.Root)
		Expect(err).ToNot(HaveOccurred())
		env.Blobstore.On("UploadFromReader", mock.Anything, mock.Anything, mock.Anything).Return(bs.UploadFromReader).Maybe()
		env.Blobstore.On("Delete", mock.Anything).Return(nil).Maybe()

		ref = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./target.txt"}
		Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		if withOlderVersion {
			Expect(os.WriteFile(n.InternalPath(), olderVersion, 0600)).To(Succeed())
			_, err := env.Tree.CreateRevision(env.Ctx, n, "2020-01-01T00:00:00Z")
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(os.WriteFile(n.InternalPath(), oldContent, 0600)).To(Succeed())
		Expect(n.SetXattrsWithContext(env.Ctx, map[string][]byte{
			prefixes.BlobsizeAttr: []byte("11"),
		})).To(Succeed())

		for _, u := range []struct {
			id   string
			size int
		}{{"session-a", len(contentA)}, {"session-b", len(contentB)}} {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, u.id, storage.UploadInfo{NodeExisted: true, Size: int64(u.size)})
			Expect(err).ToNot(HaveOccurred())
		}
	}

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	commit := func(sessionID string, content []byte) {
		Expect(env.Fs.CommitUpload(env.Ctx, ref, sessionID, storage.UploadSource{
			Body:   io.NopCloser(bytes.NewReader(content)),
			Length: int64(len(content)),
		})).To(Succeed())
		Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, sessionID)).To(Succeed())
	}

	rollback := func(sessionID string) {
		Expect(env.Fs.RollbackUpload(env.Ctx, ref, sessionID, storage.RollbackInfo{NodeExisted: true})).To(Succeed())
		Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, sessionID)).To(Succeed())
	}

	current := func() *node.Node {
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		return n
	}

	blobID := func(n *node.Node) string {
		v, err := env.Lookup.MetadataBackend().Get(env.Ctx, n, prefixes.BlobIDAttr)
		Expect(err).ToNot(HaveOccurred())
		return string(v)
	}

	revisionContents := func() [][]byte {
		n := current()
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		var contents [][]byte
		for _, r := range revisions {
			if filepath.Ext(r) == ".mpk" || filepath.Ext(r) == ".mlock" {
				continue
			}
			c, err := os.ReadFile(r)
			Expect(err).ToNot(HaveOccurred())
			contents = append(contents, c)
		}
		return contents
	}

	Context("with versioning disabled", func() {
		BeforeEach(func() {
			setup(map[string]interface{}{"disable_versioning": true}, false)
		})

		It("hands the file back to A when B is rolled back, and A can still finish", func() {
			rollback("session-b")

			n := current()
			Expect(blobID(n)).To(Equal("session-a"))
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-a"))

			commit("session-a", contentA)

			n = current()
			Expect(os.ReadFile(n.InternalPath())).To(Equal(contentA))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
			Expect(revisionContents()).To(BeEmpty())
		})

		It("puts A's content on the file when A finished before B is rolled back", func() {
			commit("session-a", contentA)
			Expect(os.ReadFile(current().InternalPath())).To(Equal(oldContent), "A wrote into the revision B kept")

			rollback("session-b")

			n := current()
			Expect(os.ReadFile(n.InternalPath())).To(Equal(contentA))
			Expect(blobID(n)).To(Equal("session-a"))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
			Expect(revisionContents()).To(BeEmpty())
		})

		It("keeps only B when both finish", func() {
			commit("session-a", contentA)
			commit("session-b", contentB)

			n := current()
			Expect(os.ReadFile(n.InternalPath())).To(Equal(contentB))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
			Expect(revisionContents()).To(BeEmpty())
		})
	})

	Context("with versioning disabled and a version from before", func() {
		BeforeEach(func() {
			setup(map[string]interface{}{"disable_versioning": true}, true)
		})

		It("keeps that version when both finish", func() {
			commit("session-a", contentA)
			commit("session-b", contentB)

			Expect(os.ReadFile(current().InternalPath())).To(Equal(contentB))
			Expect(revisionContents()).To(Equal([][]byte{olderVersion}))
		})

		It("keeps that version when B is rolled back and A finishes", func() {
			rollback("session-b")
			commit("session-a", contentA)

			Expect(os.ReadFile(current().InternalPath())).To(Equal(contentA))
			Expect(revisionContents()).To(Equal([][]byte{olderVersion}))
		})
	})

	Context("with versioning enabled", func() {
		BeforeEach(func() {
			setup(nil, false)
		})

		It("keeps the old content and A as revisions when both finish", func() {
			commit("session-a", contentA)
			commit("session-b", contentB)

			n := current()
			Expect(os.ReadFile(n.InternalPath())).To(Equal(contentB))
			Expect(revisionContents()).To(ConsistOf(oldContent, contentA))
		})

		It("restores A and keeps the old content as a revision when B is rolled back", func() {
			commit("session-a", contentA)
			rollback("session-b")

			n := current()
			Expect(os.ReadFile(n.InternalPath())).To(Equal(contentA))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
			Expect(revisionContents()).To(Equal([][]byte{oldContent}))
		})
	})
})
