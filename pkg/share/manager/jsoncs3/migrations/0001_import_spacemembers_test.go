// Copyright 2026 OpenCloud GmbH
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

package migration

import (
	"context"
	"errors"
	"os"
	"sync"

	gatewayv1beta1 "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	csmocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/share"
	shareMocks "github.com/opencloud-eu/reva/v2/pkg/share/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// mockStorageProvider is a hand-written stub that satisfies the narrow
// storageProvider interface (only ListGrants). This avoids generating a full
// ProviderAPIClient mock.
type mockStorageProvider struct {
	grants []*provider.Grant
	err    error
	status *rpc.Status
}

func (s *mockStorageProvider) ListGrants(_ context.Context, _ *provider.ListGrantsRequest, _ ...grpc.CallOption) (*provider.ListGrantsResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	st := s.status
	if st == nil {
		st = &rpc.Status{Code: rpc.Code_CODE_OK}
	}
	return &provider.ListGrantsResponse{Status: st, Grants: s.grants}, nil
}

// mockLoader records all shares delivered to it through the channels.
type mockLoader struct {
	shares   []*collaboration.Share
	received []share.ReceivedShareWithUser
	err      error // returned from Load; set before calling Migrate()
}

func (l *mockLoader) Load(_ context.Context, sc <-chan *collaboration.Share, rc <-chan share.ReceivedShareWithUser) error {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for s := range sc {
			l.shares = append(l.shares, s)
		}
	}()
	go func() {
		defer wg.Done()
		for r := range rc {
			l.received = append(l.received, r)
		}
	}()
	wg.Wait()
	return l.err
}

// okAuthenticateResponse returns an AuthenticateResponse with CODE_OK and a
// placeholder token, sufficient to let GetServiceUserContextWithContext proceed.
func okAuthenticateResponse() *gatewayv1beta1.AuthenticateResponse {
	return &gatewayv1beta1.AuthenticateResponse{
		Status: &rpc.Status{Code: rpc.Code_CODE_OK},
		Token:  "test-token",
	}
}

// buildMigration creates an ImportSpaceMembersMigration wired to the supplied
// gateway mock, share-manager mock, and loader, using a temporary DiskStorage.
func buildMigration(tmpdir string, gwMock *csmocks.GatewayAPIClient, mgr *shareMocks.Manager, ldr share.LoadableManager) *ImportSpaceMembersMigration {
	stor, err := metadata.NewDiskStorage(tmpdir)
	Expect(err).NotTo(HaveOccurred())

	pool.RemoveSelector("GatewaySelector" + "eu.opencloud.api.gateway")
	gsel := pool.GetSelector[gatewayv1beta1.GatewayAPIClient](
		"GatewaySelector",
		"eu.opencloud.api.gateway",
		func(_ grpc.ClientConnInterface) gatewayv1beta1.GatewayAPIClient {
			return gwMock
		},
	)

	m := &ImportSpaceMembersMigration{}
	m.Initialize(config{
		logger:               zerolog.Nop(),
		gatewaySelector:      gsel,
		storage:              stor,
		serviceAccountID:     "svc",
		serviceAccountSecret: "secret",
		manager:              mgr,
		loader:               ldr,
	})
	return m
}

// ── retryOnUnavailable ────────────────────────────────────────────────────────

