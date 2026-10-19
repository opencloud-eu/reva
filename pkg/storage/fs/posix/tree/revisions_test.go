// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"os"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Revisions", func() {
	var (
		n   *node.Node
		now = time.Now()
		// revisions of different ages, the ages end up in the revision keys
		ages = []time.Duration{time.Hour, 10 * 24 * time.Hour, 100 * 24 * time.Hour}
		keys []string
	)

	revisionNode := func(key string) *node.BaseNode {
		return node.NewBaseNode(n.SpaceID, key, non_watching_env.Lookup)
	}
	listKeys := func() []string {
		revs, err := non_watching_env.Tree.ListRevisionsUnchecked(non_watching_env.Ctx, n)
		Expect(err).ToNot(HaveOccurred())
		keys := make([]string, len(revs))
		for i, r := range revs {
			keys[i] = r.Key
		}
		return keys
	}

	BeforeEach(func() {
		env := non_watching_env
		dir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "dir1",
		})
		Expect(err).ToNot(HaveOccurred())
		n, err = env.CreateTestFile(uuid.NewString(), "", dir1.ID, dir1.SpaceID, 16)
		Expect(err).ToNot(HaveOccurred())

		keys = nil
		for _, age := range ages {
			version := now.Add(-age).UTC().Format(time.RFC3339Nano)
			_, err := env.Tree.CreateRevision(env.Ctx, n, version)
			Expect(err).ToNot(HaveOccurred())
			keys = append(keys, n.ID+node.RevisionIDDelimiter+version)
		}
	})

	Describe("ListRevisionsUnchecked", func() {
		It("lists all revisions", func() {
			Expect(listKeys()).To(ConsistOf(keys))
		})
	})

	Describe("revisioncleanup.PurgeRevisionUnchecked", func() {
		It("removes the revision and its metadata", func() {
			rn := revisionNode(keys[0])
			Expect(rn.InternalPath()).To(BeAnExistingFile())

			Expect(revisioncleanup.PurgeRevisionUnchecked(non_watching_env.Ctx, non_watching_env.Tree, n, keys[0])).To(Succeed())

			Expect(rn.InternalPath()).ToNot(BeAnExistingFile())
			Expect(non_watching_env.Lookup.MetadataBackend().MetadataPath(rn)).ToNot(BeAnExistingFile())
			Expect(non_watching_env.Lookup.MetadataBackend().LockfilePath(rn)).ToNot(BeAnExistingFile())
			_, err := non_watching_env.Lookup.MetadataBackend().Get(non_watching_env.Ctx, rn, prefixes.BlobsizeAttr)
			Expect(err).To(HaveOccurred())
			Expect(listKeys()).To(ConsistOf(keys[1:]))
		})

		It("leaves the current version alone", func() {
			Expect(revisioncleanup.PurgeRevisionUnchecked(non_watching_env.Ctx, non_watching_env.Tree, n, keys[0])).To(Succeed())

			fi, err := os.Stat(n.InternalPath())
			Expect(err).ToNot(HaveOccurred())
			Expect(fi.Size()).To(Equal(int64(16)))
		})

		It("rejects revision keys of other nodes", func() {
			other := &node.Node{BaseNode: node.BaseNode{SpaceID: n.SpaceID, ID: uuid.NewString()}}
			Expect(revisioncleanup.PurgeRevisionUnchecked(non_watching_env.Ctx, non_watching_env.Tree, other, keys[0])).ToNot(Succeed())
			Expect(revisioncleanup.PurgeRevisionUnchecked(non_watching_env.Ctx, non_watching_env.Tree, n, n.ID)).ToNot(Succeed())
			Expect(listKeys()).To(ConsistOf(keys))
		})
	})
})
