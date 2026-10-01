// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package authprovider

import (
	"context"
	"errors"
	"fmt"
	"testing"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuthProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuthProvider Suite")
}

// mockAuthManager is a minimal auth.Manager whose Authenticate result is
// controlled by the test.
type mockAuthManager struct {
	user  *user.User
	scope map[string]*authpb.Scope
	err   error
}

func (m *mockAuthManager) Configure(map[string]interface{}) error { return nil }

func (m *mockAuthManager) Authenticate(ctx context.Context, clientID, clientSecret string) (*user.User, map[string]*authpb.Scope, error) {
	return m.user, m.scope, m.err
}

// detailedErr is a typed error opting into status.StatusInnerErrorProvider.
type detailedErr struct {
	msg   string
	entry *types.OpaqueEntry
}

func (e *detailedErr) Error() string { return e.msg }

func (e *detailedErr) StatusInnerError() *types.OpaqueEntry { return e.entry }

var _ = Describe("service.Authenticate", func() {
	var req *authpb.AuthenticateRequest

	BeforeEach(func() {
		req = &authpb.AuthenticateRequest{ClientId: "alice", ClientSecret: "secret"}
	})

	It("attaches InnerError when the auth manager error implements the safe interface", func() {
		entry := &types.OpaqueEntry{Decoder: "json", Value: []byte(`{"reason":"expired"}`)}
		s := &service{authmgr: &mockAuthManager{err: &detailedErr{msg: "invalid credentials", entry: entry}}}

		res, err := s.Authenticate(context.Background(), req)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.Code).ToNot(Equal(rpc.Code_CODE_OK))
		Expect(res.Status.InnerError).To(Equal(entry))
	})

	It("still provides details when the typed error is wrapped", func() {
		entry := &types.OpaqueEntry{Decoder: "json", Value: []byte(`{"reason":"expired"}`)}
		inner := &detailedErr{msg: "invalid credentials", entry: entry}
		wrapped := fmt.Errorf("authmanager: %w", inner)
		s := &service{authmgr: &mockAuthManager{err: wrapped}}

		res, err := s.Authenticate(context.Background(), req)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.InnerError).To(Equal(entry))
	})

	It("produces no InnerError for a normal error", func() {
		s := &service{authmgr: &mockAuthManager{err: errors.New("invalid credentials")}}

		res, err := s.Authenticate(context.Background(), req)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.Code).ToNot(Equal(rpc.Code_CODE_OK))
		Expect(res.Status.InnerError).To(BeNil())
	})

	It("produces no InnerError and does not panic when the provider returns a nil entry", func() {
		s := &service{authmgr: &mockAuthManager{err: &detailedErr{msg: "invalid credentials", entry: nil}}}

		var res *authpb.AuthenticateResponse
		var err error
		Expect(func() {
			res, err = s.Authenticate(context.Background(), req)
		}).ToNot(Panic())

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.InnerError).To(BeNil())
	})

	It("does not copy error text into InnerError automatically", func() {
		s := &service{authmgr: &mockAuthManager{err: errors.New("some sensitive internal detail")}}

		res, err := s.Authenticate(context.Background(), req)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Status.InnerError).To(BeNil())
	})
})
