package decomposedfs_test

import (
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
)

var _ = Describe("Storage event SpaceOwner propagation", func() {
	var (
		env *helpers.DecomposedTestEnv
	)

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	Describe("CreateDir", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					CreateContainer: true,
					Stat:            true,
				}, nil)

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.CreateDir(ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/events-test-createdir",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})

	Describe("TouchFile", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					InitiateFileUpload: true,
					Stat:               true,
				}, nil)

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.TouchFile(ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/events-touch-file.txt",
			}, false, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})

	Describe("SetLock", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					InitiateFileUpload: true,
					Stat:               true,
				}, nil)

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.SetLock(ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			}, &provider.Lock{
				Type:   provider.LockType_LOCK_TYPE_EXCL,
				LockId: "events-test-lock-id",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})

	Describe("Unlock", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					InitiateFileUpload: true,
					Stat:               true,
				}, nil)

			ref := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			}
			lock := &provider.Lock{
				Type:   provider.LockType_LOCK_TYPE_EXCL,
				LockId: "events-unlock-test-lock",
			}

			_, err := env.Fs.SetLock(env.Ctx, ref, lock)
			Expect(err).ToNot(HaveOccurred())

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.Unlock(ctx, ref, lock)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})

	Describe("RestoreRecycleItem", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			registerPermissions(env.Permissions, helpers.OwnerID, &provider.ResourcePermissions{
				Stat:               true,
				InitiateFileUpload: true,
				Delete:             true,
				ListRecycle:        true,
				RestoreRecycleItem: true,
			})

			err := env.Fs.Delete(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			})
			Expect(err).ToNot(HaveOccurred())

			items, err := env.Fs.ListRecycle(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes}, "", "")
			Expect(err).ToNot(HaveOccurred())
			Expect(len(items)).To(BeNumerically(">=", 1))

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.RestoreRecycleItem(ctx, &provider.Reference{ResourceId: env.SpaceRootRes}, items[0].Key, "", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})

	Describe("RestoreRevision", func() {
		It("returns a WriteResult with SpaceOwner and propagates it via context slot", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					Stat:               true,
					InitiateFileUpload: true,
					ListFileVersions:   true,
					RestoreFileVersion: true,
				}, nil)

			fileRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			}

			n, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
			Expect(err).ToNot(HaveOccurred())
			_, err = env.Tree.CreateRevision(env.Ctx, n, "2024-01-01T00:00:00Z")
			Expect(err).ToNot(HaveOccurred())
			env.Blobstore.On("Delete", mock.Anything).Return(nil).Maybe()

			ctx := storagespace.ContextRegisterSpaceOwnerSlot(env.Ctx)

			res, err := env.Fs.RestoreRevision(ctx, fileRef, n.ID+node.RevisionIDDelimiter+"2024-01-01T00:00:00Z")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.SpaceOwner).ToNot(BeNil())

			storagespace.ContextSetSpaceOwner(ctx, res.SpaceOwner)

			Expect(storagespace.ContextGetSpaceOwner(ctx)).To(Equal(res.SpaceOwner))
		})
	})
})
