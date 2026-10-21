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

package gateway

import (
	"context"
	"encoding/json"
	"errors"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaborationv1beta1 "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	registryv1beta1 "github.com/cs3org/go-cs3apis/cs3/storage/registry/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"

	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
)

// providerAddr is the fake address used for all mock pool selectors.
const providerAddr = "storage-provider:9999"

// newTestSvc builds a minimal svc suitable for unit tests, injecting
// mock pool clients via named selectors so no real gRPC connections are made.
func newTestSvc(
	registryMock *cs3mocks.StorageRegistryAPIClient,
	spacesMock *cs3mocks.SpacesAPIClient,
	collaborationMock *cs3mocks.CollaborationAPIClient,
) *svc {
	// Register mocks into the pool using the same selector names that the
	// production code resolves through pool.GetStorageRegistryClient,
	// pool.GetSpacesProviderServiceClient, and pool.GetUserShareProviderClient.
	pool.RemoveSelector("StorageRegistrySelector" + providerAddr)
	pool.GetSelector[registryv1beta1.RegistryAPIClient](
		"StorageRegistrySelector",
		providerAddr,
		func(_ grpc.ClientConnInterface) registryv1beta1.RegistryAPIClient {
			return registryMock
		},
	)

	pool.RemoveSelector("SpacesProviderSelector" + providerAddr)
	pool.GetSelector[provider.SpacesAPIClient](
		"SpacesProviderSelector",
		providerAddr,
		func(_ grpc.ClientConnInterface) provider.SpacesAPIClient {
			return spacesMock
		},
	)

	pool.RemoveSelector("SharingCollaborationSelector" + providerAddr)
	pool.GetSelector[collaborationv1beta1.CollaborationAPIClient](
		"SharingCollaborationSelector",
		providerAddr,
		func(_ grpc.ClientConnInterface) collaborationv1beta1.CollaborationAPIClient {
			return collaborationMock
		},
	)

	return &svc{
		c: &config{
			StorageRegistryEndpoint:   providerAddr,
			UserShareProviderEndpoint: providerAddr,
		},
		providerCache:            cache.GetProviderCache(cache.Config{Store: "noop"}),
		createPersonalSpaceCache: cache.GetCreatePersonalSpaceCache(cache.Config{Store: "memory"}),
	}
}

// providerInfoWithAddress returns a ProviderInfo whose Address field is set to
// providerAddr so that the gateway will look up the right pool selector.
func providerInfoWithAddress() *registryv1beta1.ProviderInfo {
	spacesJSON, _ := json.Marshal([]*provider.StorageSpace{{
		Root: &provider.ResourceId{
			StorageId: "storageid",
			SpaceId:   "spaceid",
			OpaqueId:  "spaceid",
		},
	}})
	return &registryv1beta1.ProviderInfo{
		Address: providerAddr,
		Opaque: &typesv1beta1.Opaque{
			Map: map[string]*typesv1beta1.OpaqueEntry{
				"spaces": {
					Decoder: "json",
					Value:   spacesJSON,
				},
			},
		},
	}
}

