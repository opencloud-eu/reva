// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package blobstore_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	posixblobstore "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

var _ = Describe("Blobstore rename fallback", func() {
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

	// an upload that fails to rename with EXDEV switches the blobstore to copying while other
	// uploads are reading the flag. run with -race to catch unsynchronized access
	It("lets concurrent uploads read the flag while it is switched to copying", func() {
		type upload struct {
			n      *node.Node
			source string
			data   []byte
		}
		uploads := []upload{}
		for i := 0; i < 8; i++ {
			name := fmt.Sprintf("file%d.bin", i)
			n, err := env.CreateTestFile(name, "", env.SpaceRootRes.OpaqueId, env.SpaceRootRes.SpaceId, 64*1024)
			Expect(err).ToNot(HaveOccurred())

			data := make([]byte, 64*1024)
			_, err = rand.Read(data)
			Expect(err).ToNot(HaveOccurred())

			source := filepath.Join(env.Root, "upload-source-"+name)
			Expect(os.WriteFile(source, data, 0600)).To(Succeed())
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.SetCanUseRenameForUpload(false)
		}()
		wg.Wait()

		for i, u := range uploads {
			Expect(errs[i]).ToNot(HaveOccurred())
			Expect(os.ReadFile(u.n.InternalPath())).To(Equal(u.data))
		}
	})
})
