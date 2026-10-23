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
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/stretchr/testify/mock"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/testhelpers"
	treemocks "github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/tree/mocks"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Decomposed", func() {
	var (
		env *helpers.TestEnv

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

	Describe("NewDefault", func() {
		It("works", func() {
			bs := &treemocks.Blobstore{}
			_, err := decomposedfs.NewDefault(map[string]interface{}{
				"root":           env.Root,
				"permissionssvc": "any",
			}, bs, nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("CreateDir", func() {
		Context("Existing and non existing parent folders", func() {
			It("CreateDir succeeds", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir2",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true, Stat: true}, nil)
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
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true, Stat: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).ToNot(HaveOccurred())
				ri, err := env.Fs.GetMD(env.Ctx, dir2, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(ri.Path).To(Equal(dir2.Path))
			})
			It("dir already exists", func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, ref)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.AlreadyExists("/dir1")))
			})
			It("dir already exists in subdir", func() {
				dir3 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir3",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
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
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.PreconditionFailed("/dir1/dir2")))
			})
			It("CreateDir fails in non existing sub subbdir", func() {
				dir2 := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/dir2/dir3/dir4",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{CreateContainer: true}, nil)
				err := env.Fs.CreateDir(env.Ctx, dir2)
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(errtypes.PreconditionFailed("error: not found: dir2")))
			})
		})
	})

	Describe("TouchFile", func() {
		It("assembles the permissions on the parent when the file does not exist yet", func() {
			fileRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/newfile",
			}
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
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{}, nil)

				err := env.Fs.Delete(env.Ctx, ref)

				Expect(err).To(MatchError(ContainSubstring("not found")))
			})
		})

		Context("with insufficient permissions", func() {
			It("returns an error", func() {
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:   true,
					Delete: false,
				}, nil)

				err := env.Fs.Delete(env.Ctx, ref)

				Expect(err).To(MatchError(ContainSubstring("permission denied")))
			})
		})

		Context("with sufficient permissions", func() {
			JustBeforeEach(func() {
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

		Context("on a node marked as processing", func() {
			It("rejects with ResourceProcessing", func() {
				fileRef := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/file1",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:   true,
					Delete: true,
				}, nil)

				Expect(env.Fs.MarkProcessing(env.Ctx, fileRef, true, "test-session")).To(Succeed())

				err := env.Fs.Delete(env.Ctx, fileRef)

				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsResourceProcessing)
				Expect(ok).To(BeTrue(), "expected errtypes.ResourceProcessing, got %T: %v", err, err)
			})
		})
	})

	Describe("Move", func() {
		Context("on a source node marked as processing", func() {
			It("rejects with ResourceProcessing", func() {
				oldRef := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/file1",
				}
				newRef := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/file1-renamed",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:               true,
					Move:               true,
					InitiateFileUpload: true,
				}, nil)

				Expect(env.Fs.MarkProcessing(env.Ctx, oldRef, true, "test-session")).To(Succeed())

				_, err := env.Fs.Move(env.Ctx, oldRef, newRef)

				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsResourceProcessing)
				Expect(ok).To(BeTrue(), "expected errtypes.ResourceProcessing, got %T: %v", err, err)
			})
		})
	})

	Describe("Download", func() {
		Context("on a node marked as processing", func() {
			It("rejects with ResourceProcessing", func() {
				fileRef := &provider.Reference{
					ResourceId: env.SpaceRootRes,
					Path:       "/dir1/file1",
				}
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:                 true,
					InitiateFileDownload: true,
				}, nil)

				Expect(env.Fs.MarkProcessing(env.Ctx, fileRef, true, "test-session")).To(Succeed())

				_, _, err := env.Fs.Download(env.Ctx, fileRef, func(*provider.ResourceInfo) bool { return false })

				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsResourceProcessing)
				Expect(ok).To(BeTrue(), "expected errtypes.ResourceProcessing, got %T: %v", err, err)
			})
		})
	})
})
