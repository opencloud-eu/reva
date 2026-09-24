// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package grpc_test

import (
	"context"
	"time"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	storagep "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/metadata"

	"github.com/opencloud-eu/reva/v2/pkg/auth/scope"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposed"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	jwtmgr "github.com/opencloud-eu/reva/v2/pkg/token/manager/jwt"
	"github.com/opencloud-eu/reva/v2/tests/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This suite exercises the guestlinks auth manager end to end, across the
// real gateway/authregistry/authprovider gRPC boundary
//
//   - a "gateway" revad hosting the gateway + authregistry + storage
//     wiring,
//   - a "guestlinks" revad hosting the authprovider with the guestlinks
//     auth manager under test,
//   - a "serviceaccounts" revad hosting the authprovider that mints the
//     fresh service-account token guestlinks uses to re-validate the
//     anchor share on every request,
//   - a "shares" revad hosting the (memory) collaborative share manager
//     that persists the anchor share.

const guestSessionJWTSecret = "guest-session-jwt-secret"

// serviceAccountID must match fixtures/authprovider-serviceaccounts.toml.
const serviceAccountID = "guestlinks-sa"

var _ = Describe("guestlinks authprovider", func() {
	var (
		dependencies []RevadConfig
		variables    = map[string]string{}
		revads       map[string]*Revad

		ctx           context.Context
		gatewayClient gateway.GatewayAPIClient

		// The "memory" share manager's GetShare only allows the share
		// creator or grantee (matched by Idp+OpaqueId, ignoring user
		// type) to look a share up. The "serviceaccounts" auth manager
		// always mints an identity with Idp "none" and OpaqueId equal to
		// the configured service account id, so the owner identity used
		// to create the anchor share here is deliberately set to the
		// same Idp/OpaqueId
		owner = &userpb.User{
			Id: &userpb.UserId{
				Idp:      "none",
				OpaqueId: serviceAccountID,
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "einstein",
		}

		guestEmail  = "guest@example.com"
		guestIdp    = "guests.example.com"
		fileRef     *storagep.Reference
		anchorShare *collaboration.Share
	)

	BeforeEach(func() {
		dependencies = []RevadConfig{
			{Name: "gateway", Config: "gateway-guestlinks.toml"},
			{Name: "users", Config: "userprovider-json.toml"},
			{Name: "storage", Config: "storageprovider-decomposed-guestlinks.toml"},
			{Name: "permissions", Config: "permissions-opencloud-ci.toml"},
			{Name: "shares", Config: "shares.toml"},
			{Name: "guestlinks", Config: "authprovider-guestlinks.toml"},
			{Name: "serviceaccounts", Config: "authprovider-serviceaccounts.toml"},
		}
	})

	JustBeforeEach(func() {
		var err error
		ctx = context.Background()

		// Build an authenticated owner context, the same way other
		// integration tests do (minting a token directly rather than
		// going through a login flow), so we can create the anchor share.
		tokenManager, err := jwtmgr.New(map[string]any{"secret": "changemeplease"})
		Expect(err).ToNot(HaveOccurred())
		ownerScope, err := scope.AddOwnerScope(nil)
		Expect(err).ToNot(HaveOccurred())
		t, err := tokenManager.MintToken(ctx, owner, ownerScope)
		Expect(err).ToNot(HaveOccurred())
		ctx = ctxpkg.ContextSetToken(ctx, t)
		ctx = metadata.AppendToOutgoingContext(ctx, ctxpkg.TokenHeader, t)
		ctx = ctxpkg.ContextSetUser(ctx, owner)

		revads, err = startRevads(dependencies, variables)
		Expect(err).ToNot(HaveOccurred())

		gatewayClient, err = pool.GetGatewayServiceClient(revads["gateway"].GrpcAddress)
		Expect(err).ToNot(HaveOccurred())

		// create the owner's home and a file to share
		res, err := gatewayClient.CreateHome(ctx, &storagep.CreateHomeRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

		// Upload directly against the storage's local FS (rather than
		// through the gateway's HTTP dataprovider) to avoid depending on
		// the test host's externally-reachable hostname/port for the
		// datagateway redirect.
		fs, err := decomposed.New(map[string]interface{}{
			"root":                revads["storage"].StorageRoot,
			"permissionssvc":      revads["permissions"].GrpcAddress,
			"treesize_accounting": true,
			"treetime_accounting": true,
		}, nil, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		spaces, err := fs.ListStorageSpaces(ctx, []*storagep.ListStorageSpacesRequest_Filter{}, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(spaces).ToNot(BeEmpty())
		ssid, err := storagespace.ParseID(spaces[0].Id.OpaqueId)
		Expect(err).ToNot(HaveOccurred())

		fileRef = &storagep.Reference{ResourceId: &ssid, Path: "/file.txt"}
		Expect(helpers.Upload(ctx, fs, fileRef, []byte("hello guest"))).To(Succeed())

		statRes, err := gatewayClient.Stat(ctx, &storagep.StatRequest{Ref: fileRef})
		Expect(err).ToNot(HaveOccurred())
		Expect(statRes.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

		// create the anchor share, granting the guest email viewer access to
		// the file.
		createRes, err := gatewayClient.CreateShare(ctx, &collaboration.CreateShareRequest{
			ResourceInfo: statRes.Info,
			Grant: &collaboration.ShareGrant{
				Grantee: &storagep.Grantee{
					Type: storagep.GranteeType_GRANTEE_TYPE_USER,
					Id: &storagep.Grantee_UserId{
						UserId: &userpb.UserId{
							Idp:      guestIdp,
							OpaqueId: guestEmail,
							Type:     userpb.UserType_USER_TYPE_GUEST,
						},
					},
				},
				Permissions: &collaboration.SharePermissions{
					Permissions: &storagep.ResourcePermissions{
						Stat:                 true,
						InitiateFileDownload: true,
						GetPath:              true,
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(createRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK), createRes.GetStatus().GetMessage())
		anchorShare = createRes.GetShare()
		Expect(anchorShare).ToNot(BeNil())
	})

	AfterEach(func() {
		for _, r := range revads {
			Expect(r.Cleanup(CurrentSpecReport().Failed())).To(Succeed())
		}
	})

	// signGuestToken builds a raw guest-session JWT for the given share,
	// signed with the secret configured in
	// fixtures/authprovider-guestlinks.toml.
	signGuestToken := func(shareID string, ttl time.Duration, secret string) string {
		now := time.Now()
		claims := jwt.MapClaims{
			"iss":      "opencloud",
			"aud":      "opencloud-guest",
			"type":     "guest-session-v1",
			"share_id": shareID,
			"iat":      now.Unix(),
			"nbf":      now.Add(-time.Minute).Unix(),
			"exp":      now.Add(ttl).Unix(),
		}
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := t.SignedString([]byte(secret))
		Expect(err).ToNot(HaveOccurred())
		return signed
	}

	It("authenticates a valid guest-session JWT and returns a usable guest token", func() {
		rawToken := signGuestToken(anchorShare.GetId().GetOpaqueId(), time.Hour, guestSessionJWTSecret)

		authRes, err := gatewayClient.Authenticate(context.Background(), &gateway.AuthenticateRequest{
			Type:         "guestlinks",
			ClientId:     "",
			ClientSecret: rawToken,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(authRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK), authRes.GetStatus().GetMessage())
		Expect(authRes.GetToken()).ToNot(BeEmpty())

		guestUser := authRes.GetUser()
		Expect(guestUser).ToNot(BeNil())
		Expect(guestUser.GetId().GetOpaqueId()).To(Equal(guestEmail))
		Expect(guestUser.GetId().GetIdp()).To(Equal(guestIdp))
		Expect(guestUser.GetId().GetType()).To(Equal(userpb.UserType_USER_TYPE_GUEST))
		Expect(guestUser.GetUsername()).To(Equal(guestEmail))
		Expect(guestUser.GetDisplayName()).To(Equal(guestEmail))

		// the minted token must dismantle back to the same guest identity.
		whoAmIRes, err := gatewayClient.WhoAmI(context.Background(), &gateway.WhoAmIRequest{Token: authRes.GetToken()})
		Expect(err).ToNot(HaveOccurred())
		Expect(whoAmIRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK))
		Expect(whoAmIRes.GetUser().GetId().GetOpaqueId()).To(Equal(guestEmail))

		// the minted token must actually be usable to access the shared
		// resource. We stat directly against the storage provider (to avoid a
		// "shares jail"/mountpoint machinery here) to prove the guest identity
		// carries a real, storage-level grant on the shared file.
		storageClient, err := pool.GetStorageProviderServiceClient(revads["storage"].GrpcAddress)
		Expect(err).ToNot(HaveOccurred())
		guestCtx := metadata.AppendToOutgoingContext(context.Background(), ctxpkg.TokenHeader, authRes.GetToken())
		statRes, err := storageClient.Stat(guestCtx, &storagep.StatRequest{Ref: fileRef})
		Expect(err).ToNot(HaveOccurred())
		Expect(statRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK), statRes.GetStatus().GetMessage())
		Expect(statRes.GetInfo().GetPath()).To(Equal("/file.txt"))
	})

	It("returns the InnerError detail for an expired guest session", func() {
		rawToken := signGuestToken(anchorShare.GetId().GetOpaqueId(), -time.Hour, guestSessionJWTSecret)

		authRes, err := gatewayClient.Authenticate(context.Background(), &gateway.AuthenticateRequest{
			Type:         "guestlinks",
			ClientSecret: rawToken,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(authRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_UNAUTHENTICATED))

		inner := authRes.GetStatus().GetInnerError()
		Expect(inner).ToNot(BeNil())
		Expect(inner.GetDecoder()).To(Equal("json"))
		Expect(string(inner.GetValue())).To(MatchJSON(`{
			"type": "opencloud_guest_link_error",
			"reason": "session_expired",
			"share_id": "` + anchorShare.GetId().GetOpaqueId() + `"
		}`))
	})

	It("rejects a guest session whose anchor share has been removed", func() {
		removeRes, err := gatewayClient.RemoveShare(ctx, &collaboration.RemoveShareRequest{
			Ref: &collaboration.ShareReference{
				Spec: &collaboration.ShareReference_Id{Id: anchorShare.GetId()},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(removeRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_OK), removeRes.GetStatus().GetMessage())

		rawToken := signGuestToken(anchorShare.GetId().GetOpaqueId(), time.Hour, guestSessionJWTSecret)

		authRes, err := gatewayClient.Authenticate(context.Background(), &gateway.AuthenticateRequest{
			Type:         "guestlinks",
			ClientSecret: rawToken,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(authRes.GetStatus().GetCode()).To(Equal(rpcv1beta1.Code_CODE_UNAUTHENTICATED))
		Expect(authRes.GetStatus().GetInnerError()).To(BeNil())
	})
})
