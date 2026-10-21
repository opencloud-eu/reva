// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tus_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/rhttp/datatx/manager/tus"
	testhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
)

var _ = Describe("tus handler with decomposedfs", func() {
	const (
		size = 64 * 1024
		half = 16 * 1024
	)

	var (
		env      *testhelpers.DecomposedTestEnv
		srv      *httptest.Server
		src      []byte
		url      string
		binPath  string
		blobMu   sync.Mutex
		blobData []byte
	)

	tryPatch := func(offset int64, body io.Reader, length int64) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodPatch, url, body)
		if err != nil {
			return nil, err
		}
		req.ContentLength = length
		req.Header.Set("Tus-Resumable", "1.0.0")
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		_ = res.Body.Close()
		return res, nil
	}

	patch := func(offset int64, body io.Reader, length int64) *http.Response {
		res, err := tryPatch(offset, body, length)
		Expect(err).ToNot(HaveOccurred())
		return res
	}

	head := func() int64 {
		req, err := http.NewRequest(http.MethodHead, url, nil)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Tus-Resumable", "1.0.0")
		res, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		_ = res.Body.Close()
		Expect(res.StatusCode).To(Equal(http.StatusOK))
		offset, err := strconv.ParseInt(res.Header.Get("Upload-Offset"), 10, 64)
		Expect(err).ToNot(HaveOccurred())
		return offset
	}

	BeforeEach(func() {
		var err error
		env, err = testhelpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			GetPath:            true,
			InitiateFileUpload: true,
		}, nil).Maybe()
		// capture what ends up in the blobstore when the upload is finalized
		blobData = nil
		env.Blobstore.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			data, err := os.ReadFile(args.Get(1).(string))
			Expect(err).ToNot(HaveOccurred())
			blobMu.Lock()
			blobData = data
			blobMu.Unlock()
		}).Maybe()

		log := zerolog.Nop()
		m, err := tus.New(map[string]interface{}{}, nil, &log)
		Expect(err).ToNot(HaveOccurred())
		h, err := m.Handler(env.Fs)
		Expect(err).ToNot(HaveOccurred())
		srv = httptest.NewServer(h)

		src = make([]byte, size)
		_, err = rand.Read(src)
		Expect(err).ToNot(HaveOccurred())

		ids, err := env.Fs.InitiateUpload(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./file.bin"}, size, map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		Expect(ids["tus"]).ToNot(BeEmpty())
		url = srv.URL + "/" + ids["tus"]
		binPath = filepath.Join(env.Root, "uploads", ids["tus"])
		Expect(binPath).To(BeAnExistingFile())
	})

	AfterEach(func() {
		if srv != nil {
			srv.Close()
		}
		if env != nil {
			env.Cleanup()
		}
	})

	stagedSize := func() int64 {
		fi, err := os.Stat(binPath)
		if err != nil {
			return -1
		}
		return fi.Size()
	}

	finishedBlob := func() []byte {
		blobMu.Lock()
		defer blobMu.Unlock()
		return blobData
	}

	It("resumes sequentially from the offset returned by HEAD", func() {
		Expect(patch(0, bytes.NewReader(src[:half]), half).StatusCode).To(Equal(http.StatusNoContent))
		offset := head()
		Expect(offset).To(Equal(int64(half)))
		Expect(patch(offset, bytes.NewReader(src[offset:]), int64(size)-offset).StatusCode).To(Equal(http.StatusNoContent))

		Expect(sha256.Sum256(finishedBlob())).To(Equal(sha256.Sum256(src)))
	})

	It("does not interleave bytes when a retry overlaps a stalled PATCH", func() {
		// The first PATCH sends half of its body and then stalls, as if a proxy or a slow
		// network were holding it open.
		pr, pw := io.Pipe()
		firstDone := make(chan struct{})
		go func() {
			// the client sees this request fail; its body is cut short below
			_, _ = tryPatch(0, pr, size)
			close(firstDone)
		}()
		_, err := pw.Write(src[:half])
		Expect(err).ToNot(HaveOccurred())
		Eventually(stagedSize).Should(Equal(int64(half)))

		// The client times out and retries: HEAD, then PATCH from the returned offset.
		offset := head()
		Expect(offset).To(Equal(int64(half)))
		Expect(patch(offset, bytes.NewReader(src[offset:2*half]), half).StatusCode).To(Equal(http.StatusNoContent))

		// The stalled first request wakes up and keeps sending its body.
		go func() { _, _ = pw.Write(src[half : 2*half]) }()
		time.Sleep(200 * time.Millisecond)
		_ = pw.CloseWithError(io.ErrUnexpectedEOF)
		Eventually(firstDone, 10*time.Second).Should(BeClosed())
		_ = pr.Close()

		// Whatever was staged must be an exact prefix of the source.
		staged, err := os.ReadFile(binPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(staged)).To(BeNumerically("<=", size))
		Expect(bytes.Equal(staged, src[:len(staged)])).To(BeTrue(), "staging file holds %d bytes that are not a prefix of the source", len(staged))

		// Completing the upload from the server's offset yields the source byte-for-byte.
		offset = head()
		Expect(patch(offset, bytes.NewReader(src[offset:]), int64(size)-offset).StatusCode).To(Equal(http.StatusNoContent))
		Expect(sha256.Sum256(finishedBlob())).To(Equal(sha256.Sum256(src)))
	})
})
