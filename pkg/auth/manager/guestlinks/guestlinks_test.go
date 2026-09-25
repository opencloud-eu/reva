// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package guestlinks_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	groupv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	storageprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	"github.com/opencloud-eu/reva/v2/pkg/auth/manager/guestlinks"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
)

func TestGuestlinks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Guestlinks Suite")
}

const (
	jwtSecret         = "test-jwt-secret-1234567890"
	saID              = "sa-id"
	saSecret          = "sa-secret"
	guestEmail        = "guest@example.com"
	guestIdp          = "guests.example.com"
	guestTenant       = "tenant-1"
	validShareID      = "share-id-1"
	testGatewayAddrFn = "guestlinks-test-gateway"
)

var uniqueCounter int

func uniqueGatewayAddr() string {
	uniqueCounter++
	return fmt.Sprintf("%s-%d", testGatewayAddrFn, uniqueCounter)
}

func newTestManager(gatewayAddr string, logger *zerolog.Logger) (interface {
	Authenticate(ctx context.Context, clientID, clientSecret string) (*userpb.User, map[string]*authpb.Scope, error)
}, error) {
	m, err := guestlinks.New(map[string]any{
		"gateway_addr":           gatewayAddr,
		"jwt_secret":             jwtSecret,
		"service_account_id":     saID,
		"service_account_secret": saSecret,
	}, logger)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// mockGateway registers a *cs3mocks.GatewayAPIClient as the gateway selector
// backing pool.GetGatewayServiceClient(addr), following the established
// testing pattern used across the reva test suite (see e.g.
// internal/grpc/services/usershareprovider/usershareprovider_test.go).
func mockGateway(addr string) *cs3mocks.GatewayAPIClient {
	gatewayClient := &cs3mocks.GatewayAPIClient{}
	pool.RemoveSelector("GatewaySelector" + addr)
	pool.GetSelector[gateway.GatewayAPIClient](
		"GatewaySelector",
		addr,
		func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
			return gatewayClient
		},
	)
	return gatewayClient
}

type token struct {
	iss     string
	aud     string
	typ     string
	shareID string
	iat     *time.Time
	nbf     *time.Time
	exp     *time.Time
	alg     string // "HS256", "none", "HS384"
	secret  string
}

func defaultToken() token {
	now := time.Now()
	iat := now.Add(-time.Minute)
	nbf := now.Add(-time.Minute)
	exp := now.Add(time.Hour)
	return token{
		iss:     "opencloud",
		aud:     "opencloud-guest",
		typ:     "guest-session-v1",
		shareID: validShareID,
		iat:     &iat,
		nbf:     &nbf,
		exp:     &exp,
		alg:     "HS256",
		secret:  jwtSecret,
	}
}

func (tk token) sign() string {
	claims := jwt.MapClaims{}
	if tk.iss != "" {
		claims["iss"] = tk.iss
	}
	if tk.aud != "" {
		claims["aud"] = tk.aud
	}
	if tk.typ != "" {
		claims["type"] = tk.typ
	}
	if tk.shareID != "" {
		claims["share_id"] = tk.shareID
	}
	if tk.iat != nil {
		claims["iat"] = tk.iat.Unix()
	}
	if tk.nbf != nil {
		claims["nbf"] = tk.nbf.Unix()
	}
	if tk.exp != nil {
		claims["exp"] = tk.exp.Unix()
	}

	var method jwt.SigningMethod
	switch tk.alg {
	case "HS384":
		method = jwt.SigningMethodHS384
	case "HS512":
		method = jwt.SigningMethodHS512
	case "none":
		method = jwt.SigningMethodNone
	default:
		method = jwt.SigningMethodHS256
	}

	t := jwt.NewWithClaims(method, claims)
	var signed string
	var err error
	if tk.alg == "none" {
		signed, err = t.SignedString(jwt.UnsafeAllowNoneSignatureType)
	} else {
		signed, err = t.SignedString([]byte(tk.secret))
	}
	Expect(err).ToNot(HaveOccurred())
	return signed
}