var _ = Describe("retryOnUnavailable", func() {
	var log zerolog.Logger

	BeforeEach(func() {
		log = zerolog.Nop()
	})

	It("returns nil when op succeeds immediately", func() {
		calls := 0
		err := retryOnUnavailable(context.Background(), log, func() error {
			calls++
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(1))
	})

	It("retries on errtypes.Unavailable and succeeds when op eventually succeeds", func() {
		calls := 0
		err := retryOnUnavailable(context.Background(), log, func() error {
			calls++
			if calls < 3 {
				return errtypes.Unavailable("ldap down")
			}
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(3))
	})

	It("stops immediately on a permanent (non-Unavailable) error", func() {
		sentinel := errors.New("permanent failure")
		calls := 0
		err := retryOnUnavailable(context.Background(), log, func() error {
			calls++
			return sentinel
		})
		Expect(err).To(MatchError(sentinel))
		Expect(calls).To(Equal(1))
	})

	It("returns an error when context is cancelled before op succeeds", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already cancelled
		err := retryOnUnavailable(ctx, log, func() error {
			return errtypes.Unavailable("ldap down")
		})
		Expect(err).To(HaveOccurred())
	})

})

// ── resolveUserID ─────────────────────────────────────────────────────────────

var _ = Describe("resolveUserID", func() {
	var (
		tmpdir string
		gwMock *csmocks.GatewayAPIClient
		m      *ImportSpaceMembersMigration
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "resolve-user-*")
		Expect(err).NotTo(HaveOccurred())

		gwMock = &csmocks.GatewayAPIClient{}
		m = buildMigration(tmpdir, gwMock, &shareMocks.Manager{}, &mockLoader{})
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("successful lookup", func() {
		BeforeEach(func() {
			gwMock.On("GetUser", mock.Anything, mock.MatchedBy(func(r *userpb.GetUserRequest) bool {
				return r.UserId.OpaqueId == "alice"
			})).Return(&userpb.GetUserResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				User:   &userpb.User{Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com", Type: userpb.UserType_USER_TYPE_PRIMARY}},
			}, nil)
		})

		It("returns the full UserId", func() {
			id, err := m.resolveUserID(context.Background(), "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(id.Idp).To(Equal("idp.example.com"))
			Expect(id.OpaqueId).To(Equal("alice"))
		})

		It("caches the result and only calls the gateway once", func() {
			_, _ = m.resolveUserID(context.Background(), "alice")
			_, _ = m.resolveUserID(context.Background(), "alice")
			gwMock.AssertNumberOfCalls(GinkgoT(), "GetUser", 1)
		})
	})

	Context("gateway returns CODE_NOT_FOUND", func() {
		BeforeEach(func() {
			gwMock.On("GetUser", mock.Anything, mock.Anything).Return(&userpb.GetUserResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND, Message: "no such user"},
			}, nil)
		})

		It("returns an error without retrying", func() {
			_, err := m.resolveUserID(context.Background(), "ghost")
			Expect(err).To(HaveOccurred())
			gwMock.AssertNumberOfCalls(GinkgoT(), "GetUser", 1)
		})
	})

	Context("gateway returns CODE_UNAVAILABLE on first call then succeeds", func() {
		BeforeEach(func() {
			calls := 0
			gwMock.On("GetUser", mock.Anything, mock.Anything).Return(
				func(_ context.Context, _ *userpb.GetUserRequest, _ ...grpc.CallOption) *userpb.GetUserResponse {
					calls++
					if calls == 1 {
						return &userpb.GetUserResponse{Status: &rpc.Status{Code: rpc.Code_CODE_UNAVAILABLE, Message: "ldap down"}}
					}
					return &userpb.GetUserResponse{
						Status: &rpc.Status{Code: rpc.Code_CODE_OK},
						User:   &userpb.User{Id: &userpb.UserId{OpaqueId: "bob", Idp: "idp.example.com"}},
					}
				},
				func(_ context.Context, _ *userpb.GetUserRequest, _ ...grpc.CallOption) error { return nil },
			)
		})

		It("retries and eventually returns the resolved ID", func() {
			id, err := m.resolveUserID(context.Background(), "bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(id.OpaqueId).To(Equal("bob"))
			gwMock.AssertNumberOfCalls(GinkgoT(), "GetUser", 2)
		})
	})
})

// ── resolveGroupID ────────────────────────────────────────────────────────────

var _ = Describe("resolveGroupID", func() {
	var (
		tmpdir string
		gwMock *csmocks.GatewayAPIClient
		m      *ImportSpaceMembersMigration
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "resolve-group-*")
		Expect(err).NotTo(HaveOccurred())

		gwMock = &csmocks.GatewayAPIClient{}
		m = buildMigration(tmpdir, gwMock, &shareMocks.Manager{}, &mockLoader{})
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("successful lookup", func() {
		BeforeEach(func() {
			gwMock.On("GetGroup", mock.Anything, mock.MatchedBy(func(r *grouppb.GetGroupRequest) bool {
				return r.GroupId.OpaqueId == "admins"
			})).Return(&grouppb.GetGroupResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				Group:  &grouppb.Group{Id: &grouppb.GroupId{OpaqueId: "admins", Idp: "idp.example.com"}},
			}, nil)
		})

		It("returns the full GroupId", func() {
			id, err := m.resolveGroupID(context.Background(), "admins")
			Expect(err).NotTo(HaveOccurred())
			Expect(id.Idp).To(Equal("idp.example.com"))
			Expect(id.OpaqueId).To(Equal("admins"))
		})

		It("caches the result and only calls the gateway once", func() {
			_, _ = m.resolveGroupID(context.Background(), "admins")
			_, _ = m.resolveGroupID(context.Background(), "admins")
			gwMock.AssertNumberOfCalls(GinkgoT(), "GetGroup", 1)
		})
	})

	Context("gateway returns CODE_NOT_FOUND", func() {
		BeforeEach(func() {
			gwMock.On("GetGroup", mock.Anything, mock.Anything).Return(&grouppb.GetGroupResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND, Message: "no such group"},
			}, nil)
		})

		It("returns an error without retrying", func() {
			_, err := m.resolveGroupID(context.Background(), "ghost-group")
			Expect(err).To(HaveOccurred())
			gwMock.AssertNumberOfCalls(GinkgoT(), "GetGroup", 1)
		})
	})
})

