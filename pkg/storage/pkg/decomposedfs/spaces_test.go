// Copyright 2018-2022 CERN
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
	"os"
	"path/filepath"

	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/internal/grpc/services/storageprovider"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/spaceidindex"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

var _ = Describe("Spaces", func() {
	Describe("Create Space", func() {
		var (
			env *helpers.DecomposedTestEnv
		)
		BeforeEach(func() {
			var err error
			env, err = helpers.NewTestEnv(nil)
			Expect(err).ToNot(HaveOccurred())
			env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(
				func(ctx context.Context, in *cs3permissions.CheckPermissionRequest, opts ...grpc.CallOption) *cs3permissions.CheckPermissionResponse {
					if in.Permission == "Drives.DeletePersonal" && ctxpkg.ContextMustGetUser(ctx).Id.GetOpaqueId() == env.DeleteHomeSpacesUser.Id.OpaqueId {
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
					}
					if in.Permission == "Drives.DeleteProject" && ctxpkg.ContextMustGetUser(ctx).Id.GetOpaqueId() == env.DeleteAllSpacesUser.Id.OpaqueId {
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
					}
					if in.Permission == "Drives.ReadWriteEnabled" && ctxpkg.ContextMustGetUser(ctx).Id.GetOpaqueId() == env.DeleteAllSpacesUser.Id.OpaqueId {
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
					}
					if (in.Permission == "Drives.Create" || in.Permission == "Drives.List") && ctxpkg.ContextMustGetUser(ctx).Id.GetOpaqueId() == helpers.OwnerID {
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
					}
					// any other user
					return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_PERMISSION_DENIED}}
				},
				nil)
		})

		AfterEach(func() {
			if env != nil {
				env.Cleanup()
			}
		})

		Context("during login", func() {
			It("space is created", func() {
				resp, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resp)).To(Equal(1))
				Expect(string(resp[0].Opaque.GetMap()["spaceAlias"].Value)).To(Equal("personal/username"))
				Expect(resp[0].Name).To(Equal("username"))
				Expect(resp[0].SpaceType).To(Equal("personal"))
			})
		})
		Context("when creating a space", func() {
			It("project space is created", func() {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Mission to Mars", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				Expect(resp.StorageSpace).ToNot(Equal(nil))
				Expect(string(resp.StorageSpace.Opaque.Map["spaceAlias"].Value)).To(Equal("project/mission-to-mars"))
				Expect(resp.StorageSpace.Name).To(Equal("Mission to Mars"))
				Expect(resp.StorageSpace.SpaceType).To(Equal("project"))
				Expect(resp.StorageSpace.Opaque.Map).ToNot(HaveKey("contentType"))
			})
			It("stores and returns the content type", func() {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{
					Name: "Mission to Mars",
					Type: "project",
					Opaque: &typesv1beta1.Opaque{
						Map: map[string]*typesv1beta1.OpaqueEntry{
							"contentType": {
								Decoder: "plain",
								Value:   []byte("text-editor"),
							},
						},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				Expect(string(resp.StorageSpace.Opaque.Map["contentType"].Value)).To(Equal("text-editor"))
			})
		})

		Context("needs to check node permissions", func() {
			It("returns false on requesting for other user with canlistallspaces und no unrestricted privilege", func() {
				resp := env.Fs.MustCheckNodePermissions(env.Ctx, false)
				Expect(resp).To(Equal(true))
			})
			It("returns true on requesting unrestricted as non-admin", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[0])
				resp := env.Fs.MustCheckNodePermissions(ctx, true)
				Expect(resp).To(Equal(true))
			})
			It("returns true on requesting for own spaces", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[0])
				resp := env.Fs.MustCheckNodePermissions(ctx, false)
				Expect(resp).To(Equal(true))
			})
			It("returns false on unrestricted", func() {
				resp := env.Fs.MustCheckNodePermissions(env.Ctx, true)
				Expect(resp).To(Equal(false))
			})
		})

		Context("can delete homespace", func() {
			It("fails on trying to delete a homespace as non-admin", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[1])
				resp, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resp)).To(Equal(1))
				Expect(string(resp[0].Opaque.GetMap()["spaceAlias"].Value)).To(Equal("personal/username"))
				Expect(resp[0].Name).To(Equal("username"))
				Expect(resp[0].SpaceType).To(Equal("personal"))
				err = env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{
					Id: resp[0].GetId(),
				})
				Expect(err).To(HaveOccurred())
			})
			It("succeeds on trying to delete homespace as user with 'delete-all-home-spaces' permission", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteHomeSpacesUser)
				resp, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resp)).To(Equal(1))
				Expect(string(resp[0].Opaque.GetMap()["spaceAlias"].Value)).To(Equal("personal/username"))
				Expect(resp[0].Name).To(Equal("username"))
				Expect(resp[0].SpaceType).To(Equal("personal"))
				err = env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{
					Id: resp[0].GetId(),
				})
				Expect(err).To(Not(HaveOccurred()))
			})
			It("fails on trying to delete homespace as user with 'delete-all-spaces' permission", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				resp, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resp)).To(Equal(1))
				Expect(string(resp[0].Opaque.GetMap()["spaceAlias"].Value)).To(Equal("personal/username"))
				Expect(resp[0].Name).To(Equal("username"))
				Expect(resp[0].SpaceType).To(Equal("personal"))
				err = env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{
					Id: resp[0].GetId(),
				})
				Expect(err).To(HaveOccurred())
			})
		})

		Context("can delete (purge) project spaces", func() {
			var delReq *provider.DeleteStorageSpaceRequest
			BeforeEach(func() {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Mission to Venus", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				Expect(resp.StorageSpace).ToNot(Equal(nil))
				spaceID := resp.StorageSpace.GetId()
				err = env.Fs.DeleteStorageSpace(env.Ctx, &provider.DeleteStorageSpaceRequest{
					Id: spaceID,
				})
				Expect(err).To(Not(HaveOccurred()))
				delReq = &provider.DeleteStorageSpaceRequest{
					Opaque: &typesv1beta1.Opaque{
						Map: map[string]*typesv1beta1.OpaqueEntry{
							"purge": {
								Decoder: "plain",
								Value:   []byte("true"),
							},
						},
					},
					Id: spaceID,
				}
			})
			It("fails as a unprivileged user", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[1])
				err := env.Fs.DeleteStorageSpace(ctx, delReq)
				Expect(err).To(HaveOccurred())
			})
			It("fails as a user with 'delete-all-home-spaces privilege", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteHomeSpacesUser)
				err := env.Fs.DeleteStorageSpace(ctx, delReq)
				Expect(err).To(HaveOccurred())
			})
			It("returns not found when a non-member disables an active space", func() {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Hidden", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[1])
				err = env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{Id: resp.StorageSpace.GetId()})
				Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
			})
			It("keeps permission denied when a non-member purges a disabled space", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.Users[1])
				err := env.Fs.DeleteStorageSpace(ctx, delReq)
				Expect(err).To(BeAssignableToTypeOf(errtypes.PermissionDenied("")))
			})
			It("succeeds as a user with 'delete-all-spaces privilege", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				err := env.Fs.DeleteStorageSpace(ctx, delReq)
				Expect(err).To(Not(HaveOccurred()))
			})
			It("fails as a space manager", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.SpaceManager)
				err := env.Fs.DeleteStorageSpace(ctx, delReq)
				Expect(err).To(HaveOccurred())
			})
			It("succeeds at disabling as a space manager", func() {
				// The positive counterpart to the purge denial above: since #672
				// a space manager may still disable (but not purge) a project
				// space. delReq's space is pre-disabled in BeforeEach, so use a
				// fresh one.
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Manager Disables", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				ctx := ctxpkg.ContextSetUser(context.Background(), env.SpaceManager)
				err = env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{Id: resp.StorageSpace.GetId()})
				Expect(err).To(Not(HaveOccurred()))
			})
			// Reproducer for opencloud-eu/opencloud#2985: purging a space must
			// invalidate ALL of its msgpack indexes, not only the by-type index.
			// The by-user-id index keeps a stale entry pointing at the purged
			// space, which is exactly the reporter's "stale references in
			// indexes/by-user-id/*.mpk".
			It("removes the space from the by-user-id index", func() {
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				_, spaceID, _, err := storagespace.SplitID(delReq.Id.GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())

				// load via a fresh index instance each time: spaceidindex caches by
				// mtime, and the purge rewrites the file within the same mtime tick.
				loadOwnerIndex := func() map[string]string {
					idx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-user-id")
					Expect(idx.Init()).To(Succeed())
					m, lerr := idx.Load(helpers.OwnerID)
					Expect(lerr).ToNot(HaveOccurred())
					return m
				}

				// precondition: the owner's by-user-id index references the space
				Expect(loadOwnerIndex()).To(HaveKey(spaceID))

				// purge as the space admin
				Expect(env.Fs.DeleteStorageSpace(ctx, delReq)).To(Succeed())

				// the by-user-id index entry must be gone after the purge
				Expect(loadOwnerIndex()).ToNot(HaveKey(spaceID))
			})
			// Reproducer for the grantee side of opencloud-eu/opencloud#2985:
			// the purge must also remove the space from a user grantee's
			// by-user-id index and a group grantee's by-group-id index.
			It("removes the space from the by-user-id and by-group indexes for grantees", func() {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Shared Space", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				sid := resp.StorageSpace.GetId()
				_, spaceID, _, err := storagespace.SplitID(sid.GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())
				ref := &provider.Reference{ResourceId: resp.StorageSpace.GetRoot()}

				userGrantee := helpers.User0ID
				groupGrantee := "11111111-2222-3333-4444-555555555555"
				guestGrantee := "Guest@Example.COM"
				Expect(env.Fs.AddGrant(env.Ctx, ref, &provider.Grant{
					Grantee:     &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: &userv1beta1.UserId{OpaqueId: userGrantee}}},
					Permissions: &provider.ResourcePermissions{Stat: true},
					Creator:     &userv1beta1.UserId{OpaqueId: helpers.OwnerID},
				})).To(Succeed())
				Expect(env.Fs.AddGrant(env.Ctx, ref, &provider.Grant{
					Grantee:     &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_GROUP, Id: &provider.Grantee_GroupId{GroupId: &grouppb.GroupId{OpaqueId: groupGrantee}}},
					Permissions: &provider.ResourcePermissions{Stat: true},
					Creator:     &userv1beta1.UserId{OpaqueId: helpers.OwnerID},
				})).To(Succeed())
				Expect(env.Fs.AddGrant(env.Ctx, ref, &provider.Grant{
					Grantee:     &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: &userv1beta1.UserId{OpaqueId: guestGrantee, Type: userv1beta1.UserType_USER_TYPE_GUEST}}},
					Permissions: &provider.ResourcePermissions{Stat: true},
					Creator:     &userv1beta1.UserId{OpaqueId: helpers.OwnerID},
				})).To(Succeed())

				// seed the grantee space-index entries. The real share flow writes
				// these via a space-typed context; here we add them directly so the
				// purge has grantee index entries to remove.
				target := "../../../spaces/" + spaceID
				userIdx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-user-id")
				Expect(userIdx.Init()).To(Succeed())
				Expect(userIdx.Add(userGrantee, spaceID, target)).To(Succeed())
				groupIdx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-group-id")
				Expect(groupIdx.Init()).To(Succeed())
				Expect(groupIdx.Add(groupGrantee, spaceID, target)).To(Succeed())
				mailIdx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-mail")
				Expect(mailIdx.Init()).To(Succeed())
				guestFilename := utils.NewFSSafeUserID(&userv1beta1.UserId{OpaqueId: guestGrantee, Type: userv1beta1.UserType_USER_TYPE_GUEST}).SafeFilename()
				Expect(mailIdx.Add(guestFilename, spaceID, target)).To(Succeed())

				load := func(indexName, key string) map[string]string {
					idx := spaceidindex.New(filepath.Join(env.Root, "indexes"), indexName)
					Expect(idx.Init()).To(Succeed())
					m, lerr := idx.Load(key)
					Expect(lerr).ToNot(HaveOccurred())
					return m
				}

				// precondition: the grantee indexes reference the space
				Expect(load("by-user-id", userGrantee)).To(HaveKey(spaceID))
				Expect(load("by-group-id", groupGrantee)).To(HaveKey(spaceID))
				Expect(load("by-mail", guestFilename)).To(HaveKey(spaceID))

				// disable, then purge as the space admin
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				Expect(env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{Id: sid})).To(Succeed())
				Expect(env.Fs.DeleteStorageSpace(ctx, &provider.DeleteStorageSpaceRequest{
					Opaque: &typesv1beta1.Opaque{Map: map[string]*typesv1beta1.OpaqueEntry{"purge": {Decoder: "plain", Value: []byte("true")}}},
					Id:     sid,
				})).To(Succeed())

				// the grantee index entries must be gone after the purge
				Expect(load("by-user-id", userGrantee)).ToNot(HaveKey(spaceID))
				Expect(load("by-group-id", groupGrantee)).ToNot(HaveKey(spaceID))
				Expect(load("by-mail", guestFilename)).ToNot(HaveKey(spaceID))
			})
			// https://github.com/opencloud-eu/opencloud/issues/1865
			It("deletes the blobs of the nodes in the space", func() {
				_, spaceID, _, err := storagespace.SplitID(delReq.Id.GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())
				_, err = env.CreateTestFile("file1", "file1-blobid", spaceID, spaceID, 1)
				Expect(err).ToNot(HaveOccurred())
				env.Blobstore.On("Delete", mock.AnythingOfType("*node.Node")).Return(nil)

				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				Expect(env.Fs.DeleteStorageSpace(ctx, delReq)).To(Succeed())

				env.Blobstore.AssertCalled(GinkgoT(), "Delete", mock.MatchedBy(func(n *node.Node) bool {
					return n.BlobID == "file1-blobid" && n.SpaceID == spaceID
				}))
			})
			It("skips entries it cannot handle when deleting the blobs", func() {
				_, spaceID, _, err := storagespace.SplitID(delReq.Id.GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())
				_, err = env.CreateTestFile("backup.mpk", "backup-blobid", spaceID, spaceID, 1)
				Expect(err).ToNot(HaveOccurred())
				nodesDir := filepath.Join(env.Root, "spaces", lookup.Pathify(spaceID, 1, 2), "nodes")
				Expect(os.WriteFile(filepath.Join(nodesDir, "broken.mpk"), []byte("not msgpack"), 0600)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(nodesDir, "empty.mpk"), nil, 0600)).To(Succeed())
				env.Blobstore.On("Delete", mock.AnythingOfType("*node.Node")).Return(nil)

				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				Expect(env.Fs.DeleteStorageSpace(ctx, delReq)).To(Succeed())

				env.Blobstore.AssertCalled(GinkgoT(), "Delete", mock.MatchedBy(func(n *node.Node) bool {
					return n.BlobID == "backup-blobid"
				}))
			})
			// purging one space must remove only that space's entry: a sibling
			// space of the same owner must stay in the by-user-id index.
			It("keeps a sibling space in the by-user-id index when another is purged", func() {
				_, purgedID, _, err := storagespace.SplitID(delReq.Id.GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Sibling Space", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				_, siblingID, _, err := storagespace.SplitID(resp.StorageSpace.GetId().GetOpaqueId())
				Expect(err).ToNot(HaveOccurred())
				loadOwnerIndex := func() map[string]string {
					idx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-user-id")
					Expect(idx.Init()).To(Succeed())
					m, lerr := idx.Load(helpers.OwnerID)
					Expect(lerr).ToNot(HaveOccurred())
					return m
				}
				Expect(loadOwnerIndex()).To(HaveKey(purgedID))
				Expect(loadOwnerIndex()).To(HaveKey(siblingID))
				ctx := ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser)
				Expect(env.Fs.DeleteStorageSpace(ctx, delReq)).To(Succeed())
				after := loadOwnerIndex()
				Expect(after).ToNot(HaveKey(purgedID))
				Expect(after).To(HaveKey(siblingID))
			})
		})
	})

	Describe("List spaces with personal space permissions", func() {
		var (
			env          *helpers.DecomposedTestEnv
			spaceadmin   *userv1beta1.User
			admin        *userv1beta1.User
			adminSpace   *provider.StorageSpace
			projectSpace *provider.StorageSpace
		)

		BeforeEach(func() {
			var err error
			env, err = helpers.NewTestEnv(nil)
			Expect(err).ToNot(HaveOccurred())

			spaceadmin = env.Users[0]
			admin = env.Users[1]

			// space permissions granted to the users by their role
			grantedPermissions := func(user *userv1beta1.User) []string {
				switch user.GetId().GetOpaqueId() {
				case spaceadmin.GetId().GetOpaqueId():
					return []string{"Drives.Create", "Drives.List", "Drives.ReadWrite"}
				case admin.GetId().GetOpaqueId():
					return []string{"Drives.Create", "Drives.List", "Drives.ListPersonal", "Drives.ReadWrite", "Drives.ReadWritePersonal"}
				case env.Owner.GetId().GetOpaqueId():
					return []string{"Drives.Create"} // setup actor
				case env.DeleteAllSpacesUser.GetId().GetOpaqueId():
					return []string{"Drives.ListPersonal"} // custom role without Drives.List
				}
				return []string{}
			}
			env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(
				func(ctx context.Context, in *cs3permissions.CheckPermissionRequest, opts ...grpc.CallOption) *cs3permissions.CheckPermissionResponse {
					for _, permission := range grantedPermissions(ctxpkg.ContextMustGetUser(ctx)) {
						if permission == in.Permission {
							return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
						}
					}
					return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_PERMISSION_DENIED}}
				},
				nil)

			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
				func(ctx context.Context, n *node.Node) *provider.ResourcePermissions {
					u := ctxpkg.ContextMustGetUser(ctx)
					switch u.GetId().GetOpaqueId() {
					case env.Owner.GetId().GetOpaqueId():
						return node.OwnerPermissions() // setup actor
					case spaceadmin.GetId().GetOpaqueId(), admin.GetId().GetOpaqueId():
						if n.GetSpaceID() == u.GetId().GetOpaqueId() {
							return node.OwnerPermissions() // own personal space
						}
					}
					return node.NoPermissions()
				}, nil)

			createSpace := func(req *provider.CreateStorageSpaceRequest) *provider.StorageSpace {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
				return resp.GetStorageSpace()
			}
			// personal spaces of the other users, in addition to the owner's own one
			createSpace(&provider.CreateStorageSpaceRequest{Owner: spaceadmin, Name: "spaceadmin", Type: "personal"})
			adminSpace = createSpace(&provider.CreateStorageSpaceRequest{Owner: admin, Name: "admin", Type: "personal"})
			projectSpace = createSpace(&provider.CreateStorageSpaceRequest{Name: "Project A", Type: "project"})
			createSpace(&provider.CreateStorageSpaceRequest{Name: "Project B", Type: "project"})
		})

		AfterEach(func() {
			if env != nil {
				env.Cleanup()
			}
		})

		spaceNames := func(spaces []*provider.StorageSpace) []string {
			names := make([]string, 0, len(spaces))
			for _, space := range spaces {
				names = append(names, space.GetName())
			}
			return names
		}

		It("lists all project spaces but only the own personal space without Drives.ListPersonal", func() {
			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), spaceadmin), nil, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(ConsistOf("spaceadmin", "Project A", "Project B"))
		})

		It("lists all spaces with Drives.ListPersonal", func() {
			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), admin), nil, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(ConsistOf("username", "spaceadmin", "admin", "Project A", "Project B"))
		})

		It("only lists the own personal space when filtering by the personal space type", func() {
			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), spaceadmin), []*provider.ListStorageSpacesRequest_Filter{
				{Type: provider.ListStorageSpacesRequest_Filter_TYPE_SPACE_TYPE, Term: &provider.ListStorageSpacesRequest_Filter_SpaceType{SpaceType: "personal"}},
			}, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(ConsistOf("spaceadmin"))
			Expect(spaces[0].GetSpaceType()).To(Equal("personal"))
		})

		It("only lists the own spaces when listing restricted", func() {
			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), spaceadmin), nil, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(ConsistOf("spaceadmin"))
		})

		It("only lists the personal spaces of another user with Drives.ListPersonal", func() {
			// the requested user's groups would normally be resolved via the gateway
			Expect(env.Fs.UserCache.Set(admin.GetId().GetOpaqueId(), admin)).To(Succeed())

			spaces, err := env.Fs.ListStorageSpaces(
				ctxpkg.ContextSetUser(context.Background(), env.DeleteAllSpacesUser),
				[]*provider.ListStorageSpacesRequest_Filter{
					{Type: provider.ListStorageSpacesRequest_Filter_TYPE_USER, Term: &provider.ListStorageSpacesRequest_Filter_User{User: admin.GetId()}},
				},
				true,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(ConsistOf("admin"))
		})

		It("does not list personal spaces without a space type when the user lacks Drives.ListPersonal", func() {
			// simulate a space root without a type attribute: the space is still
			// reachable through the user space index, but it cannot be classified
			// and is therefore rejected
			n, err := node.ReadNode(
				context.Background(),
				env.Lookup,
				adminSpace.GetRoot().GetSpaceId(),
				adminSpace.GetRoot().GetOpaqueId(),
				"",
				false,
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.RemoveXattr(context.Background(), prefixes.SpaceTypeAttr)).To(Succeed())

			// the requested user's groups would normally be resolved via the gateway
			Expect(env.Fs.UserCache.Set(admin.GetId().GetOpaqueId(), admin)).To(Succeed())

			spaces, err := env.Fs.ListStorageSpaces(
				ctxpkg.ContextSetUser(context.Background(), spaceadmin),
				[]*provider.ListStorageSpacesRequest_Filter{
					{Type: provider.ListStorageSpacesRequest_Filter_TYPE_USER, Term: &provider.ListStorageSpacesRequest_Filter_User{User: admin.GetId()}},
				},
				true,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(spaceNames(spaces)).To(BeEmpty())

			// a direct lookup cannot classify the space either and has to fail
			_, err = env.Fs.ListStorageSpaces(
				ctxpkg.ContextSetUser(context.Background(), spaceadmin),
				[]*provider.ListStorageSpacesRequest_Filter{
					{Type: provider.ListStorageSpacesRequest_Filter_TYPE_ID, Term: &provider.ListStorageSpacesRequest_Filter_Id{Id: &provider.StorageSpaceId{OpaqueId: storagespace.FormatResourceID(adminSpace.GetRoot())}}},
				},
				true,
			)
			Expect(err).To(BeAssignableToTypeOf(errtypes.InternalError("")))
		})

		It("space admin can read the quota of a project space", func() {
			_, _, _, err := env.Fs.GetQuota(ctxpkg.ContextSetUser(context.Background(), spaceadmin), &provider.Reference{ResourceId: projectSpace.GetRoot()})
			Expect(err).ToNot(HaveOccurred())
		})

		It("space admin cannot read the quota of another user's personal space", func() {
			_, _, _, err := env.Fs.GetQuota(ctxpkg.ContextSetUser(context.Background(), spaceadmin), &provider.Reference{ResourceId: env.SpaceRootRes})
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
		})

		It("space admin cannot read the quota of a personal space without a space type", func() {
			n, err := node.ReadNode(
				context.Background(),
				env.Lookup,
				adminSpace.GetRoot().GetSpaceId(),
				adminSpace.GetRoot().GetOpaqueId(),
				"",
				false,
				nil,
				false,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.RemoveXattr(context.Background(), prefixes.SpaceTypeAttr)).To(Succeed())

			_, _, _, err = env.Fs.GetQuota(ctxpkg.ContextSetUser(context.Background(), spaceadmin), &provider.Reference{ResourceId: adminSpace.GetRoot()})
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
		})

		It("admin can read the quota of another user's personal space", func() {
			_, _, _, err := env.Fs.GetQuota(ctxpkg.ContextSetUser(context.Background(), admin), &provider.Reference{ResourceId: env.SpaceRootRes})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Create Space with custom alias template", func() {
		Describe("Create Spaces with custom alias template", func() {
			var (
				env *helpers.DecomposedTestEnv
			)

			BeforeEach(func() {
				var err error
				env, err = helpers.NewTestEnv(map[string]interface{}{
					"personalspacealias_template": "{{.SpaceType}}/{{.Email.Local}}@{{.Email.Domain}}",
					"generalspacealias_template":  "{{.SpaceType}}:{{.SpaceName | replace \" \" \"-\" | upper}}",
				})
				Expect(err).ToNot(HaveOccurred())
				env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}, nil)
				env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
					Stat:     true,
					AddGrant: true,
					GetQuota: true,
				}, nil)
			})

			AfterEach(func() {
				if env != nil {
					env.Cleanup()
				}
			})
			Context("during login", func() {
				It("personal space is created with custom alias", func() {
					resp, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(resp)).To(Equal(1))
					Expect(string(resp[0].Opaque.GetMap()["spaceAlias"].Value)).To(Equal("personal/username@_unknown"))
				})
			})
			Context("creating a space", func() {
				It("project space is created with custom alias", func() {
					resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Mission to Venus", Type: "project"})
					Expect(err).ToNot(HaveOccurred())
					Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
					Expect(resp.StorageSpace).ToNot(Equal(nil))
					Expect(string(resp.StorageSpace.Opaque.Map["spaceAlias"].Value)).To(Equal("project:MISSION-TO-VENUS"))

				})
			})
		})
	})

	Describe("Update Space", func() {
		var (
			env     *helpers.DecomposedTestEnv
			spaceid *provider.StorageSpaceId

			spaceadmin    *userv1beta1.User
			personalSpace *provider.StorageSpaceId

			manager  = &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "manager"}}
			editor   = &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "editor"}}
			viewer   = &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "viewer"}}
			nomember = &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "nomember"}}
			admin    = &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "admin"}}
		)
		BeforeEach(func() {
			var err error
			env, err = helpers.NewTestEnv(nil)
			Expect(err).ToNot(HaveOccurred())

			spaceadmin = env.Users[0]
			personalSpace = &provider.StorageSpaceId{OpaqueId: storagespace.FormatResourceID(env.SpaceRootRes)}

			// space permissions
			env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(
				func(ctx context.Context, in *cs3permissions.CheckPermissionRequest, opts ...grpc.CallOption) *cs3permissions.CheckPermissionResponse {
					switch ctxpkg.ContextMustGetUser(ctx).GetId().GetOpaqueId() {
					case manager.GetId().GetOpaqueId():
						switch in.Permission {
						case "Drives.Create":
							return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
						default:
							return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_PERMISSION_DENIED}}
						}
					case spaceadmin.GetId().GetOpaqueId():
						switch in.Permission {
						case "Drives.ReadWrite", "Drives.ReadWriteEnabled":
							return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
						default:
							return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_PERMISSION_DENIED}}
						}
					case admin.GetId().GetOpaqueId():
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}
					default:
						return &cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_PERMISSION_DENIED}}
					}
				}, nil)

			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
				func(ctx context.Context, n *node.Node) *provider.ResourcePermissions {
					switch ctxpkg.ContextMustGetUser(ctx).GetId().GetOpaqueId() {
					case manager.GetId().GetOpaqueId():
						return node.OwnerPermissions() // id of owner/admin
					case env.Owner.GetId().GetOpaqueId():
						return node.OwnerPermissions() // owner of the personal space
					case editor.GetId().GetOpaqueId():
						return &provider.ResourcePermissions{InitiateFileUpload: true} // mock editor
					case viewer.GetId().GetOpaqueId():
						return &provider.ResourcePermissions{Stat: true} // mock viewer
					default:
						return node.NoPermissions()
					}
				}, nil)

			resp, err := env.Fs.CreateStorageSpace(ctxpkg.ContextSetUser(context.Background(), manager), &provider.CreateStorageSpaceRequest{Name: "Mission to Venus", Type: "project"})
			Expect(err).ToNot(HaveOccurred())

			spaceid = resp.GetStorageSpace().GetId()
		})

		AfterEach(func() {
			if env != nil {
				env.Cleanup()
			}
		})

		disableSpace := func(u *userv1beta1.User, id *provider.StorageSpaceId) {
			err := env.Fs.DeleteStorageSpace(ctxpkg.ContextSetUser(context.Background(), u), &provider.DeleteStorageSpaceRequest{Id: id})
			Expect(err).ToNot(HaveOccurred())
		}

		DescribeTable("update project spaces",
			func(details func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest), expectedStatusCode rpcv1beta1.Code) {
				u, req := details()
				r, err := env.Fs.UpdateStorageSpace(ctxpkg.ContextSetUser(context.Background(), u), req)
				Expect(err).ToNot(HaveOccurred())
				Expect(r.Status.Code).To(Equal(expectedStatusCode))
			},

			Entry("Manager can change everything but quota",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return manager, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new space name",
							Id:   spaceid,
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"description": {
										Decoder: "plain",
										Value:   []byte("new description"),
									},
									"spacealias": {
										Decoder: "plain",
										Value:   []byte("new alias"),
									},
									"image": {
										Decoder: "plain",
										Value:   []byte("a$b!c"),
									},
									"readme": {
										Decoder: "plain",
										Value:   []byte("f$g!h"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("Manager cannot change quota, even with large request",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return manager, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name:  "new space name",
							Id:    spaceid,
							Quota: &provider.Quota{QuotaMaxBytes: uint64(1000)},
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"description": {
										Decoder: "plain",
										Value:   []byte("new description"),
									},
									"spacealias": {
										Decoder: "plain",
										Value:   []byte("new alias"),
									},
									"image": {
										Decoder: "plain",
										Value:   []byte("a$b!c"),
									},
									"readme": {
										Decoder: "plain",
										Value:   []byte("f$g!h"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("Editor cannot change quota",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return editor, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id:    spaceid,
							Quota: &provider.Quota{QuotaMaxBytes: uint64(1000)},
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("Editor cannot change name",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return editor, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new spacename",
							Id:   spaceid,
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("Editor cannot change contentType",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return editor, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id: spaceid,
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"contentType": {
										Decoder: "plain",
										Value:   []byte("text-editor"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("Manager can change contentType",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return manager, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id: spaceid,
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"contentType": {
										Decoder: "plain",
										Value:   []byte("text-editor"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("Admin can change quota",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return admin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id:    spaceid,
							Quota: &provider.Quota{QuotaMaxBytes: uint64(1000)},
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("Admin can change name and description",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return admin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new spacename",
							Id:   spaceid,
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"description": {
										Decoder: "plain",
										Value:   []byte("new description"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("Viewer gets OK when he changes nothing",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return viewer, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id: spaceid,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("NoMember will not find the space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return nomember, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id: spaceid,
						},
					}
				},
				rpcv1beta1.Code_CODE_NOT_FOUND,
			),
			Entry("SpaceAdmin can restore a project space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					disableSpace(manager, spaceid)
					return spaceadmin, &provider.UpdateStorageSpaceRequest{
						Opaque: &typesv1beta1.Opaque{
							Map: map[string]*typesv1beta1.OpaqueEntry{
								"restore": {
									Decoder: "plain",
									Value:   []byte("true"),
								},
							},
						},
						StorageSpace: &provider.StorageSpace{
							Id: spaceid,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
		)

		DescribeTable("update personal spaces",
			func(details func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest), expectedStatusCode rpcv1beta1.Code) {
				u, req := details()
				r, err := env.Fs.UpdateStorageSpace(ctxpkg.ContextSetUser(context.Background(), u), req)
				Expect(err).ToNot(HaveOccurred())
				Expect(r.Status.Code).To(Equal(expectedStatusCode))
			},

			Entry("SpaceAdmin cannot change the name of a personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return spaceadmin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new personal space name",
							Id:   personalSpace,
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("SpaceAdmin cannot change the description of a personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return spaceadmin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Id: personalSpace,
							Opaque: &typesv1beta1.Opaque{
								Map: map[string]*typesv1beta1.OpaqueEntry{
									"description": {
										Decoder: "plain",
										Value:   []byte("new description"),
									},
								},
							},
						},
					}
				},
				rpcv1beta1.Code_CODE_PERMISSION_DENIED,
			),
			Entry("Admin can change the name of a personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return admin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new personal space name",
							Id:   personalSpace,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("Owner can change the name of his personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return env.Owner, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new personal space name",
							Id:   personalSpace,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("SpaceAdmin can change the name of a project space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					return spaceadmin, &provider.UpdateStorageSpaceRequest{
						StorageSpace: &provider.StorageSpace{
							Name: "new space name",
							Id:   spaceid,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
			Entry("SpaceAdmin cannot restore a personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					disableSpace(admin, personalSpace)
					return spaceadmin, &provider.UpdateStorageSpaceRequest{
						Opaque: &typesv1beta1.Opaque{
							Map: map[string]*typesv1beta1.OpaqueEntry{
								"restore": {
									Decoder: "plain",
									Value:   []byte("true"),
								},
							},
						},
						StorageSpace: &provider.StorageSpace{
							Id: personalSpace,
						},
					}
				},
				rpcv1beta1.Code_CODE_NOT_FOUND,
			),
			Entry("Admin can restore a personal space",
				func() (*userv1beta1.User, *provider.UpdateStorageSpaceRequest) {
					disableSpace(admin, personalSpace)
					return admin, &provider.UpdateStorageSpaceRequest{
						Opaque: &typesv1beta1.Opaque{
							Map: map[string]*typesv1beta1.OpaqueEntry{
								"restore": {
									Decoder: "plain",
									Value:   []byte("true"),
								},
							},
						},
						StorageSpace: &provider.StorageSpace{
							Id: personalSpace,
						},
					}
				},
				rpcv1beta1.Code_CODE_OK,
			),
		)
	})

	Describe("List storage spaces for guests", func() {
		var (
			env *helpers.DecomposedTestEnv
		)

		BeforeEach(func() {
			var err error
			env, err = helpers.NewTestEnv(nil)
			Expect(err).ToNot(HaveOccurred())
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
				Stat:     true,
				AddGrant: true,
				GetQuota: true,
			}, nil)
			env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}, nil)
		})

		AfterEach(func() {
			if env != nil {
				env.Cleanup()
			}
		})

		It("finds project spaces granted to a guest via the by-mail index", func() {
			// create a project space as the owner
			resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Guest Space", Type: "project"})
			Expect(err).ToNot(HaveOccurred())
			ref := &provider.Reference{ResourceId: resp.StorageSpace.GetRoot()}

			// grant the space to a guest. A space-typed context makes the storage
			// link the space in the by-mail index, like the real share flow.
			guest := &userv1beta1.UserId{OpaqueId: "guest@example.com", Type: userv1beta1.UserType_USER_TYPE_GUEST}
			ctx := storageprovider.WithSpaceType(ctxpkg.ContextSetUser(context.Background(), env.Users[0]), "project")
			Expect(env.Fs.AddGrant(ctx, ref, &provider.Grant{
				Grantee:     &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: guest}},
				Permissions: &provider.ResourcePermissions{Stat: true},
				Creator:     &userv1beta1.UserId{OpaqueId: helpers.OwnerID},
			})).To(Succeed())

			// the guest can discover the granted space when listing their spaces
			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), &userv1beta1.User{Id: guest}), []*provider.ListStorageSpacesRequest_Filter{
				{Type: provider.ListStorageSpacesRequest_Filter_TYPE_USER, Term: &provider.ListStorageSpacesRequest_Filter_User{User: guest}},
			}, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(spaces)).To(Equal(1))
			Expect(spaces[0].SpaceType).To(Equal("project"))
			Expect(spaces[0].GetId().GetOpaqueId()).To(Equal(resp.StorageSpace.GetId().GetOpaqueId()))
		})

		It("safely indexes a project space granted to a guest with a path-like ID", func() {
			// create a project space as the owner
			resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Guest Space", Type: "project"})
			Expect(err).ToNot(HaveOccurred())
			ref := &provider.Reference{ResourceId: resp.StorageSpace.GetRoot()}

			ctx := storageprovider.WithSpaceType(ctxpkg.ContextSetUser(context.Background(), env.Users[0]), "project")
			guest := &userv1beta1.UserId{OpaqueId: "my/../email@email.com", Type: userv1beta1.UserType_USER_TYPE_GUEST}
			err = env.Fs.AddGrant(ctx, ref, &provider.Grant{
				Grantee:     &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: guest}},
				Permissions: &provider.ResourcePermissions{Stat: true},
				Creator:     &userv1beta1.UserId{OpaqueId: helpers.OwnerID},
			})
			Expect(err).NotTo(HaveOccurred())

			mailIdx := spaceidindex.New(filepath.Join(env.Root, "indexes"), "by-mail")
			Expect(mailIdx.Init()).To(Succeed())
			filename := utils.NewFSSafeUserID(guest).SafeFilename()
			index, err := mailIdx.Load(filename)
			Expect(err).NotTo(HaveOccurred())
			_, spaceID, _, err := storagespace.SplitID(resp.StorageSpace.GetId().GetOpaqueId())
			Expect(err).NotTo(HaveOccurred())
			Expect(index).To(HaveKey(spaceID))

			spaces, err := env.Fs.ListStorageSpaces(ctxpkg.ContextSetUser(context.Background(), &userv1beta1.User{Id: guest}), []*provider.ListStorageSpacesRequest_Filter{
				{Type: provider.ListStorageSpacesRequest_Filter_TYPE_USER, Term: &provider.ListStorageSpacesRequest_Filter_User{User: guest}},
			}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(spaces).To(HaveLen(1))
		})
	})
})
