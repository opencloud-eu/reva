// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"bytes"
	"io"
	"sync"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests for the triggers of the revision cleanup (see package revisioncleanup): uploads that
// replace a file, and restores. The cleanup runs right away (revisions_cleanup_async false),
// so that its effect can be checked as soon as the operation returns.
var _ = Describe("Revision cleanup", func() {
	var (
		env       *helpers.DecomposedTestEnv
		file      *node.Node
		fileRef   *provider.Reference
		deletedMu sync.Mutex
		deleted   []string // the blob ids deleted from the blobstore
	)

	newEnv := func(strategy string) {
		var err error
		env, err = helpers.NewTestEnv(map[string]any{
			"revisions_cleanup_strategy": strategy,
			"revisions_cleanup_async":    false,
		})
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			GetQuota:           true,
			InitiateFileUpload: true,
			ListContainer:      true,
			ListFileVersions:   true,
			RestoreFileVersion: true,
		}, nil)
		env.Blobstore.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		env.Blobstore.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
			deletedMu.Lock()
			defer deletedMu.Unlock()
			deleted = append(deleted, args.Get(0).(*node.Node).BlobID)
		}).Return(nil)

		fileRef = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/file1"}
		file, err = env.Lookup.NodeFromResource(env.Ctx, fileRef)
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		deleted = nil
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	deletedBlobs := func() []string {
		deletedMu.Lock()
		defer deletedMu.Unlock()
		return append([]string{}, deleted...)
	}

	revisionsOf := func(n *node.Node) []*provider.FileVersion {
		revs, err := env.Tree.ListRevisionsUnchecked(env.Ctx, n)
		Expect(err).ToNot(HaveOccurred())
		return revs
	}

	upload := func(content []byte) {
		uploadIds, err := env.Fs.InitiateUpload(env.Ctx, fileRef, int64(len(content)), map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		_, err = env.Fs.Upload(env.Ctx, storage.UploadRequest{
			Ref:    &provider.Reference{Path: "/" + uploadIds["simple"]},
			Body:   io.NopCloser(bytes.NewReader(content)),
			Length: int64(len(content)),
		}, nil)
		Expect(err).ToNot(HaveOccurred())
	}

	Describe("uploads", func() {
		It("keep the revision of the replaced content without a strategy", func() {
			newEnv("")

			upload([]byte("new content"))

			Expect(revisionsOf(file)).To(HaveLen(1))
			Expect(deletedBlobs()).To(BeEmpty())
		})

		It("trigger the cleanup when they replace a file", func() {
			// "0" deletes all revisions, including the one this upload just created
			newEnv("0")

			upload([]byte("new content"))

			Expect(revisionsOf(file)).To(BeEmpty())
			// the blob of the replaced content went with its revision
			Expect(deletedBlobs()).To(ConsistOf("file1-blobid"))
		})

		It("trigger the cleanup when MarkProcessing clears the processing mark of their session", func() {
			newEnv("0")
			// the upload pipeline marks the node, and its upload replaces the content, keeping
			// the previous content as a revision
			Expect(env.Fs.MarkProcessing(env.Ctx, fileRef, true, "session-1")).To(Succeed())
			_, err := env.Tree.CreateRevision(env.Ctx, file, "2024-01-01T00:00:00Z")
			Expect(err).ToNot(HaveOccurred())
			file.BlobID = "committed-blobid"
			Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())
			Expect(revisionsOf(file)).To(HaveLen(1))

			// clearing the mark of another session neither clears it nor triggers the cleanup
			Expect(env.Fs.MarkProcessing(env.Ctx, fileRef, false, "session-2")).To(Succeed())
			Expect(revisionsOf(file)).To(HaveLen(1))

			Expect(env.Fs.MarkProcessing(env.Ctx, fileRef, false, "session-1")).To(Succeed())
			Expect(revisionsOf(file)).To(BeEmpty())
			// the blob of the replaced content went with its revision
			Expect(deletedBlobs()).To(ConsistOf("file1-blobid"))
		})
	})

	Describe("restores", func() {
		const (
			rev1 = "2024-01-01T00:00:00Z"
			rev2 = "2024-06-01T00:00:00Z"
		)

		// gives file two revisions: rev1 with "file1-blobid", rev2 with "second-blobid",
		// and leaves the file itself with "current-blobid"
		createRevisions := func() {
			_, err := env.Tree.CreateRevision(env.Ctx, file, rev1)
			Expect(err).ToNot(HaveOccurred())
			file.BlobID = "second-blobid"
			Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())
			_, err = env.Tree.CreateRevision(env.Ctx, file, rev2)
			Expect(err).ToNot(HaveOccurred())
			file.BlobID = "current-blobid"
			Expect(file.SetXattrs(file.NodeMetadata(env.Ctx))).To(Succeed())
		}

		It("trigger the cleanup, but keep the revision with the content from before the restore", func() {
			// "0" would delete all revisions
			newEnv("0")
			createRevisions()

			Expect(env.Fs.RestoreRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1)).To(Succeed())

			// rev1 was consumed by the restore, rev2 was deleted by the cleanup, only the
			// revision created by the restore remains, holding the content from before it
			revs := revisionsOf(file)
			Expect(revs).To(HaveLen(1))
			blobID, _, err := env.Lookup.ReadBlobIDAndSizeAttr(env.Ctx, node.NewBaseNode(file.SpaceID, revs[0].Key, env.Lookup), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(blobID).To(Equal("current-blobid"))
			Expect(deletedBlobs()).To(ConsistOf("second-blobid"))

			restored, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.BlobID).To(Equal("file1-blobid"))
		})
	})

	Describe("configuration", func() {
		It("rejects an invalid strategy at startup", func() {
			_, err := helpers.NewTestEnv(map[string]any{"revisions_cleanup_strategy": "whenever"})
			Expect(err).To(HaveOccurred())
		})
	})
})
