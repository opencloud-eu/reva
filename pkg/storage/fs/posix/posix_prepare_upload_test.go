// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

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
