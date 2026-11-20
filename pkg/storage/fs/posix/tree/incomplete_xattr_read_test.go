// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"os"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	"github.com/pkg/xattr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// On NFS a read of the xattrs can come back incomplete without an error, e.g.
// right after the search service wrote the extracted facets of an upload. If the
// incomplete read ends up in the file metadata cache the node vanishes with
// "Missing parent ID on node" although its xattrs on disk are complete.
var _ = Describe("an incomplete xattr read (posix, not watching)", func() {
	var (
		dir1    *node.Node
		file    *node.Node
		path    string
		fileRef *provider.Reference
	)

	BeforeEach(func() {
		env := non_watching_env
		var err error
		dir1, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"})
		Expect(err).ToNot(HaveOccurred())

		file, err = env.CreateTestFile("vanishing.jpg", "blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
		path = file.InternalPath()
		fileRef = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/vanishing.jpg"}
		DeferCleanup(func() { _ = os.Remove(path) })
	})

	// expectVisible checks every way the reported node broke: reading it, listing
	// its parent and resolving it by path, which uploads and deletes rely on
	expectVisible := func() {
		env := non_watching_env

		parentID, err := xattr.Get(path, prefixes.ParentidAttr)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		ExpectWithOffset(1, string(parentID)).To(Equal(dir1.ID), "the xattrs on disk are complete")

		n, err := node.ReadNode(env.Ctx, env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		ExpectWithOffset(1, n.ParentID).To(Equal(dir1.ID))

		children, err := env.Tree.ListFolder(env.Ctx, dir1)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		names := []string{}
		for _, c := range children {
			names = append(names, c.Name)
		}
		ExpectWithOffset(1, names).To(ContainElement("vanishing.jpg"))

		_, err = env.Lookup.NodeFromResource(env.Ctx, fileRef)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
	}

	It("does not cache it when a write refreshes the cache", func() {
		env := non_watching_env

		By("simulating a view of the file that lacks its xattrs")
		Expect(os.Rename(path, path+".real")).To(Succeed())
		Expect(os.WriteFile(path, []byte("stale"), 0600)).To(Succeed())

		By("writing the search facets, which refreshes the cache from that view")
		// this is how SetArbitraryMetadata writes them
		n, err := node.ReadNode(env.Ctx, env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.SetXattrsWithContext(env.Ctx, map[string][]byte{
			prefixes.MetadataPrefix + "libre.graph.image.width":  []byte("474"),
			prefixes.MetadataPrefix + "libre.graph.image.height": []byte("316"),
		})).To(Succeed())

		By("showing the whole file again")
		Expect(os.Rename(path+".real", path)).To(Succeed())

		expectVisible()
	})

	It("recovers from an incomplete read that is already cached", func() {
		env := non_watching_env

		// e.g. cached by a version without the fix, the entry lives in a persistent store
		cfg := env.Options.FileMetadataCache
		if cfg.Store == "" {
			cfg = cache.Config{Database: env.Options.Root}
		}
		Expect(cache.GetFileMetadataCache(cfg).PushToCache(file.SpaceID+"/"+file.ID, map[string][]byte{
			prefixes.MetadataPrefix + "libre.graph.image.width":  []byte("474"),
			prefixes.MetadataPrefix + "libre.graph.image.height": []byte("316"),
		})).To(Succeed())

		expectVisible()
	})
})
