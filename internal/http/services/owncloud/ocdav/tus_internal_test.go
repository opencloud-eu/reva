// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocdav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/config"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
)

type gatewayStub struct {
	client gateway.GatewayAPIClient
}

func (g gatewayStub) Next(opts ...pool.Option) (gateway.GatewayAPIClient, error) {
	return g.client, nil
}

var _ = Describe("handleTusPost", func() {
	var s *svc

	BeforeEach(func() {
		c := &config.Config{}
		c.Init() // applies the default invalid_chars, which include the backslash
		s = &svc{nameValidators: ValidatorsFromConfig(c)}
	})

	// Regression test for https://github.com/opencloud-eu/opencloud/issues/2803:
	// a TUS create whose Upload-Metadata filename fails name validation returns
	// 400 Bad Request with the validation error in the body, like the other write
	// handlers (PUT, MOVE, COPY, MKCOL), instead of a bare 412 with no body. Name
	// validation runs before the gateway is contacted, so the branch is exercised
	// without a backend.
	It("rejects an invalid name with 400 and the reason in the body", func() {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(net.HeaderTusResumable, "1.0.0")
		r.Header.Set(net.HeaderUploadLength, "8")
		rec := httptest.NewRecorder()

		meta := map[string]string{"filename": `IM\000024`}
		s.handleTusPost(context.Background(), rec, r, meta, &provider.Reference{}, zerolog.Nop())

		res := rec.Result()
		defer res.Body.Close()
		Expect(res.StatusCode).To(Equal(http.StatusBadRequest))

		body, err := io.ReadAll(res.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("must not contain"))
	})

	Describe("an empty upload", func() {
		var (
			client  *mocks.GatewayAPIClient
			touched *provider.TouchFileRequest
		)

		BeforeEach(func() {
			touched = nil
			client = &mocks.GatewayAPIClient{}
			client.On("Stat", mock.Anything, mock.Anything).Return(&provider.StatResponse{
				Status: status.NewNotFound(context.Background(), "not found"),
			}, nil)
			client.On("TouchFile", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				touched = args.Get(1).(*provider.TouchFileRequest)
			}).Return(&provider.TouchFileResponse{
				Status: status.NewOK(context.Background()),
			}, nil)
			s.gatewaySelector = gatewayStub{client: client}
		})

		post := func(mtime string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Header.Set(net.HeaderTusResumable, "1.0.0")
			r.Header.Set(net.HeaderUploadLength, "0")
			rec := httptest.NewRecorder()

			meta := map[string]string{"filename": "empty.txt", "mtime": mtime}
			s.handleTusPost(context.Background(), rec, r, meta, &provider.Reference{Path: "/empty.txt"}, zerolog.Nop())
			return rec
		}

		It("creates the file with the mtime from Upload-Metadata", func() {
			Expect(post("1565237893").Code).To(Equal(http.StatusCreated))
			Expect(touched).ToNot(BeNil())
			Expect(utils.ReadPlainFromOpaque(touched.GetOpaque(), net.HeaderOCMtime)).To(Equal("1565237893"))
		})

		It("ignores an mtime of null", func() {
			Expect(post("null").Code).To(Equal(http.StatusCreated))
			Expect(touched).ToNot(BeNil())
			Expect(utils.ReadPlainFromOpaque(touched.GetOpaque(), net.HeaderOCMtime)).To(BeEmpty())
		})
	})
})
