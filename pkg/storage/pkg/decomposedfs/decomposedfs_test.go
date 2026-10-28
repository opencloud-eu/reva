// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package decomposedfs_test

import (
	"context"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	nodemocks "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node/mocks"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Decomposed", func() {
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
			Path:       "/dir1",
		}
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	// mockPermissionsByName replaces the permissions mock. An existing node gets
	// the permissions listed for its name, or only Stat. A node that does not
	// exist yet gets the permissions listed for "".
	mockPermissionsByName := func(perms map[string]*provider.ResourcePermissions) {
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
			func(_ context.Context, n *node.Node) (*provider.ResourcePermissions, error) {
				name := n.Name
				if !n.Exists {
					name = ""
				}
				if p, ok := perms[name]; ok {
					return p, nil
				}
				return &provider.ResourcePermissions{Stat: true}, nil
			}, nil)
	}

	Describe("NewDefault", func() {
		It("works", func() {
			bs := &nodemocks.Blobstore{}
			_, err := decomposedfs.NewDefault(map[string]interface{}{
				"root":           env.Root,
				"permissionssvc": "any",
			}, bs, nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects the xattrs metadata backend", func() {
			bs := &nodemocks.Blobstore{}
			_, err := decomposedfs.NewDefault(map[string]interface{}{
				"root":             env.Root,
				"permissionssvc":   "any",
				"metadata_backend": "xattrs",
			}, bs, nil, nil)
			Expect(err).To(MatchError(ContainSubstring("xattrs")))
			Expect(err).To(MatchError(ContainSubstring("messagepack")))
		})
	})

	Describe("CreateDir", func() {
		Context("Existing and non existing parent folders", func() {
			It("CreateDir succeeds", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir2",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true, Stat: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).ToNot(HaveOccurred())
				ri, err := env.Fs.GetMD(env.Ctx, dir2, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(ri.Path).To(Equal(dir2.Path))
			})
			It("CreateDir succeeds in subdir", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir2",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true, Stat: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).ToNot(HaveOccurred())
				ri, err := env.Fs.GetMD(env.Ctx, dir2, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(ri.Path).To(Equal(dir2.Path))
			})
			It("dir already exists", func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, ref)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.AlreadyExists("/dir1")))
			})
			It("dir already exists in subdir", func() {
				dir3 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir3",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir3)
				Expect(err).ToNot(HaveOccurred())
				err = env.Fs.CreateDir(env.Ctx, dir3)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.AlreadyExists("/dir1/dir3")))
			})
			It("CreateDir fails in subdir", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir2/dir3",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.PreconditionFailed("/dir1/dir2")))
			})
			It("CreateDir fails in non existing sub subbdir", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir2/dir3/dir4",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.PreconditionFailed("error: not found: dir2")))
			})
		})
	})

	Describe("Move", func() {
		It("returns the resolved references and space owner", func() {
			destination := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir2",
			}

			result, err := env.Fs.Move(env.Ctx, ref, destination)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(proto.Equal(result.SpaceOwner, env.Owner.Id)).To(BeTrue())
			Expect(result.OldReference.Path).To(Equal("/dir1"))
			Expect(result.NewReference.Path).To(Equal("/dir2"))
			Expect(result.OldReference.ResourceId.SpaceId).To(Equal(env.SpaceRootRes.SpaceId))
			Expect(result.NewReference.ResourceId.SpaceId).To(Equal(env.SpaceRootRes.SpaceId))

			_, err = env.Fs.GetMD(env.Ctx, ref, nil, nil)
			Expect(err).To(HaveOccurred())
			_, err = env.Fs.GetMD(env.Ctx, destination, nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns no result when the source is missing", func() {
			source := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/missing",
			}

			result, err := env.Fs.Move(env.Ctx, source, ref)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("does not move the root of a share", func() {
			// the grant is on subdir1, its parent dir1 grants nothing
			mockPermissionsByName(map[string]*provider.ResourcePermissions{
				"subdir1": {Stat: true, Move: true},
				"":        {Stat: true, CreateContainer: true},
			})
			shareRoot := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"}
			destination := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir2"}

			result, err := env.Fs.Move(env.Ctx, shareRoot, destination)

			Expect(err).To(MatchError(ContainSubstring("permission denied")))
			Expect(result).To(BeNil())
			_, err = env.Fs.GetMD(env.Ctx, shareRoot, nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})

		It("moves a resource below the root of a share", func() {
			// the grant is on dir1, the space root grants nothing
			mockPermissionsByName(map[string]*provider.ResourcePermissions{
				"dir1":  {Stat: true, Move: true},
				"file1": {Stat: true, Move: true},
				"":      {Stat: true, InitiateFileUpload: true},
			})
			source := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/file1"}
			destination := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1/file1"}

			_, err := env.Fs.Move(env.Ctx, source, destination)

			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("TouchFile", func() {
		It("assembles the permissions on the parent when the file does not exist yet", func() {
			fileRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/newfile",
			}
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			// a call with the not-yet-existing node has no matching expectation and fails the test
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.MatchedBy(func(n *node.Node) bool {
				return n.Exists
			})).Return(&provider.ResourcePermissions{InitiateFileUpload: true, Stat: true}, nil)

			Expect(env.Fs.TouchFile(env.Ctx, fileRef, false, "")).To(Succeed())

			env.Permissions.AssertCalled(GinkgoT(), "AssemblePermissions", mock.Anything, mock.MatchedBy(func(n *node.Node) bool {
				return n.Name == "dir1"
			}))
		})
	})

	Describe("Delete", func() {
		Context("with no permissions", func() {
			It("returns an error", func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{}, nil)

				err := env.Fs.Delete(env.Ctx, ref)

				Expect(err).To(MatchError(ContainSubstring("not found")))
			})
		})

		Context("with insufficient permissions", func() {
			It("returns an error", func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:   true,
					Delete: false,
				}, nil)

				err := env.Fs.Delete(env.Ctx, ref)

				Expect(err).To(MatchError(ContainSubstring("permission denied")))
			})
		})

		Context("on the root of a share", func() {
			It("returns an error", func() {
				// the grant is on subdir1, its parent dir1 grants nothing
				mockPermissionsByName(map[string]*provider.ResourcePermissions{
					"subdir1": {Stat: true, Delete: true},
				})
				shareRoot := &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"}

				err := env.Fs.Delete(env.Ctx, shareRoot)

				Expect(err).To(MatchError(ContainSubstring("permission denied")))
				_, err = env.Fs.GetMD(env.Ctx, shareRoot, nil, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("below the root of a share", func() {
			It("deletes the resource", func() {
				// the grant is on dir1, the space root grants nothing
				mockPermissionsByName(map[string]*provider.ResourcePermissions{
					"dir1":  {Stat: true, Delete: true},
					"file1": {Stat: true, Delete: true},
				})

				err := env.Fs.Delete(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/file1"})

				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("with sufficient permissions", func() {
			JustBeforeEach(func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:   true,
					Delete: true,
				}, nil)
			})

			It("does not (yet) delete the blob from the blobstore", func() {
				err := env.Fs.Delete(env.Ctx, ref)

				Expect(err).ToNot(HaveOccurred())
				env.Blobstore.AssertNotCalled(GinkgoT(), "Delete", mock.AnythingOfType("string"))
			})
		})
	})
})