func serviceAccountAuthOK(gatewayClient *cs3mocks.GatewayAPIClient) {
	gatewayClient.On("Authenticate", mock.Anything, mock.MatchedBy(func(req *gateway.AuthenticateRequest) bool {
		return req.Type == "serviceaccounts" && req.ClientId == saID && req.ClientSecret == saSecret
	})).Return(&gateway.AuthenticateResponse{
		Status: &rpc.Status{Code: rpc.Code_CODE_OK},
		Token:  "sa-access-token",
	}, nil)
}

func validGuestShare() *collaboration.Share {
	return &collaboration.Share{
		Id: &collaboration.ShareId{OpaqueId: validShareID},
		Grantee: &storageprovider.Grantee{
			Type: storageprovider.GranteeType_GRANTEE_TYPE_USER,
			Id: &storageprovider.Grantee_UserId{
				UserId: &userpb.UserId{
					Idp:      guestIdp,
					OpaqueId: guestEmail,
					Type:     userpb.UserType_USER_TYPE_GUEST,
					TenantId: guestTenant,
				},
			},
		},
	}
}

func getShareOK(s *collaboration.Share) *collaboration.GetShareResponse {
	return &collaboration.GetShareResponse{
		Status: &rpc.Status{Code: rpc.Code_CODE_OK},
		Share:  s,
	}
}

