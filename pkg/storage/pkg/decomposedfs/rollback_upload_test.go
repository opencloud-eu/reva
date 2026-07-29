package decomposedfs_test

import (
	"path/filepath"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
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
			Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-overwrite", true, result.SizeDiff)).To(Succeed())

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

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-overwrite", storage.UploadInfo{
				NodeExisted: true,
				Size:        30,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.VersionCreated).To(BeFalse())

			Expect(env.Fs.MarkProcessing(env.Ctx, ref, true, "session-overwrite")).To(Succeed())
			err = env.Fs.RollbackUpload(env.Ctx, ref, "session-overwrite", true, result.SizeDiff)
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
			Expect(env.Fs.RollbackUpload(env.Ctx, ref, "session-a", true, 10)).To(Succeed())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			blobID, err := n.Xattr(env.Ctx, prefixes.BlobIDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(blobID)).To(Equal("session-b"))
		})
	})

	Context("new node (NodeExists=false)", func() {
		It("returns nil without error", func() {
			missingRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/does-not-exist.txt",
			}
			Expect(env.Fs.RollbackUpload(env.Ctx, missingRef, "any-session-id", false, 0)).To(Succeed())
		})
	})
})
