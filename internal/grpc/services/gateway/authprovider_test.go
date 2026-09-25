// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"errors"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("translateProviderAuthenticateResult", func() {
	ctx := context.Background()
	innerError := &types.OpaqueEntry{Decoder: "json", Value: []byte(`{"reason":"expired"}`)}

	It("passes CODE_UNAUTHENTICATED and its InnerError through unchanged", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_UNAUTHENTICATED, Message: "nope", InnerError: innerError},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeTrue())
		Expect(resp.Status).To(Equal(res.Status))
		Expect(resp.Status.InnerError).To(Equal(innerError))
	})

	It("passes CODE_PERMISSION_DENIED and its InnerError, if present, through unchanged", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_PERMISSION_DENIED, Message: "nope", InnerError: innerError},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeTrue())
		Expect(resp.Status).To(Equal(res.Status))
		Expect(resp.Status.InnerError).To(Equal(innerError))
	})

	It("passes CODE_NOT_FOUND through unchanged", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND, Message: "nope"},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeTrue())
		Expect(resp.Status).To(Equal(res.Status))
	})

	It("passes CODE_UNAVAILABLE through unchanged", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_UNAVAILABLE, Message: "backend down"},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeTrue())
		Expect(resp.Status).To(Equal(res.Status))
	})

	It("maps unexpected application statuses to CODE_INTERNAL", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_INVALID_ARGUMENT, Message: "nope"},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeTrue())
		Expect(resp.Status.Code).To(Equal(rpc.Code_CODE_INTERNAL))
	})

	It("does not short-circuit on CODE_OK", func() {
		res := &authpb.AuthenticateResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_OK},
		}

		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", res, nil)

		Expect(done).To(BeFalse())
		Expect(resp).To(BeNil())
	})

	It("maps transport errors calling the auth provider to CODE_INTERNAL", func() {
		resp, done := translateProviderAuthenticateResult(ctx, "guestlinks", nil, errors.New("connection refused"))

		Expect(done).To(BeTrue())
		Expect(resp.Status.Code).To(Equal(rpc.Code_CODE_INTERNAL))
	})
})
