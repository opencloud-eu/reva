// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tus_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/rhttp/datatx/manager/tus"
	testhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
)

var _ = Describe("tus handler status codes", func() {
	var (
		env *testhelpers.DecomposedTestEnv
		srv *httptest.Server
	)

	BeforeEach(func() {
		var err error
		env, err = testhelpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			GetPath:            true,
			InitiateFileUpload: true,
		}, nil).Maybe()
		env.Blobstore.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

		log := zerolog.Nop()
		m, err := tus.New(map[string]interface{}{}, nil, &log)
		Expect(err).ToNot(HaveOccurred())
		h, err := m.Handler(env.Fs)
		Expect(err).ToNot(HaveOccurred())
		srv = httptest.NewServer(h)
	})

	AfterEach(func() {
		if srv != nil {
			srv.Close()
		}
		if env != nil {
			env.Cleanup()
		}
	})

	upload := func(checksum string, content []byte) *http.Response {
		ids, err := env.Fs.InitiateUpload(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./file.txt"}, int64(len(content)), map[string]string{
			"checksum": checksum,
		})
		Expect(err).ToNot(HaveOccurred())
		req, err := http.NewRequest(http.MethodPatch, srv.URL+"/"+ids["tus"], bytes.NewReader(content))
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Tus-Resumable", "1.0.0")
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		res, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		_ = res.Body.Close()
		return res
	}

	It("rejects an upload with a wrong checksum with 460 Checksum Mismatch", func() {
		res := upload("sha1 0000000000000000000000000000000000000000", []byte("hello"))
		Expect(res.StatusCode).To(Equal(460))
	})

	It("accepts an upload with a matching checksum", func() {
		res := upload("sha1 aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d", []byte("hello"))
		Expect(res.StatusCode).To(Equal(http.StatusNoContent))
	})
})