var _ = Describe("guestlinks auth manager", func() {
	var (
		ctx           context.Context
		addr          string
		gatewayClient *cs3mocks.GatewayAPIClient
		logBuf        *bytes.Buffer
		logger        zerolog.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		addr = uniqueGatewayAddr()
		gatewayClient = mockGateway(addr)
		logBuf = &bytes.Buffer{}
		logger = zerolog.New(logBuf)
	})

	Describe("Configure / New", func() {
		It("fails construction when gateway_addr is empty", func() {
			_, err := guestlinks.New(map[string]any{
				"jwt_secret":             jwtSecret,
				"service_account_id":     saID,
				"service_account_secret": saSecret,
			}, &logger)
			Expect(err).To(HaveOccurred())
		})

		It("fails construction when jwt_secret is empty", func() {
			_, err := guestlinks.New(map[string]any{
				"gateway_addr":           addr,
				"service_account_id":     saID,
				"service_account_secret": saSecret,
			}, &logger)
			Expect(err).To(HaveOccurred())
		})

		It("fails construction when service_account_id is empty", func() {
			_, err := guestlinks.New(map[string]any{
				"gateway_addr":           addr,
				"jwt_secret":             jwtSecret,
				"service_account_secret": saSecret,
			}, &logger)
			Expect(err).To(HaveOccurred())
		})

		It("fails construction when service_account_secret is empty", func() {
			_, err := guestlinks.New(map[string]any{
				"gateway_addr":       addr,
				"jwt_secret":         jwtSecret,
				"service_account_id": saID,
			}, &logger)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Authenticate", func() {
		It("rejects a non-empty client_id", func() {
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "not-empty", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			var ic errtypes.IsInvalidCredentials
			Expect(errAs(err, &ic)).To(BeTrue())
		})

		It("authenticates a valid HS256 token with an active guest share", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(validGuestShare()), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			u, scope, err := m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).ToNot(HaveOccurred())
			Expect(u).ToNot(BeNil())
			Expect(u.Id.OpaqueId).To(Equal(guestEmail))
			Expect(u.Id.Idp).To(Equal(guestIdp))
			Expect(u.Id.TenantId).To(Equal(guestTenant))
			Expect(u.Id.Type).To(Equal(userpb.UserType_USER_TYPE_GUEST))
			Expect(u.Username).To(Equal(guestEmail))
			Expect(u.DisplayName).To(Equal(guestEmail))

			Expect(scope).To(HaveKey("user"))
			Expect(scope["user"].Role).To(Equal(authpb.Role_ROLE_OWNER))
		})

		It("accepts a pending guest share without mutating it", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(validGuestShare()), nil)
			gatewayClient.AssertNotCalled(GinkgoT(), "UpdateReceivedShare", mock.Anything, mock.Anything)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).ToNot(HaveOccurred())
		})

		DescribeTable("rejects tokens with wrong algorithm",
			func(alg string) {
				tk := defaultToken()
				tk.alg = alg
				m, err := newTestManager(addr, &logger)
				Expect(err).ToNot(HaveOccurred())

				_, _, err = m.Authenticate(ctx, "", tk.sign())
				Expect(err).To(HaveOccurred())
				assertGenericUnauthenticated(err)
			},
			Entry("none", "none"),
			Entry("HS384", "HS384"),
			Entry("HS512", "HS512"),
		)

		It("rejects a bad signature", func() {
			tk := defaultToken()
			tk.secret = "wrong-secret"
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a wrong issuer", func() {
			tk := defaultToken()
			tk.iss = "someone-else"
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a wrong audience", func() {
			tk := defaultToken()
			tk.aud = "someone-else"
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a wrong type", func() {
			tk := defaultToken()
			tk.typ = "something-else"
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a missing share_id", func() {
			tk := defaultToken()
			tk.shareID = ""
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a missing iat", func() {
			tk := defaultToken()
			tk.iat = nil
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a missing nbf", func() {
			tk := defaultToken()
			tk.nbf = nil
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a missing exp", func() {
			tk := defaultToken()
			tk.exp = nil
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a future nbf with generic unauthenticated and no detail", func() {
			tk := defaultToken()
			future := time.Now().Add(time.Hour)
			tk.nbf = &future
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects an invalid (future) iat with generic unauthenticated and no detail", func() {
			tk := defaultToken()
			future := time.Now().Add(time.Hour)
			tk.iat = &future
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("returns unauthenticated with the exact JSON InnerError for an expiry-only failure", func() {
			tk := defaultToken()
			past := time.Now().Add(-time.Hour)
			tk.exp = &past
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())

			var ic errtypes.IsInvalidCredentials
			Expect(errAs(err, &ic)).To(BeTrue())

			entry := status.InnerErrorFromErr(err)
			Expect(entry).ToNot(BeNil())
			Expect(entry.Decoder).To(Equal("json"))
			Expect(string(entry.Value)).To(MatchJSON(fmt.Sprintf(
				`{"type":"opencloud_guest_link_error","reason":"session_expired","share_id":%q}`,
				validShareID,
			)))

			// expired token must not trigger the service-account/share lookup
			gatewayClient.AssertNotCalled(GinkgoT(), "Authenticate", mock.Anything, mock.Anything)
			gatewayClient.AssertNotCalled(GinkgoT(), "GetShare", mock.Anything, mock.Anything)
		})

		It("does not expose share_id or detail for an expired token with a bad signature", func() {
			tk := defaultToken()
			past := time.Now().Add(-time.Hour)
			tk.exp = &past
			tk.secret = "wrong-secret"
			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", tk.sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)

			gatewayClient.AssertNotCalled(GinkgoT(), "Authenticate", mock.Anything, mock.Anything)
			gatewayClient.AssertNotCalled(GinkgoT(), "GetShare", mock.Anything, mock.Anything)
		})

		It("mints a fresh service-account token on every valid unexpired request", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(validGuestShare()), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			for i := 0; i < 3; i++ {
				_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
				Expect(err).ToNot(HaveOccurred())
			}

			gatewayClient.AssertNumberOfCalls(GinkgoT(), "Authenticate", 3)
			gatewayClient.AssertNumberOfCalls(GinkgoT(), "GetShare", 3)
		})

		It("maps service-account authentication rejection to unavailable", func() {
			gatewayClient.On("Authenticate", mock.Anything, mock.Anything).Return(&gateway.AuthenticateResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_UNAUTHENTICATED},
			}, nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			var iu errtypes.IsUnavailable
			Expect(errAs(err, &iu)).To(BeTrue())

			gatewayClient.AssertNotCalled(GinkgoT(), "GetShare", mock.Anything, mock.Anything)
		})

		It("maps a GetShare backend transport failure to unavailable", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("boom"))

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			var iu errtypes.IsUnavailable
			Expect(errAs(err, &iu)).To(BeTrue())
		})

		It("maps a share-provider CODE_UNAVAILABLE status to unavailable", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.GetShareResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_UNAVAILABLE},
			}, nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			var iu errtypes.IsUnavailable
			Expect(errAs(err, &iu)).To(BeTrue())
		})

		It("maps an unexpected GetShare status to internal", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.GetShareResponse{
				Status: &rpc.Status{Code: rpc.Code_CODE_INVALID_ARGUMENT},
			}, nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			var ii errtypes.IsInternalError
			Expect(errAs(err, &ii)).To(BeTrue())
		})

		DescribeTable("returns generic unauthenticated for missing/inaccessible/deleted anchor shares",
			func(code rpc.Code) {
				serviceAccountAuthOK(gatewayClient)
				gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.GetShareResponse{
					Status: &rpc.Status{Code: code},
				}, nil)

				m, err := newTestManager(addr, &logger)
				Expect(err).ToNot(HaveOccurred())

				_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
				Expect(err).To(HaveOccurred())
				assertGenericUnauthenticated(err)
			},
			Entry("not found", rpc.Code_CODE_NOT_FOUND),
			Entry("permission denied", rpc.Code_CODE_PERMISSION_DENIED),
			Entry("unauthenticated", rpc.Code_CODE_UNAUTHENTICATED),
		)

		It("returns generic unauthenticated for an expired anchor share (first-lookup JSONCS3 expired-share behavior)", func() {
			serviceAccountAuthOK(gatewayClient)
			s := validGuestShare()
			s.Expiration = &types.Timestamp{Seconds: uint64(time.Now().Add(-time.Hour).Unix())}
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(s), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a group share anchor", func() {
			serviceAccountAuthOK(gatewayClient)
			s := validGuestShare()
			s.Grantee = &storageprovider.Grantee{
				Type: storageprovider.GranteeType_GRANTEE_TYPE_GROUP,
				Id: &storageprovider.Grantee_GroupId{
					GroupId: &groupIDPlaceholder,
				},
			}
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(s), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("rejects a normal (non-guest) user share anchor", func() {
			serviceAccountAuthOK(gatewayClient)
			s := validGuestShare()
			s.Grantee.GetUserId().Type = userpb.UserType_USER_TYPE_PRIMARY
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(s), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).To(HaveOccurred())
			assertGenericUnauthenticated(err)
		})

		It("returns a user/owner scope", func() {
			serviceAccountAuthOK(gatewayClient)
			gatewayClient.On("GetShare", mock.Anything, mock.Anything).Return(getShareOK(validGuestShare()), nil)

			m, err := newTestManager(addr, &logger)
			Expect(err).ToNot(HaveOccurred())

			_, sc, err := m.Authenticate(ctx, "", defaultToken().sign())
			Expect(err).ToNot(HaveOccurred())
			Expect(sc).To(HaveKey("user"))
			Expect(sc["user"].Role).To(Equal(authpb.Role_ROLE_OWNER))
		})
	})
})

var groupIDPlaceholder = groupv1beta1.GroupId{OpaqueId: "some-group"}

func assertGenericUnauthenticated(err error) {
	var ic errtypes.IsInvalidCredentials
	Expect(errAs(err, &ic)).To(BeTrue())
	Expect(status.InnerErrorFromErr(err)).To(BeNil())
}

// errAs is a tiny wrapper around errors.As to keep call sites terse.
func errAs(err error, target any) bool {
	return errors.As(err, target)
}
