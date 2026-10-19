// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"fmt"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Revisions", func() {
	var (
		env *helpers.DecomposedTestEnv
		n   *node.Node
		now = time.Now()
		// revisions of different ages, the ages end up in the revision keys
		ages = []time.Duration{time.Hour, 10 * 24 * time.Hour, 100 * 24 * time.Hour}
		keys []string
	)

	revisionNode := func(key string) *node.BaseNode {
		return node.NewBaseNode(n.SpaceID, key, env.Lookup)
	}
	blobOfRevision := func(i int) string {
		return fmt.Sprintf("revision-%d-blobid", i)
	}
	listKeys := func() []string {
		revs, err := env.Tree.ListRevisionsUnchecked(env.Ctx, n)
		Expect(err).ToNot(HaveOccurred())
		keys := make([]string, len(revs))
		for i, r := range revs {
			keys[i] = r.Key
		}
		return keys
	}
	expectBlobDeletion := func(blobIDs ...string) {
		for _, blobID := range blobIDs {
			env.Blobstore.On("Delete", mock.MatchedBy(func(bn *node.Node) bool {
				return bn.BlobID == blobID && bn.SpaceID == n.SpaceID
			})).Return(nil).Once()
		}
	}

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		n, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "dir1/file1",
		})
		Expect(err).ToNot(HaveOccurred())

		keys = nil
		for i, age := range ages {
			version := now.Add(-age).UTC().Format(time.RFC3339Nano)
			_, err := env.Tree.CreateRevision(env.Ctx, n, version)
			Expect(err).ToNot(HaveOccurred())
			key := n.ID + node.RevisionIDDelimiter + version
			// give every revision its own blob, CreateRevision copies the one of the current node
			Expect(env.Lookup.MetadataBackend().Set(env.Ctx, revisionNode(key), prefixes.BlobIDAttr, []byte(blobOfRevision(i)))).To(Succeed())
			keys = append(keys, key)
		}
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	Describe("ListRevisionsUnchecked", func() {
		It("lists all revisions", func() {
			Expect(listKeys()).To(ConsistOf(keys))
		})
	})

	Describe("revisioncleanup.PurgeRevisionUnchecked", func() {
		It("removes the revision, its metadata and its blob", func() {
			expectBlobDeletion(blobOfRevision(0))

			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0])).To(Succeed())

			rn := revisionNode(keys[0])
			Expect(rn.InternalPath()).ToNot(BeAnExistingFile())
			Expect(env.Lookup.MetadataBackend().MetadataPath(rn)).ToNot(BeAnExistingFile())
			Expect(env.Lookup.MetadataBackend().LockfilePath(rn)).ToNot(BeAnExistingFile())
			Expect(listKeys()).To(ConsistOf(keys[1:]))
			env.Blobstore.AssertExpectations(GinkgoT())
		})

		It("leaves the current version alone", func() {
			expectBlobDeletion(blobOfRevision(0))

			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0])).To(Succeed())

			Expect(n.InternalPath()).To(BeAnExistingFile())
			blobID, _, err := env.Lookup.ReadBlobIDAndSizeAttr(env.Ctx, n, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(blobID).To(Equal("file1-blobid"))
		})

		It("rejects revision keys of other nodes", func() {
			other := &node.Node{BaseNode: node.BaseNode{SpaceID: n.SpaceID, ID: "some-other-node"}}
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, other, keys[0])).ToNot(Succeed())
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, n.ID)).ToNot(Succeed())
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0]+"/../x")).ToNot(Succeed())
			Expect(listKeys()).To(ConsistOf(keys))
			env.Blobstore.AssertNotCalled(GinkgoT(), "Delete", mock.Anything)
		})
	})
})
