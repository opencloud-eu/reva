// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("PrepareUpload", func() {
	var (
		env *helpers.DecomposedTestEnv
		ref *provider.Reference
	)

	JustBeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		ref = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/upload-target.txt",
		}
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	touchTarget := func() {
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
			Return(&provider.ResourcePermissions{
				InitiateFileUpload: true,
				Stat:               true,
				ListFileVersions:   true,
			}, nil)
		err := env.Fs.TouchFile(env.Ctx, ref, false, "")
		Expect(err).ToNot(HaveOccurred())
	}

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

	Context("new file nothing has created yet", func() {
		var (
			perms       *provider.ResourcePermissions
			placeholder string
			createRef   *provider.Reference
			info        storage.UploadInfo
			parent      *node.Node
		)

		BeforeEach(func() {
			perms = &provider.ResourcePermissions{InitiateFileUpload: true, Stat: true}
		})

		JustBeforeEach(func() {
			// replace the env's catch-all permissions
			env.Permissions.ExpectedCalls = nil
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(perms, nil)

			var err error
			parent, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(parent.Exists).To(BeTrue())

			placeholder = uuid.New().String()
			createRef = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: env.SpaceRootRes.SpaceId, OpaqueId: placeholder}}
			info = storage.UploadInfo{
				NodeExisted: false,
				Size:        42,
				ParentID:    parent.ID,
				Name:        "upload-target.txt",
			}
		})

		byID := func() *node.Node {
			n, err := env.Lookup.NodeFromID(env.Ctx, createRef.ResourceId)
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		byName := func() *node.Node {
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		expectNothingCreated := func() {
			Expect(byID().Exists).To(BeFalse(), "a node was left under the placeholder id")
			Expect(byName().Exists).To(BeFalse(), "a file was left listed in its folder")
		}

		It("creates the node under the placeholder id with all its metadata", func() {
			result, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.SizeDiff).To(Equal(int64(42)))
			Expect(result.VersionCreated).To(BeFalse())
			Expect(result.SpaceOwner.GetOpaqueId()).To(Equal(env.Owner.GetId().GetOpaqueId()))

			n := byID()
			Expect(n.Exists).To(BeTrue())
			Expect(n.ParentID).To(Equal(parent.ID))
			Expect(n.Name).To(Equal("upload-target.txt"))
			Expect(n.BlobID).To(Equal("session-new"))
			Expect(n.Blobsize).To(Equal(int64(42)))
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-new"))

			Expect(byName().ID).To(Equal(placeholder))
		})

		It("checks the quota once", func() {
			calls := 0
			original := node.CheckQuota
			node.CheckQuota = func(ctx context.Context, spaceRoot *node.Node, overwrite bool, oldSize, newSize uint64) (bool, error) {
				calls++
				return original(ctx, spaceRoot, overwrite, oldSize, newSize)
			}
			defer func() { node.CheckQuota = original }()

			_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).ToNot(HaveOccurred())
			Expect(calls).To(Equal(1))
		})

		It("leaves a missing node NotFound when not told where it goes", func() {
			info.ParentID, info.Name = "", ""

			_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
			expectNothingCreated()
		})

		Context("when the parent went away", func() {
			It("returns NotFound and creates nothing", func() {
				info.ParentID = uuid.New().String()

				_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
				expectNothingCreated()
			})
		})

		Context("when the user may no longer upload into the parent", func() {
			BeforeEach(func() {
				perms = &provider.ResourcePermissions{Stat: true}
			})

			It("returns PermissionDenied and creates nothing", func() {
				_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.PermissionDenied("")))
				expectNothingCreated()
			})
		})

		Context("when the user can no longer see the parent", func() {
			BeforeEach(func() {
				perms = &provider.ResourcePermissions{}
			})

			It("returns NotFound and creates nothing", func() {
				_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
				expectNothingCreated()
			})
		})

		Context("when another upload took the name meanwhile", func() {
			It("returns AlreadyExists and leaves the other file alone", func() {
				err := env.Fs.TouchFile(env.Ctx, ref, false, "")
				Expect(err).ToNot(HaveOccurred())
				other := byName()

				_, err = env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.AlreadyExists("")))

				Expect(byID().Exists).To(BeFalse())
				kept := byName()
				Expect(kept.Exists).To(BeTrue())
				Expect(kept.ID).To(Equal(other.ID))
			})
		})

		Context("when the quota is exceeded", func() {
			It("returns InsufficientStorage and creates nothing", func() {
				original := node.CheckQuota
				node.CheckQuota = func(context.Context, *node.Node, bool, uint64, uint64) (bool, error) {
					return false, errtypes.InsufficientStorage("quota exceeded")
				}
				defer func() { node.CheckQuota = original }()

				_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.InsufficientStorage("")))
				expectNothingCreated()
			})
		})

		// A new node cannot hold a lock, so a lock id is refused before anything is made.
		Context("when the request carries a lock id", func() {
			It("returns Aborted without creating the node", func() {
				created := false
				original := node.CheckQuota
				node.CheckQuota = func(ctx context.Context, spaceRoot *node.Node, overwrite bool, oldSize, newSize uint64) (bool, error) {
					created = true // InitNewNode checks the quota once it has made the node file
					return original(ctx, spaceRoot, overwrite, oldSize, newSize)
				}
				defer func() { node.CheckQuota = original }()
				ctx := ctxpkg.ContextSetLockID(env.Ctx, "a-lock-the-new-file-cannot-have")

				_, err := env.Fs.PrepareUpload(ctx, createRef, "session-new", info)
				Expect(err).To(BeAssignableToTypeOf(errtypes.Aborted("")))
				Expect(created).To(BeFalse(), "the node was created before the lock id was refused")
				expectNothingCreated()
			})
		})

		// The parent-folder update runs after the node and its metadata are written.
		Context("when it fails after creating the node", func() {
			It("purges the node it created", func() {
				// A directory where the parent's lock file goes fails the propagation.
				lockPath := env.Lookup.MetadataBackend().LockfilePath(parent)
				Expect(os.RemoveAll(lockPath)).To(Succeed())
				Expect(os.Mkdir(lockPath, 0700)).To(Succeed())

				_, err := env.Fs.PrepareUpload(env.Ctx, createRef, "session-new", info)
				Expect(err).To(MatchError(ContainSubstring("could not propagate")))
				expectNothingCreated()
				_, err = os.Stat(env.Lookup.InternalPath(env.SpaceRootRes.SpaceId, placeholder))
				Expect(os.IsNotExist(err)).To(BeTrue(), "the node file was left on disk")
			})
		})
	})
	Context("when the node does not exist on disk", func() {
		It("returns NotFound", func() {
			info := storage.UploadInfo{NodeExisted: false, Size: 100}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", info)
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsNotFound)
			Expect(ok).To(BeTrue(), "expected errtypes.NotFound, got %T: %v", err, err)
		})
	})

	Context("new file (NodeExisted=false)", func() {
		JustBeforeEach(touchTarget)

		It("writes xattrs and returns VersionCreated=false", func() {
			info := storage.UploadInfo{
				NodeExisted: false,
				Size:        42,
				Checksums: storage.UploadChecksums{
					SHA1:    []byte("sha1val"),
					MD5:     []byte("md5val"),
					Adler32: []byte("adler32val"),
				},
			}

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-new", info)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.VersionCreated).To(BeFalse())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			blobID, err := n.Xattr(env.Ctx, prefixes.BlobIDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(blobID)).To(Equal("session-new"))
		})

		// RollbackUpload and the unmark only act on the session that marked the node.
		It("marks the node as processing for the session", func() {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-new", storage.UploadInfo{NodeExisted: false, Size: 42})
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-new"))
		})

		It("reports the space owner", func() {
			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-new", storage.UploadInfo{NodeExisted: false, Size: 42})

			Expect(err).ToNot(HaveOccurred())
			Expect(result.SpaceOwner.GetOpaqueId()).To(Equal(env.Owner.GetId().GetOpaqueId()))
		})
	})

	Context("overwrite with versioning enabled (default)", func() {
		JustBeforeEach(touchTarget)

		It("creates a version file and returns VersionCreated=true", func() {
			// First PrepareUpload establishes initial metadata on the node.
			info1 := storage.UploadInfo{
				NodeExisted: false,
				Size:        10,
			}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", info1)
			Expect(err).ToNot(HaveOccurred())

			// Second call: overwrite.
			info2 := storage.UploadInfo{
				NodeExisted: true,
				Size:        20,
			}
			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-2", info2)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.VersionCreated).To(BeTrue())

			Expect(revisionsOf()).To(HaveLen(1))
		})

		It("marks the node as processing for the new session", func() {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{NodeExisted: false, Size: 10})
			Expect(err).ToNot(HaveOccurred())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-2", storage.UploadInfo{NodeExisted: true, Size: 20})
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-2"))
		})

		// the mark is written with the metadata, ahead of the parent-folder update, and
		// the coordinator discards the session on failure: nothing else would unmark it
		It("does not leave the node marked when the parent-folder update fails", func() {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{NodeExisted: false, Size: 10})
			Expect(err).ToNot(HaveOccurred())
			Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, "session-1")).To(Succeed())
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			parent, err := n.Parent(env.Ctx)
			Expect(err).ToNot(HaveOccurred())

			// a directory where the parent's lock file goes fails the propagation
			lockPath := env.Lookup.MetadataBackend().LockfilePath(parent)
			Expect(os.RemoveAll(lockPath)).To(Succeed())
			Expect(os.Mkdir(lockPath, 0700)).To(Succeed())

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-2", storage.UploadInfo{NodeExisted: true, Size: 20})
			Expect(err).To(MatchError(ContainSubstring("could not propagate")))

			n, err = env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.IsProcessing(env.Ctx)).To(BeFalse(), "the failed upload left the file marked as processing")
		})

		// the coordinator already resolved an existing file's owner at initiate
		It("leaves the space owner to the coordinator", func() {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{NodeExisted: false, Size: 10})
			Expect(err).ToNot(HaveOccurred())

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-2", storage.UploadInfo{NodeExisted: true, Size: 20})

			Expect(err).ToNot(HaveOccurred())
			Expect(result.SpaceOwner).To(BeNil())
		})
	})

	Context("overwrite with versioning disabled", func() {
		JustBeforeEach(func() {
			var err error
			env, err = helpers.NewTestEnv(map[string]interface{}{
				"disable_versioning": true,
			})
			Expect(err).ToNot(HaveOccurred())

			ref = &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/upload-target.txt",
			}

			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					InitiateFileUpload: true,
					Stat:               true,
					ListFileVersions:   true,
				}, nil)
			err = env.Fs.TouchFile(env.Ctx, ref, false, "")
			Expect(err).ToNot(HaveOccurred())
		})

		It("does not create a version file and returns VersionCreated=false", func() {
			// Lay down initial metadata.
			info1 := storage.UploadInfo{NodeExisted: false, Size: 10}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", info1)
			Expect(err).ToNot(HaveOccurred())
			// the first upload has finished
			Expect(env.Fs.MarkProcessing(env.Ctx, ref, false, "session-1")).To(Succeed())

			// Overwrite.
			info2 := storage.UploadInfo{NodeExisted: true, Size: 20}
			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-2", info2)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.VersionCreated).To(BeFalse())

			Expect(revisionsOf()).To(BeEmpty())
		})

		It("snapshots a node another upload is still processing, with its mark", func() {
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{NodeExisted: false, Size: 10})
			Expect(err).ToNot(HaveOccurred())

			result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-2", storage.UploadInfo{NodeExisted: true, Size: 20})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.VersionCreated).To(BeTrue())

			revisions := revisionsOf()
			Expect(revisions).To(HaveLen(1))
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			revisionNode := node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+strings.TrimPrefix(revisions[0], env.Lookup.VersionPath(n.SpaceID, n.ID, "")), env.Lookup)
			mark, err := env.Lookup.MetadataBackend().Get(env.Ctx, revisionNode, prefixes.StatusPrefix)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(mark)).To(Equal(node.ProcessingStatus + "session-1"))
			blobID, err := env.Lookup.MetadataBackend().Get(env.Ctx, revisionNode, prefixes.BlobIDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(blobID)).To(Equal("session-1"))
		})
	})

	Context("quota exceeded on overwrite", func() {
		JustBeforeEach(touchTarget)

		It("returns an error when the new size would exceed quota", func() {
			// Lay down initial metadata.
			info1 := storage.UploadInfo{NodeExisted: false, Size: 5}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", info1)
			Expect(err).ToNot(HaveOccurred())

			original := node.CheckQuota
			node.CheckQuota = func(_ context.Context, _ *node.Node, _ bool, _, _ uint64) (bool, error) {
				return false, errtypes.InsufficientStorage("quota exceeded")
			}
			defer func() { node.CheckQuota = original }()

			info := storage.UploadInfo{NodeExisted: true, Size: 20}
			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-2", info)
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsInsufficientStorage)
			Expect(ok).To(BeTrue(), "expected errtypes.InsufficientStorage, got %T: %v", err, err)

			// only a node the upload created may be purged, never a file that already had content
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeTrue())
			id, err := n.ProcessingID(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-1"), "the rejected upload must not take the node over")
		})
	})

	Context("quota exceeded on a new file", func() {
		JustBeforeEach(touchTarget)

		It("returns an error and reports the upload as an addition", func() {
			var (
				called       bool
				gotOverwrite bool
				gotOldSize   uint64
			)
			original := node.CheckQuota
			node.CheckQuota = func(_ context.Context, _ *node.Node, overwrite bool, oldSize, _ uint64) (bool, error) {
				called, gotOverwrite, gotOldSize = true, overwrite, oldSize
				return false, errtypes.InsufficientStorage("quota exceeded")
			}
			defer func() { node.CheckQuota = original }()

			info := storage.UploadInfo{NodeExisted: false, Size: 20}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-new", info)

			Expect(called).To(BeTrue(), "quota was not checked for a new file")
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsInsufficientStorage)
			Expect(ok).To(BeTrue(), "expected errtypes.InsufficientStorage, got %T: %v", err, err)

			// there are no bytes to replace, so the size must count as pure growth
			Expect(gotOverwrite).To(BeFalse())
			Expect(gotOldSize).To(BeZero())
		})

		// the node is not marked yet, so RollbackUpload would not recognise it as the
		// upload's and the user would be left with an empty file
		It("purges the node it was asked to fill", func() {
			touched, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(touched.Exists).To(BeTrue())

			original := node.CheckQuota
			node.CheckQuota = func(_ context.Context, _ *node.Node, _ bool, _, _ uint64) (bool, error) {
				return false, errtypes.InsufficientStorage("quota exceeded")
			}
			defer func() { node.CheckQuota = original }()

			_, err = env.Fs.PrepareUpload(env.Ctx, ref, "session-new", storage.UploadInfo{NodeExisted: false, Size: 20})
			Expect(err).To(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeFalse(), "the empty file is still listed in its folder")
			_, err = os.Stat(touched.InternalPath())
			Expect(os.IsNotExist(err)).To(BeTrue(), "the node file was left on disk")
			_, err = os.Stat(env.Lookup.MetadataBackend().MetadataPath(touched))
			Expect(os.IsNotExist(err)).To(BeTrue(), "the node metadata was left on disk")
		})
	})

	Context("precondition checks on overwrite", func() {
		var (
			oldEtag string
			oldTime time.Time
		)

		JustBeforeEach(func() {
			touchTarget()

			// Lay down initial metadata so the node has a valid etag/mtime.
			info1 := storage.UploadInfo{NodeExisted: false, Size: 5}
			_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-init", info1)
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			oldTime, err = n.GetMTime(env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			oldEtag, err = node.CalculateEtag(n.ID, oldTime)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("IfMatch mismatch", func() {
			It("returns Aborted", func() {
				info := storage.UploadInfo{
					NodeExisted: true,
					Size:        5,
					IfMatch:     "wrong-etag",
				}
				_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-x", info)
				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsAborted)
				Expect(ok).To(BeTrue(), "expected errtypes.Aborted, got %T: %v", err, err)
			})

			// the rejected session must not take the node over from the one that holds it
			It("leaves the processing mark alone", func() {
				_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-x", storage.UploadInfo{
					NodeExisted: true,
					Size:        5,
					IfMatch:     "wrong-etag",
				})
				Expect(err).To(HaveOccurred())

				n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
				Expect(err).ToNot(HaveOccurred())
				id, err := n.ProcessingID(env.Ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(id).To(Equal("session-init"))
			})
		})

		Context("IfMatch match", func() {
			It("proceeds normally", func() {
				info := storage.UploadInfo{
					NodeExisted: true,
					Size:        5,
					IfMatch:     oldEtag,
				}
				result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-match", info)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})

		Context("IfNoneMatch=* on existing node", func() {
			It("returns Aborted", func() {
				info := storage.UploadInfo{
					NodeExisted: true,
					Size:        5,
					IfNoneMatch: "*",
				}
				_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-x", info)
				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsAborted)
				Expect(ok).To(BeTrue(), "expected errtypes.Aborted, got %T: %v", err, err)
			})
		})

		Context("IfUnmodifiedSince violated", func() {
			It("returns Aborted when the node was modified after the given time", func() {
				before := oldTime.Add(-time.Second)
				info := storage.UploadInfo{
					NodeExisted:       true,
					Size:              5,
					IfUnmodifiedSince: before,
				}
				_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-x", info)
				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsAborted)
				Expect(ok).To(BeTrue(), "expected errtypes.Aborted, got %T: %v", err, err)
			})
		})
	})
})
