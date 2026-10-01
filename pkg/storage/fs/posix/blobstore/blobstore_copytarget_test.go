// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package blobstore_test

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	posixblobstore "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

var _ = Describe("Blobstore with a copy target", func() {
	var (
		env *posixhelpers.TestEnv
		bs  *posixblobstore.Blobstore
	)

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(map[string]interface{}{"enable_fs_revisions": true})
		Expect(err).ToNot(HaveOccurred())

		bs, err = posixblobstore.New(env.Root)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		env.Cleanup()
	})

	newUpload := func(name string, size int, dir string) (*node.Node, string, []byte) {
		n, err := env.CreateTestFile(name, "", env.SpaceRootRes.OpaqueId, env.SpaceRootRes.SpaceId, int64(size))
		Expect(err).ToNot(HaveOccurred())

		data := make([]byte, size)
		_, err = rand.Read(data)
		Expect(err).ToNot(HaveOccurred())

		source := filepath.Join(dir, "upload-source-"+name)
		Expect(os.WriteFile(source, data, 0600)).To(Succeed())
		return n, source, data
	}

	DescribeTable("writes the blob and the copy",
		func(canRename bool) {
			bs.SetCanUseRenameForUpload(canRename)
			n, source, data := newUpload("blob.bin", 1024, env.Root)
			copyTarget := filepath.Join(env.Root, "copies", "blob.bin.current")

			Expect(bs.Upload(n, source, copyTarget)).To(Succeed())

			Expect(os.ReadFile(n.InternalPath())).To(Equal(data))
			copied, err := os.ReadFile(copyTarget)
			Expect(err).ToNot(HaveOccurred())
			Expect(sha256.Sum256(copied)).To(Equal(sha256.Sum256(data)))
		},
		Entry("when the upload is renamed into place", true),
		Entry("when the upload is copied into place", false),
	)

	It("keeps the current revision when the tree writes a blob with fs revisions enabled", func() {
		env.Blobstore.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(bs.Upload)
		n, source, data := newUpload("file.txt", 4096, env.Root)

		Expect(env.Tree.WriteBlob(n, source)).To(Succeed())

		Expect(os.ReadFile(n.InternalPath())).To(Equal(data))
		current, err := os.ReadFile(env.Lookup.CurrentPath(n.SpaceID, n.ID))
		Expect(err).ToNot(HaveOccurred())
		Expect(sha256.Sum256(current)).To(Equal(sha256.Sum256(data)))
	})

	It("handles concurrent uploads from another device", func() {
		// the uploads come from a directory on another filesystem, so renaming them fails with EXDEV
		// and every upload falls back to copying at the same time
		srcDir, err := os.MkdirTemp("", "blobstore-src-*")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(srcDir)
		var srcStat, rootStat syscall.Stat_t
		Expect(syscall.Stat(srcDir, &srcStat)).To(Succeed())
		Expect(syscall.Stat(env.Root, &rootStat)).To(Succeed())
		if srcStat.Dev == rootStat.Dev {
			Skip("the temp dir is on the same device as the test root")
		}

		type upload struct {
			n      *node.Node
			source string
			data   []byte
		}
		uploads := []upload{}
		for i := 0; i < 8; i++ {
			n, source, data := newUpload(fmt.Sprintf("file%d.bin", i), 64*1024, srcDir)
			uploads = append(uploads, upload{n, source, data})
		}

		wg := sync.WaitGroup{}
		errs := make([]error, len(uploads))
		for i, u := range uploads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = bs.Upload(u.n, u.source, "")
			}()
		}
		wg.Wait()

		for i, u := range uploads {
			Expect(errs[i]).ToNot(HaveOccurred())
			Expect(os.ReadFile(u.n.InternalPath())).To(Equal(u.data))
		}
	})
})
