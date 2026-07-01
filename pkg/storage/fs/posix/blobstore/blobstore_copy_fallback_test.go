// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package blobstore_test

import (
	"crypto/rand"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	posixblobstore "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
)

var _ = Describe("Blobstore with copy fallback", func() {
	var (
		env *posixhelpers.TestEnv
		bs  *posixblobstore.Blobstore
	)

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		bs, err = posixblobstore.New(env.Root)
		Expect(err).ToNot(HaveOccurred())

		// force the copy-based upload path, as if the upload area and the
		// blobstore root were on different devices
		bs.SetCanUseRenameForUpload(false)
	})

	AfterEach(func() {
		env.Cleanup()
	})

	DescribeTable("stores the blob using the copy fallback",
		func(size int) {
			n, err := env.CreateTestFile("blob.bin", "", env.SpaceRootRes.OpaqueId, env.SpaceRootRes.SpaceId, int64(size))
			Expect(err).ToNot(HaveOccurred())

			data := make([]byte, size)
			_, err = rand.Read(data)
			Expect(err).ToNot(HaveOccurred())

			source := filepath.Join(env.Root, "upload-source")
			Expect(os.WriteFile(source, data, 0600)).To(Succeed())

			Expect(bs.Upload(n, source, "")).To(Succeed())

			written, err := os.ReadFile(n.InternalPath())
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(data))
		},
		Entry("small file", 1024),
		Entry("large file bigger than the 16MiB fsync window", 17<<20),
	)
})
