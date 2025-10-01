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
	"os"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests pin the posix layout for revision deletion and revert: on
// posix, revisions live under <spaceRoot>/.oc-nodes/<pathify(id)>.REV.<ts>
// rather than next to the node file, so the revision path must be resolved
// through the layout-aware lookup (VersionPath) or DeleteRevision would find
// nothing and purge the node instead of restoring it. The file is its own
// blob on posix, so the blob store is not involved.
var _ = Describe("DeleteRevision (posix layout)", func() {
	var (
		dir1    *node.Node
		file    *node.Node
		fileRef *provider.Reference
	)

	const (
		rev1 = "2024-01-01T00:00:00Z"
		rev2 = "2024-06-01T00:00:00Z"
	)

	BeforeEach(func() {
		var err error
		dir1, err = non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       "/dir1",
		})
		Expect(err).ToNot(HaveOccurred())

		file, err = non_watching_env.CreateTestFile("revision-file", "orig-blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
		fileRef = &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       "/dir1/revision-file",
		}
	})

	// createTwoRevisions captures "orig-blobid" in rev1 and "second-blobid" in
	// rev2, then leaves the node with "bad-blobid", simulating a stuck upload
	createTwoRevisions := func() {
		_, err := non_watching_env.Tree.CreateRevision(non_watching_env.Ctx, file, rev1)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "second-blobid"
		file.Blobsize = 2000
		Expect(file.SetXattrs(file.NodeMetadata(non_watching_env.Ctx))).To(Succeed())

		_, err = non_watching_env.Tree.CreateRevision(non_watching_env.Ctx, file, rev2)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "bad-blobid"
		file.Blobsize = 42
		Expect(file.SetXattrs(file.NodeMetadata(non_watching_env.Ctx))).To(Succeed())
	}

	allowDeletePermissions := func() {
		non_watching_env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		non_watching_env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			RestoreFileVersion: true,
		}, nil)
	}

	It("deletes a specific revision without touching the node", func() {
		createTwoRevisions()
		allowDeletePermissions()

		err := non_watching_env.Fs.DeleteRevision(non_watching_env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1)
		Expect(err).ToNot(HaveOccurred())

		// the deleted revision file is gone, the later revision survives
		_, statErr := os.Stat(non_watching_env.Lookup.VersionPath(file.SpaceID, file.ID, rev1))
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		_, statErr = os.Stat(non_watching_env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))
		Expect(statErr).ToNot(HaveOccurred())

		// the node's current content is untouched
		restored, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.Exists).To(BeTrue())
		Expect(restored.BlobID).To(Equal("bad-blobid"))
	})

	It("is a no-op when the revision does not exist", func() {
		createTwoRevisions()
		allowDeletePermissions()

		err := non_watching_env.Fs.DeleteRevision(non_watching_env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+"2023-01-01T00:00:00Z")
		Expect(err).ToNot(HaveOccurred())

		restored, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.Exists).To(BeTrue())
		Expect(restored.BlobID).To(Equal("bad-blobid"))
		_, statErr := os.Stat(non_watching_env.Lookup.VersionPath(file.SpaceID, file.ID, rev1))
		Expect(statErr).ToNot(HaveOccurred())
		_, statErr = os.Stat(non_watching_env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))
		Expect(statErr).ToNot(HaveOccurred())
	})

	It("restores the latest revision instead of purging the node", func() {
		origBlob, origSize := file.BlobID, file.Blobsize
		Expect(origBlob).To(Equal("orig-blobid"))

		_, err := non_watching_env.Tree.CreateRevision(non_watching_env.Ctx, file, rev1)
		Expect(err).ToNot(HaveOccurred())

		// simulate a new upload overwriting the node's blob metadata
		file.BlobID = "bad-blobid"
		file.Blobsize = 42
		Expect(file.SetXattrs(file.NodeMetadata(non_watching_env.Ctx))).To(Succeed())

		_, err = file.DeleteRevision(non_watching_env.Ctx, "")
		Expect(err).ToNot(HaveOccurred())

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

		_, err = file.DeleteRevision(non_watching_env.Ctx, "")
		Expect(err).ToNot(HaveOccurred())

		after, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(after.Exists).To(BeFalse())
	})

	It("reverts the upload that created the given revision", func() {
		origBlob, origSize := file.BlobID, file.Blobsize

		createTwoRevisions()

		Expect(file.RevertUpload(non_watching_env.Ctx, rev1)).To(Succeed())

		restored, err := node.ReadNode(non_watching_env.Ctx, non_watching_env.Lookup, file.SpaceID, file.ID, "", false, nil, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.Exists).To(BeTrue())
		Expect(restored.BlobID).To(Equal(origBlob))
		Expect(restored.Blobsize).To(Equal(origSize))
	})
})