// ── spaceGrantToShares ────────────────────────────────────────────────────────

var _ = Describe("spaceGrantToShares", func() {
	var (
		tmpdir  string
		gwMock  *csmocks.GatewayAPIClient
		mgrMock *shareMocks.Manager
		m       *ImportSpaceMembersMigration

		space *provider.StorageSpace
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "grant-to-shares-*")
		Expect(err).NotTo(HaveOccurred())

		gwMock = &csmocks.GatewayAPIClient{}
		mgrMock = &shareMocks.Manager{}
		m = buildMigration(tmpdir, gwMock, mgrMock, &mockLoader{})

		space = &provider.StorageSpace{
			Id:   &provider.StorageSpaceId{OpaqueId: "space1"},
			Root: &provider.ResourceId{StorageId: "stor1", SpaceId: "space1", OpaqueId: "space1"},
		}
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("user grant — share does not yet exist", func() {
		BeforeEach(func() {
			// resolveUserID
			gwMock.On("GetUser", mock.Anything, mock.Anything).Return(&userpb.GetUserResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				User:   &userpb.User{Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com"}},
			}, nil)
			// GetShare returns not-found so the share needs to be created
			mgrMock.On("GetShare", mock.Anything, mock.Anything).Return(nil, errtypes.NotFound("not found"))
		})

		It("returns a new share and one received-share entry for the user", func() {
			grant := &provider.Grant{
				Grantee: &provider.Grantee{
					Type: provider.GranteeType_GRANTEE_TYPE_USER,
					Id:   &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "alice"}},
				},
				Permissions: &provider.ResourcePermissions{},
				Creator:     &userpb.UserId{OpaqueId: "owner", Idp: "idp.example.com", Type: userpb.UserType_USER_TYPE_PRIMARY},
			}

			sh, rs, err := m.spaceGrantToShares(context.Background(), grant, space)
			Expect(err).NotTo(HaveOccurred())
			Expect(sh).NotTo(BeNil())
			Expect(rs).To(HaveLen(1))
			Expect(rs[0].UserID.OpaqueId).To(Equal("alice"))
		})
	})

	Context("user grant — share already exists", func() {
		BeforeEach(func() {
			gwMock.On("GetUser", mock.Anything, mock.Anything).Return(&userpb.GetUserResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				User:   &userpb.User{Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com"}},
			}, nil)
			// GetShare returns an existing share
			mgrMock.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.Share{}, nil)
		})

		It("returns nil share (skip) without error", func() {
			grant := &provider.Grant{
				Grantee: &provider.Grantee{
					Type: provider.GranteeType_GRANTEE_TYPE_USER,
					Id:   &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "alice"}},
				},
				Permissions: &provider.ResourcePermissions{},
			}

			sh, _, err := m.spaceGrantToShares(context.Background(), grant, space)
			Expect(err).NotTo(HaveOccurred())
			Expect(sh).To(BeNil())
		})
	})

	Context("group grant — share does not yet exist", func() {
		BeforeEach(func() {
			// resolveGroupID
			gwMock.On("GetGroup", mock.Anything, mock.Anything).Return(&grouppb.GetGroupResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				Group:  &grouppb.Group{Id: &grouppb.GroupId{OpaqueId: "admins", Idp: "idp.example.com"}},
			}, nil)
			// GetShare → not found
			mgrMock.On("GetShare", mock.Anything, mock.Anything).Return(nil, errtypes.NotFound("not found"))
			// GetMembers → two members
			gwMock.On("GetMembers", mock.Anything, mock.Anything).Return(&grouppb.GetMembersResponse{
				Status:  &rpc.Status{Code: rpc.Code_CODE_OK},
				Members: []*userpb.UserId{{OpaqueId: "u1", Idp: "idp"}, {OpaqueId: "u2", Idp: "idp"}},
			}, nil)
		})

		It("returns a share and received-share entries for each member plus a group-level nil entry", func() {
			grant := &provider.Grant{
				Grantee: &provider.Grantee{
					Type: provider.GranteeType_GRANTEE_TYPE_GROUP,
					Id:   &provider.Grantee_GroupId{GroupId: &grouppb.GroupId{OpaqueId: "admins"}},
				},
				Permissions: &provider.ResourcePermissions{},
				Creator:     &userpb.UserId{OpaqueId: "owner", Idp: "idp.example.com", Type: userpb.UserType_USER_TYPE_PRIMARY},
			}

			sh, rs, err := m.spaceGrantToShares(context.Background(), grant, space)
			Expect(err).NotTo(HaveOccurred())
			Expect(sh).NotTo(BeNil())
			// 2 member entries + 1 group-level nil entry
			Expect(rs).To(HaveLen(3))
			Expect(rs[2].UserID).To(BeNil())
		})
	})
})

