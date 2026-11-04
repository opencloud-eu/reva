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
	"errors"
	"os"
	"path/filepath"
	"strings"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v2/pkg/store"
	"github.com/rs/zerolog"
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

var _ = Describe("a revision deleted by another process", func() {
	var (
		env     *helpers.DecomposedTestEnv
		file    *node.Node
		fileRef *provider.Reference
	)

	// the 2024-01-01 revision captures "orig-blobid", the 2024-06-01 revision
	// captures "second-blobid", and the node holds "current-blobid"
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

		file, err = env.CreateTestFile("stalerev-file", "orig-blobid", dir1.ID, dir1.SpaceID, 1234)
		Expect(err).ToNot(HaveOccurred())
		fileRef = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/stalerev-file",
		}

		_, err = env.Tree.CreateRevision(env.Ctx, file, rev1)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "second-blobid"
		file.Blobsize = 2000
		Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())

		_, err = env.Tree.CreateRevision(env.Ctx, file, rev2)
		Expect(err).ToNot(HaveOccurred())

		file.BlobID = "current-blobid"
		file.Blobsize = 3000
		Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:                 true,
			ListFileVersions:     true,
			InitiateFileDownload: true,
			RestoreFileVersion:   true,
		}, nil)

		// Delete rev1 the way another process does, e.g. `opencloud storage-users
		// revisions cleanup`: its files are gone, but this process's metadata cache
		// still has its entry, from reading it before (as listing the versions does).
		revNode := node.NewBaseNode(file.SpaceID, file.ID+node.RevisionIDDelimiter+rev1, env.Lookup)
		_, err = env.Lookup.MetadataBackend().All(env.Ctx, revNode)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Remove(revNode.InternalPath())).To(Succeed())
		Expect(os.Remove(env.Lookup.MetadataBackend().MetadataPath(revNode))).To(Succeed())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	revisionFiles := func() []string {
		paths, err := filepath.Glob(env.Lookup.VersionPath(file.SpaceID, file.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		var revisions []string
		for _, p := range paths {
			if !strings.HasSuffix(p, ".mpk") && !strings.HasSuffix(p, ".mlock") {
				revisions = append(revisions, filepath.Base(p))
			}
		}
		return revisions
	}

	It("is not restored, and the node keeps its content", func() {
		err := env.Fs.RestoreRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1)
		_, notFound := err.(errtypes.IsNotFound)
		Expect(notFound).To(BeTrue(), "expected a not found error, got %v", err)

		// the node still points to its own blob, not to the deleted revision's
		restored, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
		Expect(err).ToNot(HaveOccurred())
		Expect(restored.BlobID).To(Equal("current-blobid"))
		Expect(restored.Blobsize).To(Equal(int64(3000)))

		// and the failed restore left no revision of the current content behind
		Expect(revisionFiles()).To(ConsistOf(filepath.Base(env.Lookup.VersionPath(file.SpaceID, file.ID, rev2))))
	})

	It("is not downloaded", func() {
		env.Blobstore.On("Download", mock.Anything).Return(nil, errors.New("the blob was deleted")).Maybe()

		// the test env's tree checks no permissions (see helpers.NewTestEnv), so download
		// through a tree that does, with the mocked permissions
		log := zerolog.Nop()
		t := tree.New(env.Lookup, env.Blobstore, env.Options, permissions.NewPermissions(env.Permissions, nil), store.Create(), &log)
		_, _, err := t.DownloadRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1,
			func(*provider.ResourceInfo) bool { return true })
		_, notFound := err.(errtypes.IsNotFound)
		Expect(notFound).To(BeTrue(), "expected a not found error, got %v", err)
	})
})
