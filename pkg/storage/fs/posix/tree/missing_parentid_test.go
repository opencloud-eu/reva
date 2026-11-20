// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"os"
	"strings"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	"github.com/pkg/xattr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A file whose xattrs on disk lost the parent id is still known to the id cache,
// so it is never assimilated again: it vanishes from listings and its path can
// neither be uploaded to nor deleted. The parent id is right there in the xattrs
// of the parent folder, so posix should re-assimilate the file instead.
// See https://github.com/opencloud-eu/reva/issues/1047
var _ = Describe("a file without a parent id on disk (posix, not watching)", func() {
	var (
		dir1    *node.Node
		file    *node.Node
		path    string
		fileRef *provider.Reference
	)

	// forget forgets the cached metadata of the file, so reads go to disk
	forget := func() {
		env := non_watching_env
		cfg := env.Options.FileMetadataCache
		if cfg.Store == "" {
			cfg = cache.Config{Database: env.Options.Root}
		}
		ExpectWithOffset(1, cache.GetFileMetadataCache(cfg).RemoveMetadata(file.SpaceID+"/"+file.ID)).To(Succeed())
	}

	listed := func() bool {
		env := non_watching_env
		children, err := env.Tree.ListFolder(env.Ctx, dir1)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		for _, c := range children {
			if c.Name == "orphan.jpg" {
				return true
			}
		}
		return false
	}

	BeforeEach(func() {
		env := non_watching_env
		var err error
		dir1, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"})
		Expect(err).ToNot(HaveOccurred())

		file, err = env.CreateTestFile("orphan.jpg", "blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
		path = file.InternalPath()
		fileRef = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/orphan.jpg"}
		DeferCleanup(func() { _ = os.Remove(path) })
	})

	Context("when only the parent id is missing", func() {
		BeforeEach(func() {
			Expect(xattr.Remove(path, prefixes.ParentidAttr)).To(Succeed())
			forget()
		})

		It("lists the file and keeps its id", func() {
			Expect(listed()).To(BeTrue())

			parentID, err := xattr.Get(path, prefixes.ParentidAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(parentID)).To(Equal(dir1.ID))

			id, err := xattr.Get(path, prefixes.IDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(id)).To(Equal(file.ID), "shares and links point to the id")
		})

		It("resolves the file by path, as an upload or a delete would", func() {
			env := non_watching_env
			n, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeTrue())
			Expect(n.ID).To(Equal(file.ID))
			Expect(n.ParentID).To(Equal(dir1.ID))
		})

		It("reads the node by id", func() {
			env := non_watching_env
			n, err := node.ReadNode(env.Ctx, env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.ParentID).To(Equal(dir1.ID))
		})
	})

	Context("when all attributes of ours are missing but the id cache still knows the path", func() {
		BeforeEach(func() {
			names, err := xattr.List(path)
			Expect(err).ToNot(HaveOccurred())
			for _, name := range names {
				if strings.HasPrefix(name, prefixes.OcPrefix) {
					Expect(xattr.Remove(path, name)).To(Succeed())
				}
			}
			forget()
		})

		It("lists the file again", func() {
			Expect(listed()).To(BeTrue())

			parentID, err := xattr.Get(path, prefixes.ParentidAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(parentID)).To(Equal(dir1.ID))
		})

		It("resolves the file by path, as an upload or a delete would", func() {
			env := non_watching_env
			n, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeTrue())
			Expect(n.ParentID).To(Equal(dir1.ID))
		})
	})
})
