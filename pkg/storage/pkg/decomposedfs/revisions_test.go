// Copyright 2018-2021 CERN
// Copyright 2025 OpenCloud GmbH <mail@opencloud.eu>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy of this license at
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
// granted to it by virtue of its status as an Intergovernmental Organization.

package decomposedfs_test

import (
	"os"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DeleteRevision", func() {
	var (
		env     *helpers.DecomposedTestEnv
		file    *node.Node
		fileRef *provider.Reference
	)

	// the 2024-01-01 revision captures "orig-blobid", the 2024-06-01 revision
	// captures "second-blobid" and the node is left with "bad-blobid",
	// simulating a stuck upload
	const (
		rev1 = "2024-01-01T00:00:00Z"
		rev2 = "2024-06-01T00:00:00Z"
	)

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		dir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1",
		})
		Expect(err).ToNot(HaveOccurred())

		file, err = env.CreateTestFile("delrev-file", "orig-blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
		fileRef = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/delrev-file",
		}

		_, err = env.Tree.CreateRevision(env.Ctx, file, rev1)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "second-blobid"
		file.Blobsize = 2000
		Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())

		_, err = env.Tree.CreateRevision(env.Ctx, file, rev2)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "bad-blobid"
		file.Blobsize = 42
		Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	allowDeletePermissions := func() {
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			RestoreFileVersion: true,
		}, nil)
	}

	It("deletes the revision's own blob, not the node's current blob", func() {
		allowDeletePermissions()
		var deleted []string
		env.Blobstore.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
			deleted = append(deleted, args.Get(0).(*node.Node).BlobID)
		}).Return(nil)

		err := env.Fs.DeleteRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1)
		Expect(err).ToNot(HaveOccurred())

		// the deleted revision file is gone, the later revision survives
		_, statErr := os.Stat(env.Lookup.VersionPath(file.SpaceID, file.ID, rev1))
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		_, statErr = os.Stat(env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))
		Expect(statErr).ToNot(HaveOccurred())

		// the node's current content is untouched
		restored, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.BlobID).To(Equal("bad-blobid"))
		Expect(restored.Blobsize).To(Equal(int64(42)))

		// only the revision's own blob was deleted
		Expect(deleted).To(Equal([]string{"orig-blobid"}))
	})

	It("is a no-op when the revision does not exist", func() {
		allowDeletePermissions()

		err := env.Fs.DeleteRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+"2023-01-01T00:00:00Z")
		Expect(err).ToNot(HaveOccurred())

		restored, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.BlobID).To(Equal("bad-blobid"))
		_, statErr := os.Stat(env.Lookup.VersionPath(file.SpaceID, file.ID, rev1))
		Expect(statErr).ToNot(HaveOccurred())
		_, statErr = os.Stat(env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))
		Expect(statErr).ToNot(HaveOccurred())
	})

	It("does not unmark a stuck node when deleting an older revision", func() {
		allowDeletePermissions()
		env.Blobstore.On("Delete", mock.Anything).Return(nil)

		Expect(file.SetXattr(env.Ctx, prefixes.StatusPrefix, []byte(node.ProcessingStatus+"upload-1"))).To(Succeed())
		Expect(file.IsProcessing(env.Ctx)).To(BeTrue())

		err := env.Fs.DeleteRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1)
		Expect(err).ToNot(HaveOccurred())

		restored, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.BlobID).To(Equal("bad-blobid"))
		Expect(restored.IsProcessing(env.Ctx)).To(BeTrue())
		_, statErr := os.Stat(env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))
		Expect(statErr).ToNot(HaveOccurred())
	})
})
