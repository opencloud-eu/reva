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

package posix_test

import (
	"os"
	"path/filepath"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PrepareUpload on posix", func() {
	var (
		env *posixhelpers.TestEnv
		ref *provider.Reference
	)

	oldContent := []byte("old content")

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
			&provider.ResourcePermissions{Stat: true, InitiateFileUpload: true}, nil)

		ref = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./target.txt"}
		Expect(env.Fs.TouchFile(env.Ctx, ref, false, "")).To(Succeed())

		// the file's content as it was before the upload
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(n.InternalPath(), oldContent, 0600)).To(Succeed())
		Expect(n.SetXattrsWithContext(env.Ctx, map[string][]byte{
			prefixes.BlobsizeAttr: []byte("11"),
		})).To(Succeed())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("snapshots the current content and keeps it in the live file until the commit", func() {
		result, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{
			NodeExisted: true,
			Size:        20,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.VersionCreated).To(BeTrue())

		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())

		// the revision holds a copy of the content the upload replaces
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		Expect(revisions).To(HaveLen(1))
		Expect(os.ReadFile(revisions[0])).To(Equal(oldContent))

		// the live file describes the upload, but its bytes are only replaced by CommitUpload
		Expect(n.BlobID).To(Equal("session-1"))
		Expect(n.Blobsize).To(Equal(int64(20)))
		Expect(os.ReadFile(n.InternalPath())).To(Equal(oldContent))
	})

	It("leaves no revision behind when a precondition fails", func() {
		_, err := env.Fs.PrepareUpload(env.Ctx, ref, "session-1", storage.UploadInfo{
			NodeExisted: true,
			Size:        20,
			IfMatch:     "wrong-etag",
		})
		Expect(err).To(HaveOccurred())

		n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		revisions, err := filepath.Glob(env.Lookup.VersionPath(n.SpaceID, n.ID, "*"))
		Expect(err).ToNot(HaveOccurred())
		Expect(revisions).To(BeEmpty())
	})
})
