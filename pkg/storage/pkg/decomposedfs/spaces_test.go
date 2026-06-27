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
	"path/filepath"

	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/spaceidindex"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
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

		// Reproducer for opencloud-eu/opencloud#1878: a ListStorageSpaces that
		// resolves a node whose name attribute is gone (e.g. after the owning user
		// was deleted) must skip it, not hard-fail the whole listing with code 15
		// ("error listing spaces") or spam an error log on every call.
		Context("when a space node's name attribute is unset", func() {
			// orphanSpace creates a project space and removes its name attribute, so
			// reading it returns ENOATTR ("no data available"), the exact #1878 state.
			orphanSpace := func(name string) *provider.StorageSpace {
				resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: name, Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				root := resp.StorageSpace.GetRoot()
				n, err := node.ReadNode(env.Ctx, env.Lookup, root.GetSpaceId(), root.GetOpaqueId(), "", true, nil, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(n.RemoveXattr(env.Ctx, prefixes.NameAttr, true)).To(Succeed())
				return resp.StorageSpace
			}

			It("returns an empty result instead of failing for an id filter", func() {
				space := orphanSpace("Nameless")
				// the id filter (not the +grant type) is what routes the request into
				// the direct-read branch; +grant mirrors the real caller from the issue.
				filters := []*provider.ListStorageSpacesRequest_Filter{
					{
						Type: provider.ListStorageSpacesRequest_Filter_TYPE_ID,
						Term: &provider.ListStorageSpacesRequest_Filter_Id{Id: &provider.StorageSpaceId{OpaqueId: storagespace.FormatResourceID(space.GetRoot())}},
					},
					{
						Type: provider.ListStorageSpacesRequest_Filter_TYPE_SPACE_TYPE,
						Term: &provider.ListStorageSpacesRequest_Filter_SpaceType{SpaceType: "+grant"},
					},
				}
				spaces, err := env.Fs.ListStorageSpaces(env.Ctx, filters, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(spaces).To(BeEmpty())
			})

			It("skips the unreadable space but still returns the healthy ones for a list-all", func() {
				healthy, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "Healthy", Type: "project"})
				Expect(err).ToNot(HaveOccurred())
				orphan := orphanSpace("Nameless")

				// list-all exercises the worker-loop path; it must skip the orphan and
				// still return the healthy space, not truncate the whole result.
				spaces, err := env.Fs.ListStorageSpaces(env.Ctx, nil, false)
				Expect(err).ToNot(HaveOccurred())
				ids := make([]string, 0, len(spaces))
				for _, s := range spaces {
					ids = append(ids, s.GetId().GetOpaqueId())
				}
				Expect(ids).To(ContainElement(healthy.StorageSpace.GetId().GetOpaqueId()))
				Expect(ids).ToNot(ContainElement(orphan.GetId().GetOpaqueId()))
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
			// Reproducer for opencloud-eu/opencloud#2985: purging a space must
			// invalidate ALL of its msgpack indexes, not only the by-type index.
			// The by-user-id index keeps a stale entry pointing at the purged
			// space, which is exactly the reporter's "stale references in
			// indexes/by-user-id/*.mpk".
			It("removes the space from the by-user-id index", func() {
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

				// purge as the space owner
				Expect(env.Fs.DeleteStorageSpace(env.Ctx, delReq)).To(Succeed())

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

				load := func(indexName, key string) map[string]string {
					idx := spaceidindex.New(filepath.Join(env.Root, "indexes"), indexName)
					Expect(idx.Init()).To(Succeed())
					m, lerr := idx.Load(key)
					Expect(lerr).ToNot(HaveOccurred())
					return m
				}

				// precondition: both grantee indexes reference the space
				Expect(load("by-user-id", userGrantee)).To(HaveKey(spaceID))
				Expect(load("by-group-id", groupGrantee)).To(HaveKey(spaceID))

				// disable, then purge as the space owner
				Expect(env.Fs.DeleteStorageSpace(env.Ctx, &provider.DeleteStorageSpaceRequest{Id: sid})).To(Succeed())
				Expect(env.Fs.DeleteStorageSpace(env.Ctx, &provider.DeleteStorageSpaceRequest{
					Opaque: &typesv1beta1.Opaque{Map: map[string]*typesv1beta1.OpaqueEntry{"purge": {Decoder: "plain", Value: []byte("true")}}},
					Id:     sid,
				})).To(Succeed())

				// the grantee index entries must be gone after the purge
				Expect(load("by-user-id", userGrantee)).ToNot(HaveKey(spaceID))
				Expect(load("by-group-id", groupGrantee)).ToNot(HaveKey(spaceID))
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
				Expect(env.Fs.DeleteStorageSpace(env.Ctx, delReq)).To(Succeed())
				after := loadOwnerIndex()
				Expect(after).ToNot(HaveKey(purgedID))
				Expect(after).To(HaveKey(siblingID))
			})
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
		)
	})
})
