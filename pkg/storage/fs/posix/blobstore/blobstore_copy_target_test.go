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

var _ = Describe("Blobstore with a copy target", func() {
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
	})

	AfterEach(func() {
		env.Cleanup()
	})

	// fs revisions pass a copy target to keep the current version of the file
	DescribeTable("writes the blob to the copy target as well",
		func(canUseRenameForUpload bool, size int) {
			bs.SetCanUseRenameForUpload(canUseRenameForUpload)

			n, err := env.CreateTestFile("blob.bin", "", env.SpaceRootRes.OpaqueId, env.SpaceRootRes.SpaceId, int64(size))
			Expect(err).ToNot(HaveOccurred())

			data := make([]byte, size)
			_, err = rand.Read(data)
			Expect(err).ToNot(HaveOccurred())

			source := filepath.Join(env.Root, "upload-source")
			Expect(os.WriteFile(source, data, 0600)).To(Succeed())

			copyTarget := filepath.Join(env.Root, "revisions", "blob.bin.current")
			Expect(bs.Upload(n, source, copyTarget)).To(Succeed())

			written, err := os.ReadFile(n.InternalPath())
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(data))

			copied, err := os.ReadFile(copyTarget)
			Expect(err).ToNot(HaveOccurred())
			Expect(copied).To(Equal(data))
		},
		Entry("when the source is renamed into place", true, 1024),
		Entry("when the source is copied into place", false, 1024),
		Entry("when the blob is bigger than the 16MiB fsync window", true, 17<<20),
	)
})
