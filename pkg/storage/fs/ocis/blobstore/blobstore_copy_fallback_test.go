// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package blobstore_test

import (
	"os"
	"path"
	"path/filepath"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/ocis/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/tests/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Blobstore with copy fallback", func() {
	var (
		tmpRoot   string
		sourceDir string
		blobNode  *node.Node
		blobPath  string
		data      []byte

		bs *blobstore.Blobstore
	)

	BeforeEach(func() {
		var err error
		tmpRoot, err = helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		// Upload renames the source into place whenever it can. Only a source on another
		// device makes it fall back to copying, which is the path that used to leak the
		// file descriptor of the blob file.
		sourceDir, err = helpers.CrossDeviceTempDir(tmpRoot, "reva-unit-tests-*-src")
		if err != nil {
			Skip(err.Error())
		}

		data = []byte("1234567890")
		blobNode = &node.Node{
			SpaceID: "wonderfullspace",
			BlobID:  "huuuuugeblob",
		}
		blobPath = path.Join(tmpRoot, "spaces", "wo", "nderfullspace", "blobs", "hu", "uu", "uu", "ge", "blob")

		bs, err = blobstore.New(tmpRoot)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if sourceDir != "" {
			os.RemoveAll(sourceDir)
		}
		if tmpRoot != "" {
			os.RemoveAll(tmpRoot)
		}
	})

	It("copies the blob and leaves no file descriptor behind", func() {
		source := filepath.Join(sourceDir, "blobsrc")
		Expect(os.WriteFile(source, data, 0700)).To(Succeed())

		Expect(bs.Upload(blobNode, source)).To(Succeed())

		// a rename would have moved the source away, a copy leaves it where it is
		_, err := os.Stat(source)
		Expect(err).ToNot(HaveOccurred())

		written, err := os.ReadFile(blobPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(Equal(data))

		leaked, err := helpers.OpenFDsPointingTo(blobPath)
		if err != nil {
			Skip(err.Error())
		}
		Expect(leaked).To(BeEmpty())
	})

	It("closes the blob file when the copy fails", func() {
		// a directory can be opened for reading but not read, so the copy breaks after the
		// blob file has been created
		source := filepath.Join(sourceDir, "not-a-file")
		Expect(os.MkdirAll(source, 0700)).To(Succeed())

		Expect(bs.Upload(blobNode, source)).To(HaveOccurred())

		leaked, err := helpers.OpenFDsPointingTo(blobPath)
		if err != nil {
			Skip(err.Error())
		}
		Expect(leaked).To(BeEmpty())
	})
})
