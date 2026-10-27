// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package posix_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	posixblobstore "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PrepareUpload on posix", func() {
	var (
		env *posixhelpers.TestEnv
		ref *provider.Reference
	)

	oldContent := []byte("old content")

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
			&provider.ResourcePermissions{Stat: true, InitiateFileUpload: true}, nil)

		ref = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./target.txt"}
		Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())

		// the file's content as it was before the upload
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(n.InternalPath(), oldContent, 0600)).To(Succeed())
		Expect(n.SetXattrsWithContext(env.Ctx, map[string][]byte{
			prefixes.BlobsizeAttr: []byte("11"),
		})).To(Succeed())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("snapshots the current content and keeps it in the live file until the commit", func() {
		result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{
			NodeExisted: true,
			Size:        20,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.VersionCreated).To(BeTrue())

		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())

		// the revision holds a copy of the content the upload replaces
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		Expect(revisions).To(HaveLen(1))
		Expect(os.ReadFile(revisions[0])).To(Equal(oldContent))

		// the live file describes the upload, but its bytes are only replaced by CommitUpload
		Expect(n.BlobID).To(Equal("session-1"))
		Expect(n.Blobsize).To(Equal(int64(20)))
		Expect(os.ReadFile(n.InternalPath())).To(Equal(oldContent))
	})

	Describe("CommitUpload", func() {
		newContent := []byte("new content, 20 byte")

		prepare := func(sessionID string) {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, sessionID, storage.UploadInfo{
				NodeExisted: true,
				Size:        int64(len(newContent)),
			})
			Expect(err).ToNot(HaveOccurred())
		}

		commit := func(sessionID string, source storage.UploadSource) {
			Expect(env.Fs.CommitUpload(env.Ctx, ref, sessionID, source)).To(Succeed())
		}

		bodySource := func() storage.UploadSource {
			return storage.UploadSource{
				Body:   io.NopCloser(bytes.NewReader(newContent)),
				Length: int64(len(newContent)),
			}
		}

		// the test env mocks the blobstore; let it write through the real posix blobstore
		BeforeEach(func() {
			bs, err := posixblobstore.New(env.Root)
			Expect(err).ToNot(HaveOccurred())
			env.Blobstore.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(bs.Upload).Maybe()
			env.Blobstore.On("UploadFromReader", mock.Anything, mock.Anything, mock.Anything).Return(bs.UploadFromReader).Maybe()
		})

		revisionContents := func(n *node.Node) [][]byte {
			revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
			Expect(err).ToNot(HaveOccurred())
			var contents [][]byte
			for _, r := range revisions {
				c, err := os.ReadFile(r)
				Expect(err).ToNot(HaveOccurred())
				contents = append(contents, c)
			}
			return contents
		}

		It("replaces the content of the file and keeps the upload's metadata", func() {
			prepare("session-1")
			commit("session-1", bodySource())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(n.InternalPath())).To(Equal(newContent))
			Expect(n.BlobID).To(Equal("session-1"))
			Expect(n.Blobsize).To(Equal(int64(len(newContent))))
			Expect(revisionContents(n)).To(Equal([][]byte{oldContent}))

			// nothing staged in the space root, where the watcher would pick it up
			Expect(filepath.Glob(filepath.Join(n.SpaceRoot.InternalPath(), ".upload-*"))).To(BeEmpty())
		})

		It("moves a staged file into place", func() {
			staged := filepath.Join(env.Root, "staged-session-1")
			Expect(os.WriteFile(staged, newContent, 0600)).To(Succeed())

			prepare("session-1")
			commit("session-1", storage.UploadSource{Path: staged, Length: int64(len(newContent))})

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(n.InternalPath())).To(Equal(newContent))
			Expect(n.BlobID).To(Equal("session-1"))
			_, err = os.Stat(staged)
			Expect(os.IsNotExist(err)).To(BeTrue(), "the staged file should have been moved")
		})

		It("writes into the revision when a newer upload took over the file", func() {
			prepare("session-a")
			prepare("session-b")
			source := bodySource()
			source.ScanResult, source.ScanDate = "clean", time.Now()
			commit("session-a", source)

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			// the file belongs to session-b now and still holds the old bytes until session-b commits
			Expect(n.BlobID).To(Equal("session-b"))
			Expect(os.ReadFile(n.InternalPath())).To(Equal(oldContent))
			// session-a's bytes went to the revision session-b created for it
			Expect(revisionContents(n)).To(ConsistOf(oldContent, newContent))

			// and the revision still describes session-a's upload, not the file
			revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
			Expect(err).ToNot(HaveOccurred())
			var blobIDs []string
			for _, r := range revisions {
				rev := node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+strings.TrimPrefix(r, env.Lookup.VersionPath(n.SpaceID, n.ID, "")), env.Lookup)
				blobID, err := env.Lookup.MetadataBackend().Get(env.Ctx, rev, prefixes.BlobIDAttr)
				Expect(err).ToNot(HaveOccurred())
				blobIDs = append(blobIDs, string(blobID))
			}
			Expect(blobIDs).To(ContainElement("session-a"))
			Expect(blobIDs).ToNot(ContainElement("session-b"))
		})
	})

	Describe("a new file nothing has created yet", func() {
		var (
			placeholder string
			createRef   *provider.Reference
			info        storage.UploadInfo
		)

		BeforeEach(func() {
			placeholder = uuid.New().String()
			createRef = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: env.SpaceRootRes.SpaceId, OpaqueId: placeholder}}
			info = storage.UploadInfo{
				NodeExisted: false,
				Size:        42,
				ParentID:    env.SpaceRootRes.OpaqueId,
				Name:        "new.txt",
			}
		})

		byName := func() *node.Node {
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./new.txt"})
			Expect(err).ToNot(HaveOccurred())
			return n
		}

		It("creates the file under the placeholder id, marked as processing", func() {
			result, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.SizeDiff).To(Equal(int64(42)))
			Expect(result.SpaceOwner).ToNot(BeNil())

			n := byName()
			Expect(n.Exists).To(BeTrue())
			Expect(n.ID).To(Equal(placeholder))
			Expect(n.InternalPath()).To(Equal(filepath.Join(env.Root, "users", env.Owner.Username, "new.txt")))
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-new"))
		})

		It("leaves a file that took the name meanwhile alone", func() {
			Expect(env.Fs.TouchFile(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./new.txt"}, false, "")).To(Succeed())
			other := byName()

			_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).To(BeAssignableToTypeOf(errtypes.AlreadyExists("")))
			Expect(err.Error()).ToNot(ContainSubstring(env.Root), "the error should not reveal the storage path")

			kept := byName()
			Expect(kept.ID).To(Equal(other.ID))
			path, err := env.Lookup.IDCache.Get(env.Ctx, env.SpaceRootRes.SpaceId, placeholder)
			Expect(err).To(HaveOccurred(), "the losing upload's id should not be cached, got %q", path)
		})

		It("removes the file and its cache entry when the quota is exceeded", func() {
			original := node.CheckQuota
			node.CheckQuota = func(context.Context, *node.Node, bool, uint64, uint64) (bool, error) {
				return false, errtypes.InsufficientStorage("quota exceeded")
			}
			defer func() { node.CheckQuota = original }()

			_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).To(BeAssignableToTypeOf(errtypes.InsufficientStorage("")))

			Expect(byName().Exists).To(BeFalse())
			_, err = os.Stat(filepath.Join(env.Root, "users", env.Owner.Username, "new.txt"))
			Expect(os.IsNotExist(err)).To(BeTrue(), "the file was left on disk")
			_, err = env.Lookup.IDCache.Get(env.Ctx, env.SpaceRootRes.SpaceId, placeholder)
			Expect(err).To(HaveOccurred(), "the id was left in the cache")
		})
	})

	It("restores the previous metadata and keeps the file on rollback", func() {
		before, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())

		result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{
			NodeExisted: true,
			Size:        20,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.SizeDiff).To(Equal(int64(9)))

		Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-1")).To(Succeed())
		Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-1", storage.RollbackInfo{NodeExisted: true, SizeDiff: result.SizeDiff})).To(Succeed())

		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.BlobID).To(Equal(before.BlobID))
		Expect(n.Blobsize).To(Equal(int64(11)))
		Expect(os.ReadFile(n.InternalPath())).To(Equal(oldContent))

		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		Expect(revisions).To(BeEmpty())
	})

	It("removes a file the upload created on rollback", func() {
		newRef := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./new.txt"}
		Expect(env.Fs.TouchFile(env.Ctx, newRef, false, "")).To(Succeed())
		created, err := env.Lookup.NodeFromResource(env.Ctx, newRef)
		Expect(err).ToNot(HaveOccurred())

		result, err := env.Fs.PrepareUpload(env.Ctx, newRef, "session-new", storage.UploadInfo{
			NodeExisted: false,
			Size:        20,
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(env.Fs.MarkProcessing(env.Ctx, newRef, true, "session-new")).To(Succeed())
		Expect(env.Fs.RollbackUpload(env.Ctx, newRef, "session-new", storage.RollbackInfo{NodeExisted: false, SizeDiff: result.SizeDiff})).To(Succeed())

		_, err = os.Stat(created.InternalPath())
		Expect(os.IsNotExist(err)).To(BeTrue(), "the file created by the upload should be gone")
		n, err := env.Lookup.NodeFromResource(env.Ctx, newRef)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.Exists).To(BeFalse())
	})

	It("removes a file whose metadata became unreadable on rollback", func() {
		newRef := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./orphan.txt"}
		Expect(env.Fs.TouchFile(env.Ctx, newRef, false, "")).To(Succeed())
		created, err := env.Lookup.NodeFromResource(env.Ctx, newRef)
		Expect(err).ToNot(HaveOccurred())
		rootBefore, err := env.Fs.GetMD(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes}, nil, nil)
		Expect(err).ToNot(HaveOccurred())

		result, err := env.Fs.PrepareUpload(env.Ctx, newRef, "session-orphan", storage.UploadInfo{
			NodeExisted: false,
			Size:        20,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(env.Fs.MarkProcessing(env.Ctx, newRef, true, "session-orphan")).To(Succeed())

		Expect(env.Lookup.MetadataBackend().Purge(env.Ctx, created)).To(Succeed())
		idRef := &provider.Reference{ResourceId: &provider.ResourceId{
			StorageId: env.SpaceRootRes.GetStorageId(),
			SpaceId:   env.SpaceRootRes.GetSpaceId(),
			OpaqueId:  created.ID,
		}}
		_, err = env.Lookup.NodeFromResource(env.Ctx, idRef)
		Expect(err).To(HaveOccurred(), "the node should be unreadable")
		Expect(env.Fs.IsOrphaned(env.Ctx, idRef)).To(BeTrue())

		Expect(env.Fs.RollbackUpload(env.Ctx, idRef, "session-orphan", storage.RollbackInfo{
			SizeDiff: result.SizeDiff,
			NodeID:   created.ID,
			ParentID: created.ParentID,
			Filename: "orphan.txt",
			Size:     20,
		})).To(Succeed())

		_, err = os.Stat(created.InternalPath())
		Expect(os.IsNotExist(err)).To(BeTrue(), "the orphaned file should be gone")
		rootAfter, err := env.Fs.GetMD(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes}, nil, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(rootAfter.Size).To(Equal(rootBefore.Size), "the quota should be released")
	})

	It("leaves no revision behind when a precondition fails", func() {
		_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{
			NodeExisted: true,
			Size:        20,
			IfMatch:     "wrong-etag",
		})
		Expect(err).To(HaveOccurred())

		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		Expect(revisions).To(BeEmpty())
	})
})
