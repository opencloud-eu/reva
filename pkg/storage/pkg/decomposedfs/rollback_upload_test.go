// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("RollbackUpload", func() {
	var (
		env  *helpers.DecomposedTestEnv
		ref  *provider.Reference
		opts map[string]interface{}
	)

	BeforeEach(func() {
		opts = nil
	})

	JustBeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(opts)
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
			Return(&provider.ResourcePermissions{
				InitiateFileUpload: true,
				Stat:               true,
				ListFileVersions:   true,
			}, nil)

		ref = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/rollback-target.txt",
		}
	})

	// the tree's ListRevisions bypasses the permissions mock, so look at the disk
	revisionsOf := func() []string {
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		var paths []string
		for _, r := range revisions {
			if filepath.Ext(r) != ".mpk" && filepath.Ext(r) != ".mlock" {
				paths = append(paths, r)
			}
		}
		return paths
	}

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	Context("versioning enabled, existing node, session owns node", func() {
		It("restores previous version, removes version file, reverts size propagation", func() {
			err := env.Fs.TouchFile(env.Ctx, ref, false, "")
			Expect(err).ToNot(HaveOccurred())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-init", storage.UploadInfo{
				NodeExisted: false,
				Size:        10,
			})
			Expect(err).ToNot(HaveOccurred())

			parentInfoBefore, err := env.Fs.GetMD(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"}, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			sizeBefore := parentInfoBefore.Size

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-overwrite", storage.UploadInfo{
				NodeExisted: true,
				Size:        30,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.VersionCreated).To(BeTrue())
			Expect(result.SizeDiff).To(Equal(int64(20))) // 30 - 10

			Expect(revisionsOf()).To(HaveLen(1))

			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-overwrite")).To(Succeed())
			Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-overwrite", storage.RollbackInfo{NodeExisted: true, SizeDiff: result.SizeDiff})).To(Succeed())

			Expect(revisionsOf()).To(BeEmpty())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			blobID, err := n.Xattr(env.Ctx, prefixes.BlobIDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(blobID)).To(Equal("session-init"))

			parentInfoAfter, err := env.Fs.GetMD(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"}, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(parentInfoAfter.Size).To(Equal(sizeBefore))
		})
	})

	Context("versioning disabled, existing node, session owns node", func() {
		BeforeEach(func() {
			opts = map[string]interface{}{"disable_versioning": true}
		})

		It("keeps the node and reports that there is no revision to restore", func() {
			err := env.Fs.TouchFile(env.Ctx, ref, false, "")
			Expect(err).ToNot(HaveOccurred())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-init", storage.UploadInfo{
				NodeExisted: false,
				Size:        10,
			})
			Expect(err).ToNot(HaveOccurred())
			// the first upload has finished
			Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, "session-init")).To(Succeed())

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-overwrite", storage.UploadInfo{
				NodeExisted: true,
				Size:        30,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.VersionCreated).To(BeFalse())

			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-overwrite")).To(Succeed())
			err = env.Fs.RollbackUpload(env.Ctx, ref, "session-overwrite", storage.RollbackInfo{NodeExisted: true, SizeDiff: result.SizeDiff})
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsPreconditionFailed)
			Expect(ok).To(BeTrue(), "expected errtypes.PreconditionFailed, got %T: %v", err, err)

			// upstream purges the node here
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeTrue())
		})
	})

	Context("session guard: another session owns the node", func() {
		It("is a no-op and does not touch the node", func() {
			err := env.Fs.TouchFile(env.Ctx, ref, false, "")
			Expect(err).ToNot(HaveOccurred())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-a", storage.UploadInfo{
				NodeExisted: false,
				Size:        10,
			})
			Expect(err).ToNot(HaveOccurred())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-b", storage.UploadInfo{
				NodeExisted: true,
				Size:        20,
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-b")).To(Succeed())
			Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-a", storage.RollbackInfo{NodeExisted: true, SizeDiff: 10})).To(Succeed())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			blobID, err := n.Xattr(env.Ctx, prefixes.BlobIDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(blobID)).To(Equal("session-b"))
		})
	})

	Context("new node created by the upload, session owns node", func() {
		It("purges the node and reverts size propagation", func() {
			parentRef := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"}
			parentInfoBefore, err := env.Fs.GetMD(env.Ctx, parentRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())

			Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())
			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-new", storage.UploadInfo{
				NodeExisted: false,
				Size:        10,
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-new")).To(Succeed())
			Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-new", storage.RollbackInfo{NodeExisted: false, SizeDiff: result.SizeDiff})).To(Succeed())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeFalse())

			parentInfoAfter, err := env.Fs.GetMD(env.Ctx, parentRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(parentInfoAfter.Size).To(Equal(parentInfoBefore.Size))
		})
	})

	Context("new node (NodeExists=false)", func() {
		It("returns nil without error", func() {
			missingRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/does-not-exist.txt",
			}
			Expect(env.Fs.RollbackUpload(env.Ctx, missingRef, "any-session-id", storage.RollbackInfo{NodeExisted: false, SizeDiff: 0})).To(Succeed())
		})
	})

	// Two uploads to the same file overlap: B prepares while A is still processing.
	// Whatever happens to B, A must be able to finish.
	Context("a newer upload supersedes one still processing, versioning disabled", func() {
		BeforeEach(func() {
			opts = map[string]interface{}{"disable_versioning": true}
		})

		JustBeforeEach(func() {
			env.Blobstore.On("UploadFromReader", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			env.Blobstore.On("Delete", mock.Anything).Return(nil)

			Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-init", storage.UploadInfo{Size: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, "session-init")).To(Succeed())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-a", storage.UploadInfo{NodeExisted: true, Size: 20})
			Expect(err).ToNot(HaveOccurred())
			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-b", storage.UploadInfo{NodeExisted: true, Size: 30})
			Expect(err).ToNot(HaveOccurred())
		})

		commit := func(sessionID string, size int64) {
			Expect(env.Fs.CommitUpload(env.Ctx, ref, sessionID, storage.UploadSource{
				Body:   io.NopCloser(bytes.NewReader(make([]byte, size))),
				Length: size,
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

		It("hands the node back to A when B is rolled back, and A can still finish", func() {
			rollback("session-b")

			n := current()
			Expect(n.BlobID).To(Equal("session-a"))
			Expect(n.Blobsize).To(Equal(int64(20)))
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-a"), "A's processing mark should be restored")
			Expect(revisionsOf()).To(BeEmpty())

			commit("session-a", 20)
			n = current()
			Expect(n.BlobID).To(Equal("session-a"))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
		})

		It("keeps A when A finished before B is rolled back", func() {
			commit("session-a", 20)
			Expect(current().BlobID).To(Equal("session-b"), "A wrote into the revision B kept")

			rollback("session-b")

			n := current()
			Expect(n.BlobID).To(Equal("session-a"))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse(), "A is done, its mark must not come back")
			Expect(revisionsOf()).To(BeEmpty())
		})

		It("keeps only B when both finish", func() {
			commit("session-a", 20)
			commit("session-b", 30)

			n := current()
			Expect(n.BlobID).To(Equal("session-b"))
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse())
			Expect(revisionsOf()).To(BeEmpty(), "versioning is disabled")
		})

		It("drops A's revision when A is rolled back while B is processing", func() {
			rollback("session-a")
			Expect(revisionsOf()).To(BeEmpty())

			commit("session-b", 30)
			Expect(current().BlobID).To(Equal("session-b"))
		})
	})

	// A node whose metadata is gone but whose file remains, e.g. because an
	// ancestor was trashed while the upload was in flight. Reading the node fails,
	// so the rollback has only the session to go on.
	Context("orphaned node: the metadata is unreadable", func() {
		var (
			idRef    *provider.Reference
			nodePath string
			dir1Ref  *provider.Reference
			info     storage.RollbackInfo
		)

		parentSize := func() uint64 {
			parentInfo, err := env.Fs.GetMD(env.Ctx, dir1Ref, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			return parentInfo.Size
		}

		// orphan prepares an upload, then purges the target's metadata. It returns
		// the parent size from before the optimistic propagation.
		orphan := func() uint64 {
			dir1Ref = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"}
			sizeBefore := parentSize()

			Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			nodePath = n.InternalPath()
			// address the node by id: the path walk needs the metadata the purge destroys
			idRef = &provider.Reference{ResourceId: &provider.ResourceId{
				StorageId: env.SpaceRootRes.GetStorageId(),
				SpaceId:   env.SpaceRootRes.GetSpaceId(),
				OpaqueId:  n.ID,
			}}

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-orphan", storage.UploadInfo{
				NodeExisted: false,
				Size:        30,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-orphan")).To(Succeed())
			Expect(parentSize()).To(Equal(sizeBefore+30), "the optimistic size should be propagated")

			Expect(env.Lookup.MetadataBackend().Purge(env.Ctx, n)).To(Succeed())
			_, err = env.Lookup.NodeFromResource(env.Ctx, idRef)
			Expect(err).To(HaveOccurred(), "the node should be unreadable")
			Expect(env.Fs.IsOrphaned(env.Ctx, idRef)).To(BeTrue())

			info = storage.RollbackInfo{
				SizeDiff: result.SizeDiff,
				NodeID:   n.ID,
				ParentID: n.ParentID,
				Filename: "rollback-target.txt",
				Size:     30,
			}
			return sizeBefore
		}

		It("releases the quota and removes the unreachable node", func() {
			sizeBefore := orphan()

			Expect(env.Fs.RollbackUpload(env.Ctx, idRef, "session-orphan", info)).To(Succeed())

			Expect(parentSize()).To(Equal(sizeBefore))
			_, err := os.Stat(nodePath)
			Expect(err).To(HaveOccurred(), "the orphaned node should be gone")
			children, err := env.Fs.ListFolder(env.Ctx, dir1Ref, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			for _, c := range children {
				Expect(c.GetPath()).ToNot(HaveSuffix("rollback-target.txt"), "the parent should not list the orphan")
			}
		})

		It("reports the lookup failure when the session carries no ids", func() {
			orphan()

			err := env.Fs.RollbackUpload(env.Ctx, idRef, "session-orphan", storage.RollbackInfo{SizeDiff: 30})

			Expect(err).To(MatchError(ContainSubstring("node lookup failed")))
			_, statErr := os.Stat(nodePath)
			Expect(statErr).ToNot(HaveOccurred(), "the node should be left for a retry")
		})

		It("keeps the node when the quota cannot be released", func() {
			orphan()
			// a parent id pointing nowhere fails the walk up the tree
			info.ParentID = "does-not-exist"

			err := env.Fs.RollbackUpload(env.Ctx, idRef, "session-orphan", info)

			Expect(err).To(MatchError(ContainSubstring("could not revert propagate")))
			_, statErr := os.Stat(nodePath)
			Expect(statErr).ToNot(HaveOccurred(), "the node should be left for a retry")
		})
	})
})