var _ = Describe("CreateStorageSpace", func() {
	var (
		ctx               context.Context
		registryMock      *cs3mocks.StorageRegistryAPIClient
		spacesMock        *cs3mocks.SpacesAPIClient
		collaborationMock *cs3mocks.CollaborationAPIClient
		s                 *svc

		creatorUser = &userpb.User{
			Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp"},
		}

		// A CreateStorageSpace response representing a freshly created project space.
		createdSpaceID = &provider.StorageSpaceId{
			OpaqueId: storagespace.FormatStorageID("storageid", "spaceid"),
		}
		createdSpaceRoot = &provider.ResourceId{
			StorageId: "storageid",
			SpaceId:   "spaceid",
			OpaqueId:  "spaceid",
		}
		successfulCreateSpaceResp = &provider.CreateStorageSpaceResponse{
			Status: status.NewOK(context.Background()),
			StorageSpace: &provider.StorageSpace{
				Id:   createdSpaceID,
				Root: createdSpaceRoot,
			},
		}
	)

	BeforeEach(func() {
		registryMock = cs3mocks.NewStorageRegistryAPIClient(GinkgoT())
		spacesMock = cs3mocks.NewSpacesAPIClient(GinkgoT())
		collaborationMock = cs3mocks.NewCollaborationAPIClient(GinkgoT())
		s = newTestSvc(registryMock, spacesMock, collaborationMock)
		ctx = ctxpkg.ContextSetUser(context.Background(), creatorUser)

		// Default: registry resolves to our mock provider for both GetStorageProviders
		// (used in CreateStorageSpace) and ListStorageProviders (used in DeleteStorageSpace
		// during rollback).
		registryMock.EXPECT().
			GetStorageProviders(mock.Anything, mock.Anything, mock.Anything).
			Return(&registryv1beta1.GetStorageProvidersResponse{
				Status:    status.NewOK(ctx),
				Providers: []*registryv1beta1.ProviderInfo{providerInfoWithAddress()},
			}, nil).Maybe()

		registryMock.EXPECT().
			ListStorageProviders(mock.Anything, mock.Anything, mock.Anything).
			Return(&registryv1beta1.ListStorageProvidersResponse{
				Status:    status.NewOK(ctx),
				Providers: []*registryv1beta1.ProviderInfo{providerInfoWithAddress()},
			}, nil).Maybe()

		// Default: the storage provider creates the space successfully.
		spacesMock.EXPECT().
			CreateStorageSpace(mock.Anything, mock.Anything, mock.Anything).
			Return(successfulCreateSpaceResp, nil).Maybe()
	})

	Context("personal space", func() {
		It("skips the initial share and returns success", func() {
			resp, err := s.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{
				Type: "personal",
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			// No share must be created for personal spaces.
			collaborationMock.AssertNotCalled(GinkgoT(), "CreateShare")
		})
	})

	Context("non-personal space", func() {
		var req *provider.CreateStorageSpaceRequest

		BeforeEach(func() {
			req = &provider.CreateStorageSpaceRequest{Type: "project", Name: "My Project"}
		})

		It("creates the initial share and returns the space on success", func() {
			collaborationMock.EXPECT().
				CreateShare(mock.Anything, mock.Anything, mock.Anything).
				Return(&collaborationv1beta1.CreateShareResponse{
					Status: status.NewOK(ctx),
				}, nil).Once()

			resp, err := s.CreateStorageSpace(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(resp.GetStorageSpace().GetId().GetOpaqueId()).To(Equal(createdSpaceID.OpaqueId))
			collaborationMock.AssertNumberOfCalls(GinkgoT(), "CreateShare", 1)
		})

		It("rolls back the space and returns an error when CreateShare returns a gRPC error", func() {
			collaborationMock.EXPECT().
				CreateShare(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, errors.New("share service unavailable")).Once()

			spacesMock.EXPECT().
				DeleteStorageSpace(mock.Anything, mock.Anything, mock.Anything).
				Return(&provider.DeleteStorageSpaceResponse{
					Status: status.NewOK(ctx),
				}, nil).Twice()

			collaborationMock.EXPECT().
				ListShares(mock.Anything, mock.Anything).
				Return(&collaborationv1beta1.ListSharesResponse{
					Status: status.NewOK(ctx),
				}, nil).Once()

			resp, err := s.CreateStorageSpace(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
		})

		It("rolls back the space and returns an error when CreateShare returns a non-OK status", func() {
			collaborationMock.EXPECT().
				CreateShare(mock.Anything, mock.Anything, mock.Anything).
				Return(&collaborationv1beta1.CreateShareResponse{
					Status: status.NewInternal(ctx, "share store full"),
				}, nil).Once()

			spacesMock.EXPECT().
				DeleteStorageSpace(mock.Anything, mock.Anything, mock.Anything).
				Return(&provider.DeleteStorageSpaceResponse{
					Status: status.NewOK(ctx),
				}, nil).Twice()

			collaborationMock.EXPECT().
				ListShares(mock.Anything, mock.Anything).
				Return(&collaborationv1beta1.ListSharesResponse{
					Status: status.NewOK(ctx),
				}, nil).Once()

			resp, err := s.CreateStorageSpace(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
			// The status message should be propagated from the share response.
			Expect(resp.GetStatus().GetMessage()).To(ContainSubstring("share store full"))
		})

		It("still returns an error even if the rollback DeleteStorageSpace also fails", func() {
			collaborationMock.EXPECT().
				CreateShare(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, errors.New("share service unavailable")).Once()

			spacesMock.EXPECT().
				DeleteStorageSpace(mock.Anything, mock.Anything, mock.Anything).
				Return(nil, errors.New("storage provider unavailable")).Once()

			resp, err := s.CreateStorageSpace(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
		})

		It("does not create an initial share when the storage provider fails to create the space", func() {
			spacesMock.EXPECT().
				CreateStorageSpace(mock.Anything, mock.Anything, mock.Anything).
				Unset()
			spacesMock.EXPECT().
				CreateStorageSpace(mock.Anything, mock.Anything, mock.Anything).
				Return(&provider.CreateStorageSpaceResponse{
					Status: status.NewInternal(ctx, "disk full"),
				}, nil).Once()

			resp, err := s.CreateStorageSpace(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
			collaborationMock.AssertNotCalled(GinkgoT(), "CreateShare")
		})
	})
})

// stubStorageProvider records whether Delete reached the storage provider.
type stubStorageProvider struct {
	provider.ProviderAPIClient
	deleted bool
}

func (s *stubStorageProvider) Delete(
	ctx context.Context,
	_ *provider.DeleteRequest,
	_ ...grpc.CallOption,
) (*provider.DeleteResponse, error) {
	s.deleted = true
	return &provider.DeleteResponse{Status: status.NewOK(ctx)}, nil
}

// registerStubStorageProvider injects the given stub as the storage provider
// client so that requests passing the gateway reach it.
func registerStubStorageProvider(stub *stubStorageProvider) {
	pool.RemoveSelector("StorageProviderSelector" + providerAddr)
	pool.GetSelector[provider.ProviderAPIClient](
		"StorageProviderSelector",
		providerAddr,
		func(_ grpc.ClientConnInterface) provider.ProviderAPIClient { return stub },
	)
}

func receivedShares(shares ...*collaborationv1beta1.ReceivedShare) *collaborationv1beta1.ListReceivedSharesResponse {
	return &collaborationv1beta1.ListReceivedSharesResponse{
		Status: status.NewOK(context.Background()),
		Shares: shares,
	}
}

func receivedShareOf(id *provider.ResourceId) *collaborationv1beta1.ReceivedShare {
	return &collaborationv1beta1.ReceivedShare{
		Share: &collaborationv1beta1.Share{ResourceId: id},
	}
}

var _ = Describe("Delete", func() {
	var (
		ctx               context.Context
		registryMock      *cs3mocks.StorageRegistryAPIClient
		collaborationMock *cs3mocks.CollaborationAPIClient
		stub              *stubStorageProvider
		s                 *svc

		sharee = &userpb.User{
			Id: &userpb.UserId{OpaqueId: "brian", Idp: "idp"},
		}

		sharedID = &provider.ResourceId{StorageId: "storageid", SpaceId: "spaceid", OpaqueId: "nodeid"}

		deleteRequest = func(id *provider.ResourceId, path string) *provider.DeleteRequest {
			return &provider.DeleteRequest{
				Ref: &provider.Reference{ResourceId: id, Path: path},
			}
		}
	)

	BeforeEach(func() {
		registryMock = cs3mocks.NewStorageRegistryAPIClient(GinkgoT())
		collaborationMock = cs3mocks.NewCollaborationAPIClient(GinkgoT())
		stub = &stubStorageProvider{}
		s = newTestSvc(registryMock, &cs3mocks.SpacesAPIClient{}, collaborationMock)
		registerStubStorageProvider(stub)
		ctx = ctxpkg.ContextSetUser(context.Background(), sharee)

		registryMock.EXPECT().
			ListStorageProviders(mock.Anything, mock.Anything, mock.Anything).
			Return(&registryv1beta1.ListStorageProvidersResponse{
				Status:    status.NewOK(ctx),
				Providers: []*registryv1beta1.ProviderInfo{providerInfoWithAddress()},
			}, nil).Maybe()
	})

	Context("the reference points at the root of a received share", func() {
		It("denies the delete for an empty path", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(receivedShareOf(sharedID)), nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_PERMISSION_DENIED))
			Expect(stub.deleted).To(BeFalse())
		})

		It("denies the delete for a dot path", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(receivedShareOf(sharedID)), nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, "."))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_PERMISSION_DENIED))
			Expect(stub.deleted).To(BeFalse())
		})

		It("denies the delete for a share with a backwards compatible id", func() {
			legacyID := &provider.ResourceId{StorageId: "storageid$spaceid", OpaqueId: "nodeid"}
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(receivedShareOf(legacyID)), nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_PERMISSION_DENIED))
			Expect(stub.deleted).To(BeFalse())
		})
	})

	Context("the reference points at a resource which is not a received share root", func() {
		It("deletes a resource the user did not receive", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(receivedShareOf(sharedID)), nil).Once()

			otherID := &provider.ResourceId{StorageId: "storageid", SpaceId: "spaceid", OpaqueId: "othernode"}
			resp, err := s.Delete(ctx, deleteRequest(otherID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(stub.deleted).To(BeTrue())
		})

		It("deletes a resource when the user received no shares", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(), nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(stub.deleted).To(BeTrue())
		})

		It("deletes the root of a space the user is a member of", func() {
			spaceRootID := &provider.ResourceId{StorageId: "storageid", SpaceId: "spaceid", OpaqueId: "spaceid"}
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(receivedShares(receivedShareOf(spaceRootID)), nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(spaceRootID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(stub.deleted).To(BeTrue())
		})

		It("deletes a resource below the root of a received share", func() {
			resp, err := s.Delete(ctx, deleteRequest(sharedID, "child.txt"))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(stub.deleted).To(BeTrue())
			collaborationMock.AssertNotCalled(GinkgoT(), "ListReceivedShares", mock.Anything, mock.Anything)
		})
	})

	Context("the received shares cannot be listed", func() {
		It("fails the delete when the share provider returns a non-OK status", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(&collaborationv1beta1.ListReceivedSharesResponse{
					Status: status.NewInternal(ctx, "share store unavailable"),
				}, nil).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
			Expect(resp.GetStatus().GetMessage()).To(Equal("could not verify share permissions for delete"))
			Expect(stub.deleted).To(BeFalse())
		})

		It("fails the delete when the share provider call fails", func() {
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(nil, errors.New("share service unavailable")).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_INTERNAL))
			Expect(resp.GetStatus().GetMessage()).To(Equal("could not verify share permissions for delete"))
			Expect(stub.deleted).To(BeFalse())
		})

		It("allows the delete when the caller is not permitted to list received shares", func() {
			// public link and app provider tokens are rejected here by the auth interceptor
			collaborationMock.EXPECT().
				ListReceivedShares(mock.Anything, mock.Anything).
				Return(nil, gstatus.Error(codes.PermissionDenied, "auth: core access token is invalid")).Once()

			resp, err := s.Delete(ctx, deleteRequest(sharedID, ""))

			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
			Expect(stub.deleted).To(BeTrue())
			collaborationMock.AssertNumberOfCalls(GinkgoT(), "ListReceivedShares", 1)
		})
	})
})