// ── Migrate (integration-level) ───────────────────────────────────────────────

var _ = Describe("ImportSpaceMembersMigration.Migrate", func() {
	var (
		tmpdir  string
		gwMock  *csmocks.GatewayAPIClient
		mgrMock *shareMocks.Manager
		loader  *mockLoader
		m       *ImportSpaceMembersMigration
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "migrate-*")
		Expect(err).NotTo(HaveOccurred())

		gwMock = &csmocks.GatewayAPIClient{}
		mgrMock = &shareMocks.Manager{}
		loader = &mockLoader{}
		m = buildMigration(tmpdir, gwMock, mgrMock, loader)

		// Authenticate — needed by GetServiceUserContextWithContext
		gwMock.On("Authenticate", mock.Anything, mock.Anything).Return(
			okAuthenticateResponse(), nil)
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("no project spaces exist", func() {
		BeforeEach(func() {
			gwMock.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(
				&provider.ListStorageSpacesResponse{
					Status:        &rpc.Status{Code: rpc.Code_CODE_OK},
					StorageSpaces: []*provider.StorageSpace{},
				}, nil)
		})

		It("completes without error and the loader records nothing", func() {
			Expect(m.Migrate()).To(Succeed())
			Expect(loader.shares).To(BeEmpty())
			Expect(loader.received).To(BeEmpty())
		})
	})

	Context("one space with one user grant", func() {
		BeforeEach(func() {
			space := &provider.StorageSpace{
				Id:   &provider.StorageSpaceId{OpaqueId: "s1"},
				Root: &provider.ResourceId{StorageId: "stor", SpaceId: "s1", OpaqueId: "s1"},
			}
			gwMock.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(
				&provider.ListStorageSpacesResponse{
					Status:        &rpc.Status{Code: rpc.Code_CODE_OK},
					StorageSpaces: []*provider.StorageSpace{space},
				}, nil)

			// injected provider — returns one grant
			m.providerResolver = func(_ context.Context, _ *provider.StorageSpace) (storageProvider, error) {
				return &mockStorageProvider{
					grants: []*provider.Grant{
						{
							Grantee: &provider.Grantee{
								Type: provider.GranteeType_GRANTEE_TYPE_USER,
								Id:   &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "alice"}},
							},
							Permissions: &provider.ResourcePermissions{},
							Creator:     &userpb.UserId{OpaqueId: "owner", Idp: "idp", Type: userpb.UserType_USER_TYPE_PRIMARY},
						},
					},
				}, nil
			}

			gwMock.On("GetUser", mock.Anything, mock.Anything).Return(&userpb.GetUserResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_OK},
				User:   &userpb.User{Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp"}},
			}, nil)

			mgrMock.On("GetShare", mock.Anything, mock.Anything).Return(nil, errtypes.NotFound(""))
		})

		It("sends one share and one received-share to the loader", func() {
			Expect(m.Migrate()).To(Succeed())
			Expect(loader.shares).To(HaveLen(1))
			Expect(loader.received).To(HaveLen(1))
		})
	})

	Context("ListStorageSpaces returns an error", func() {
		BeforeEach(func() {
			gwMock.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(
				nil, errors.New("network error"))
		})

		It("returns an error", func() {
			Expect(m.Migrate()).To(HaveOccurred())
		})
	})

	Context("ListStorageSpaces returns a non-OK status", func() {
		BeforeEach(func() {
			gwMock.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(
				&provider.ListStorageSpacesResponse{
					Status: &rpc.Status{Code: rpc.Code_CODE_INTERNAL, Message: "storage crashed"},
				}, nil)
		})

		It("returns an error", func() {
			Expect(m.Migrate()).To(HaveOccurred())
		})
	})

	Context("loader returns an error", func() {
		BeforeEach(func() {
			loader.err = errors.New("load failure")

			gwMock.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(
				&provider.ListStorageSpacesResponse{
					Status:        &rpc.Status{Code: rpc.Code_CODE_OK},
					StorageSpaces: []*provider.StorageSpace{},
				}, nil)
		})

		It("propagates the loader error", func() {
			Expect(m.Migrate()).To(MatchError("load failure"))
		})
	})
})
