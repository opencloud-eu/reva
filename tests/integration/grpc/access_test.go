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

package grpc_test

import (
	"context"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/metadata"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	storagep "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/auth/scope"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposed"
	jwt "github.com/opencloud-eu/reva/v2/pkg/token/manager/jwt"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/opencloud-eu/reva/v2/tests/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test suite tests the different authentication and access scenarios
//
// It uses the `startRevads` helper to spawn the according reva daemon and
// other dependencies like a userprovider if needed.
// It also sets up an authenticated context depending on the scenario and a
// service client to the gateway provider to be used in the assertion functions.
var _ = Describe("Access to shared resources", func() {
	var (
		dependencies = []RevadConfig{
			{Name: "gateway", Config: GatewaySingleStorage},
			{Name: "users", Config: UserProviderJSON},
			{Name: "storage", Config: StorageProviderDecomposed},
			{Name: "storage_publiclink", Config: StoragePublicLink},
			{Name: "permissions", Config: PermissionsOpenCloudCI},
			{Name: "shares", Config: Shares},
		}
		variables = map[string]string{}
		revads    = map[string]*Revad{}

		ctx           context.Context
		serviceClient gateway.GatewayAPIClient
		user          = &userpb.User{
			Id: &userpb.UserId{
				Idp:      "http://localhost:20080",
				OpaqueId: "4c510ada-c86b-4815-8820-42cdf82c3d51",
				TenantId: "c239389d-c249-499d-ae80-07558429769a",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "einstein",
		}

		fs      storage.FS
		homeRef = &storagep.Reference{
			ResourceId: &storagep.ResourceId{
				SpaceId:  user.Id.OpaqueId,
				OpaqueId: user.Id.OpaqueId,
			},
			Path: ".",
		}
		parentPathRef   = &storagep.Reference{ResourceId: homeRef.ResourceId, Path: "./dir"}
		sharePathRef    = &storagep.Reference{ResourceId: homeRef.ResourceId, Path: "./dir/subdir"}
		childPathRef    = &storagep.Reference{ResourceId: homeRef.ResourceId, Path: "./dir/subdir/file.txt"}
		parentIDRef     *storagep.Reference
		projectSpaceRef *storagep.Reference

		assertResponse = func(ctx context.Context, ref *storagep.Reference, op string, expectedCode rpcv1beta1.Code) {
			GinkgoHelper() // Mark this function as a helper so that errors are reported at the caller site

			switch op {
			case "Stat":
				statRes, err := serviceClient.Stat(ctx, &storagep.StatRequest{Ref: ref})
				Expect(err).ToNot(HaveOccurred())
				Expect(statRes.Status.Code).To(Equal(expectedCode))
			case "InitiateFileDownload":
				ifdRes, err := serviceClient.InitiateFileDownload(ctx, &storagep.InitiateFileDownloadRequest{Ref: ref})
				Expect(err).ToNot(HaveOccurred())
				Expect(ifdRes.Status.Code).To(Equal(expectedCode))
			case "InitiateFileUpload":
				uploadRes, err := serviceClient.InitiateFileUpload(ctx, &storagep.InitiateFileUploadRequest{Ref: ref})
				Expect(err).ToNot(HaveOccurred())
				Expect(uploadRes.Status.Code).To(Equal(expectedCode))
			case "Delete":
				delRes, err := serviceClient.Delete(ctx, &storagep.DeleteRequest{Ref: ref})
				Expect(err).ToNot(HaveOccurred())
				Expect(delRes.Status.Code).To(Equal(expectedCode))
			default:
				Fail("unknown operation for assertNotFound")
			}
		}
	)

	JustBeforeEach(func() {
		var err error
		ctx = context.Background()

		// Add auth token
		tokenManager, err := jwt.New(map[string]interface{}{"secret": "changemeplease"})
		Expect(err).ToNot(HaveOccurred())
		scope, err := scope.AddOwnerScope(nil)
		Expect(err).ToNot(HaveOccurred())
		t, err := tokenManager.MintToken(ctx, user, scope)
		Expect(err).ToNot(HaveOccurred())
		ctx = ctxpkg.ContextSetToken(ctx, t)
		ctx = metadata.AppendToOutgoingContext(ctx, ctxpkg.TokenHeader, t)
		ctx = ctxpkg.ContextSetUser(ctx, user)

		// Start revads
		revads, err = startRevads(dependencies, variables)
		Expect(err).ToNot(HaveOccurred())
		Expect(revads["gateway"]).ToNot(BeNil())
		serviceClient, err = pool.GetGatewayServiceClient(revads["gateway"].GrpcAddress)
		Expect(err).ToNot(HaveOccurred())

		// Prepare storage
		fs, err = decomposed.New(map[string]any{
			"root":                revads["storage"].StorageRoot,
			"permissionssvc":      revads["permissions"].GrpcAddress,
			"treesize_accounting": true,
			"treetime_accounting": true,
		}, nil, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		r, err := serviceClient.CreateHome(ctx, &storagep.CreateHomeRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(r.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

		err = fs.CreateDir(ctx, parentPathRef)
		Expect(err).ToNot(HaveOccurred())
		n, err := fs.GetMD(ctx, parentPathRef, []string{}, []string{})
		Expect(err).ToNot(HaveOccurred())
		parentIDRef = &storagep.Reference{
			ResourceId: n.Id,
			Path:       ".",
		}

		err = fs.CreateDir(ctx, sharePathRef)
		Expect(err).ToNot(HaveOccurred())

		err = helpers.Upload(ctx, fs, childPathRef, []byte("1234567890"))
		Expect(err).ToNot(HaveOccurred())

		res, err := fs.CreateStorageSpace(ctx, &storagep.CreateStorageSpaceRequest{
			Type:  "project",
			Name:  "project1",
			Owner: user,
		})
		Expect(err).ToNot(HaveOccurred())
		projectSpaceRef = &storagep.Reference{
			ResourceId: res.StorageSpace.Root,
			Path:       ".",
		}
	})

	AfterEach(func() {
		for _, r := range revads {
			Expect(r.Cleanup(CurrentSpecReport().Failed())).To(Succeed())
		}
	})

	Context("using a public link", func() {
		Context("with read permissions", func() {
			var (
				publicReadLinkRef   *storagep.Reference
				publicReadLinkToken string
				readLinkPermissions = &storagep.ResourcePermissions{
					Stat:                 true,
					GetPath:              true,
					InitiateFileDownload: true,
				}
				publicReadLinkCtx context.Context
			)

			JustBeforeEach(func() {
				statRes, err := serviceClient.Stat(ctx, &storagep.StatRequest{Ref: sharePathRef})
				Expect(err).ToNot(HaveOccurred())
				Expect(statRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

				By("creating a public link for the resource")
				createRes, err := serviceClient.CreatePublicShare(ctx, &link.CreatePublicShareRequest{
					ResourceInfo: &storagep.ResourceInfo{
						Id:                statRes.Info.Id,
						ArbitraryMetadata: &storagep.ArbitraryMetadata{},
					},
					Grant: &link.Grant{
						Permissions: &link.PublicSharePermissions{
							Permissions: readLinkPermissions,
						},
					},
					Description: "test",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(createRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				publicReadLinkToken = createRes.Share.Token

				By("authenticating with the public link token")
				publicReadLinkCtx = context.Background()
				authRes, err := serviceClient.Authenticate(publicReadLinkCtx, &gateway.AuthenticateRequest{
					Type:         "publicshares",
					ClientId:     publicReadLinkToken,
					ClientSecret: "password|",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(authRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				publicReadLinkCtx = ctxpkg.ContextSetToken(publicReadLinkCtx, authRes.Token)
				publicReadLinkCtx = metadata.AppendToOutgoingContext(publicReadLinkCtx, ctxpkg.TokenHeader, authRes.Token)

				publicReadLinkRef = &storagep.Reference{
					ResourceId: &storagep.ResourceId{
						StorageId: utils.PublicStorageProviderID,
						SpaceId:   utils.PublicStorageSpaceID,
						OpaqueId:  publicReadLinkToken,
					},
					Path: ".",
				}
			})

			When("using the public link authenticated context", func() {
				It("grants read access", func() {
					By("allowing stat requests to the shared resource")
					assertResponse(publicReadLinkCtx, publicReadLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)

					By("allowing stat requests to a child of the shared resource")
					publicReadLinkRef.Path = "file.txt"
					assertResponse(publicReadLinkCtx, publicReadLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)

					By("allowing download requests to a child of the shared resource")
					assertResponse(publicReadLinkCtx, publicReadLinkRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_OK)
				})

				It("denies writeaccess", func() {
					By("denying InitiateFileUpload requests to the shared resource")
					assertResponse(publicReadLinkCtx, homeRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)

					By("denying Delete requests to the shared resource")
					assertResponse(publicReadLinkCtx, homeRef, "Delete", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
				})
			})
		})

		Context("with write permissions", func() {
			var (
				publicWriteLinkRef   *storagep.Reference
				publicWriteLinkToken string
				writeLinkPermissions = &storagep.ResourcePermissions{
					Stat:                 true,
					CreateContainer:      true,
					Delete:               true,
					GetPath:              true,
					InitiateFileDownload: true,
					InitiateFileUpload:   true,
				}
				publicWriteLinkCtx context.Context
			)

			JustBeforeEach(func() {
				statRes, err := serviceClient.Stat(ctx, &storagep.StatRequest{Ref: sharePathRef})
				Expect(err).ToNot(HaveOccurred())
				Expect(statRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

				By("creating a public link for the resource")
				createRes, err := serviceClient.CreatePublicShare(ctx, &link.CreatePublicShareRequest{
					ResourceInfo: &storagep.ResourceInfo{
						Id:                statRes.Info.Id,
						ArbitraryMetadata: &storagep.ArbitraryMetadata{},
					},
					Grant: &link.Grant{
						Permissions: &link.PublicSharePermissions{
							Permissions: writeLinkPermissions,
						},
					},
					Description: "test",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(createRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				publicWriteLinkToken = createRes.Share.Token

				By("authenticating with the public link token")
				publicWriteLinkCtx = context.Background()
				authRes, err := serviceClient.Authenticate(publicWriteLinkCtx, &gateway.AuthenticateRequest{
					Type:         "publicshares",
					ClientId:     publicWriteLinkToken,
					ClientSecret: "password|",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(authRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
				publicWriteLinkCtx = ctxpkg.ContextSetToken(publicWriteLinkCtx, authRes.Token)
				publicWriteLinkCtx = metadata.AppendToOutgoingContext(publicWriteLinkCtx, ctxpkg.TokenHeader, authRes.Token)

				publicWriteLinkRef = &storagep.Reference{
					ResourceId: &storagep.ResourceId{
						StorageId: utils.PublicStorageProviderID,
						SpaceId:   utils.PublicStorageSpaceID,
						OpaqueId:  publicWriteLinkToken,
					},
				}

				// Warm up any caches that might be involved
				assertResponse(publicWriteLinkCtx, sharePathRef, "Stat", rpcv1beta1.Code_CODE_OK)
				assertResponse(publicWriteLinkCtx, publicWriteLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)
				assertResponse(ctx, sharePathRef, "Stat", rpcv1beta1.Code_CODE_OK)
				assertResponse(ctx, projectSpaceRef, "Stat", rpcv1beta1.Code_CODE_OK)
			})

			When("using the regular user context", func() {
				It("denies access to the public link", func() {
					assertResponse(ctx, publicWriteLinkRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)
				})
			})

			When("using the public link authenticated context", func() {
				It("grants access", func() {
					By("allowing stat requests to the shared resource")
					assertResponse(publicWriteLinkCtx, publicWriteLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)

					By("allowing stat requests to a child of the shared resource")
					publicWriteLinkRef.Path = "file.txt"
					assertResponse(publicWriteLinkCtx, publicWriteLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)

					By("allowing uploads to the shared resource")
					assertResponse(publicWriteLinkCtx, publicWriteLinkRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_OK)
				})

				It("denies access to the space root", func() {
					// Access to the space root
					By("denying stat requests to the space root of the shared resource")
					assertResponse(publicWriteLinkCtx, homeRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)

					By("denying InitiateFileDownload requests to the space root of the shared resource")
					assertResponse(publicWriteLinkCtx, homeRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)

					By("denying uploads to the space root of the shared resource")
					assertResponse(publicWriteLinkCtx, homeRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
				})

				It("denies access outside the shared resource", func() {
					// Access to the parent of the shared resource
					By("denying path based stat requests to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentPathRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)
					By("denying id based stat requests to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentIDRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)

					By("denying path based InitiateFileDownload requests to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentPathRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
					By("denying id based InitiateFileDownload requests to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentIDRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)

					By("denying path based uploads to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentPathRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
					By("denying id based uploads to the parent of the shared resource")
					assertResponse(publicWriteLinkCtx, parentIDRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
				})

				It("denies access to project spaces", func() {
					// Access to a project space of the share creator
					By("denying stat requests to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)

					By("denying InitiateFileDownload requests to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)

					By("denying uploads to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
				})
			})

			Context("when sharing a resource in the space root", func() {
				JustBeforeEach(func() {
					statRes, err := serviceClient.Stat(ctx, &storagep.StatRequest{Ref: parentPathRef})
					Expect(err).ToNot(HaveOccurred())
					Expect(statRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

					By("creating a public link for the resource")
					createRes, err := serviceClient.CreatePublicShare(ctx, &link.CreatePublicShareRequest{
						ResourceInfo: &storagep.ResourceInfo{
							Id:                statRes.Info.Id,
							ArbitraryMetadata: &storagep.ArbitraryMetadata{},
						},
						Grant: &link.Grant{
							Permissions: &link.PublicSharePermissions{
								Permissions: writeLinkPermissions,
							},
						},
						Description: "test",
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(createRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
					publicWriteLinkToken = createRes.Share.Token

					By("authenticating with the public link token")
					publicWriteLinkCtx = context.Background()
					authRes, err := serviceClient.Authenticate(publicWriteLinkCtx, &gateway.AuthenticateRequest{
						Type:         "publicshares",
						ClientId:     publicWriteLinkToken,
						ClientSecret: "password|",
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(authRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
					publicWriteLinkCtx = ctxpkg.ContextSetToken(publicWriteLinkCtx, authRes.Token)
					publicWriteLinkCtx = metadata.AppendToOutgoingContext(publicWriteLinkCtx, ctxpkg.TokenHeader, authRes.Token)

					publicWriteLinkRef = &storagep.Reference{
						ResourceId: &storagep.ResourceId{
							StorageId: utils.PublicStorageProviderID,
							SpaceId:   utils.PublicStorageSpaceID,
							OpaqueId:  publicWriteLinkToken,
						},
					}
					// Try to access the parent of the shared resource (=space root) to warm up caches
					assertResponse(publicWriteLinkCtx, homeRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)
					assertResponse(publicWriteLinkCtx, sharePathRef, "Stat", rpcv1beta1.Code_CODE_OK)
					assertResponse(publicWriteLinkCtx, publicWriteLinkRef, "Stat", rpcv1beta1.Code_CODE_OK)
					assertResponse(ctx, projectSpaceRef, "Stat", rpcv1beta1.Code_CODE_OK)
				})

				It("denies access to project spaces", func() {
					// Access to a project space of the share creator
					By("denying stat requests to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "Stat", rpcv1beta1.Code_CODE_NOT_FOUND)

					By("denying InitiateFileDownload requests to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "InitiateFileDownload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)

					By("denying uploads to a project space of the share creator")
					assertResponse(publicWriteLinkCtx, projectSpaceRef, "InitiateFileUpload", rpcv1beta1.Code_CODE_PERMISSION_DENIED)
				})
			})
		})
	})
})
