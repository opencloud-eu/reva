// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package tree_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

// These tests pin the posix layout regression: on posix, revisions live under
// <spaceRoot>/.oc-nodes/<pathify(id)>.REV.<ts> rather than next to the node
// file, so the latest-revision lookup must be layout-aware (VersionPath) or
// RevertCurrentRevision would find nothing and purge the node instead of
// restoring it.
var _ = Describe("RevertCurrentRevision (posix layout)", func() {
	var (
		dir1 *node.Node
		file *node.Node
	)

	BeforeEach(func() {
		var err error
		dir1, err = non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       "/dir1",
		})
		Expect(err).ToNot(HaveOccurred())

		file, err = non_watching_env.CreateTestFile("revert-file", "revert-blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
	})

	It("restores the latest revision instead of purging the node", func() {
		origBlob, origSize := file.BlobID, file.Blobsize
		Expect(origBlob).To(Equal("revert-blobid"))

		_, err := non_watching_env.Tree.CreateRevision(non_watching_env.Ctx, file, "2024-01-01T00:00:00Z")
		Expect(err).ToNot(HaveOccurred())

		// simulate a new upload overwriting the node's blob metadata
		file.BlobID = "bad-blobid"
		file.Blobsize = 42
		Expect(file.SetXattrs(file.NodeMetadata(non_watching_env.Ctx))).To(Succeed())

		Expect(file.RevertCurrentRevision(non_watching_env.Ctx)).To(Succeed())

		restored, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.Exists).To(BeTrue())
		Expect(restored.BlobID).To(Equal(origBlob))
		Expect(restored.Blobsize).To(Equal(origSize))
	})

	It("purges the node when there is no revision", func() {
		before, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(before.Exists).To(BeTrue())

		Expect(file.RevertCurrentRevision(non_watching_env.Ctx)).To(Succeed())

		after, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(after.Exists).To(BeFalse())
	})
})
