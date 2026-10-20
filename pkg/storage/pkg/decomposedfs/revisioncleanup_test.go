// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs"
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

	newEnvWithOptions := func(options map[string]any) {
		var err error
		env, err = helpers.NewTestEnv(options)
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
	newEnvWith := func(strategy string, async bool) {
		newEnvWithOptions(map[string]any{
			"revisions_cleanup_strategy": strategy,
			"revisions_cleanup_async":    async,
		})
	}

	// the cleanup runs right away, so that its effect can be checked as soon as the
	// triggering operation returns
	newEnv := func(strategy string) { newEnvWith(strategy, false) }

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

	uploadTo := func(ref *provider.Reference, content []byte) {
		uploadIds, err := env.Fs.InitiateUpload(env.Ctx, ref, int64(len(content)), map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		_, err = env.Fs.Upload(env.Ctx, storage.UploadRequest{
			Ref:    &provider.Reference{Path: "/" + uploadIds["simple"]},
			Body:   io.NopCloser(bytes.NewReader(content)),
			Length: int64(len(content)),
		}, nil)
		Expect(err).ToNot(HaveOccurred())
	}
	upload := func(content []byte) { uploadTo(fileRef, content) }

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

		It("trigger the cleanup in the background with revisions_cleanup_async", func() {
			newEnvWith("0", true)

			upload([]byte("new content"))

			Eventually(func() []*provider.FileVersion { return revisionsOf(file) }).Should(BeEmpty())
			Eventually(deletedBlobs).Should(ConsistOf("file1-blobid"))
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

		It("don't hold the node lock while waiting for room in the background cleanup's queue", func() {
			// a single worker and a single queue slot, so that the restore has to wait for room
			newEnvWithOptions(map[string]any{
				"revisions_cleanup_strategy":         "0",
				"revisions_cleanup_async":            true,
				"revisions_cleanup_async_workers":    1,
				"revisions_cleanup_async_queue_size": 1,
				"revisions_cleanup_async_queue_full": "block",
			})
			createRevisions()
			subdir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"})
			Expect(err).ToNot(HaveOccurred())
			for _, name := range []string{"busy", "queued"} {
				_, err := env.CreateTestFile(name, name+"-blobid", subdir1.ID, subdir1.SpaceID, 10)
				Expect(err).ToNot(HaveOccurred())
			}
			// the cleanup of "busy" keeps the only worker busy until released
			busy, release := make(chan struct{}), make(chan struct{})
			DeferCleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			env.Blobstore.ExpectedCalls = nil
			env.Blobstore.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			env.Blobstore.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
				if args.Get(0).(*node.Node).BlobID == "busy-blobid" {
					close(busy)
					<-release
				}
			}).Return(nil)

			uploadTo(&provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1/busy"}, []byte("new"))
			// once the worker is busy with "busy", "queued" fills the only queue slot
			Eventually(busy).Should(BeClosed())
			uploadTo(&provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1/queued"}, []byte("new"))

			restored := make(chan error, 1)
			go func() { restored <- env.Fs.RestoreRevision(env.Ctx, fileRef, file.ID+node.RevisionIDDelimiter+rev1) }()
			time.Sleep(200 * time.Millisecond) // the restore waits for room in the queue
			Consistently(restored).ShouldNot(Receive(), "the restore didn't wait for room in the queue")

			// the restore must not hold the lock of the file meanwhile: a worker could be waiting for it
			locked := make(chan func() error, 1)
			go func() {
				n, err := env.Lookup.NodeFromResource(env.Ctx, fileRef)
				if err != nil {
					return
				}
				if unlock, err := env.Lookup.MetadataBackend().Lock(n); err == nil {
					locked <- unlock
				}
			}()
			var unlock func() error
			Eventually(locked).Should(Receive(&unlock), "the restore holds the lock of the file while it waits")
			Expect(unlock()).To(Succeed())

			close(release)
			Eventually(restored, 10*time.Second).Should(Receive(Succeed()))
		})
	})

	Describe("configuration", func() {
		It("rejects an invalid strategy at startup", func() {
			_, err := helpers.NewTestEnv(map[string]any{"revisions_cleanup_strategy": "whenever"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid background cleanup settings at startup", func() {
			for _, config := range []map[string]any{
				{"revisions_cleanup_strategy": "auto", "revisions_cleanup_async_queue_full": "wait"},
				{"revisions_cleanup_strategy": "auto", "revisions_cleanup_async_workers": -1},
				{"revisions_cleanup_strategy": "auto", "revisions_cleanup_async_queue_size": -1},
			} {
				_, err := helpers.NewTestEnv(config)
				Expect(err).To(HaveOccurred(), "%v", config)
			}
		})

		It("stops the background workers again when the storage can't be set up", func() {
			// the cleanup's workers, by their function in the goroutine dump
			workers := func() int {
				buf := make([]byte, 1<<22)
				return strings.Count(string(buf[:runtime.Stack(buf, true)]), "revisioncleanup.(*Service).work(")
			}
			before := workers()

			// async uploads need an event stream: New fails after the cleanup has started
			_, err := decomposedfs.NewDefault(map[string]any{
				"root":                            GinkgoT().TempDir(),
				"asyncfileuploads":                true,
				"revisions_cleanup_strategy":      "auto",
				"revisions_cleanup_async":         true,
				"revisions_cleanup_async_workers": 3,
			}, nil, nil, &zerolog.Logger{})
			Expect(err).To(MatchError(ContainSubstring("need nats")))

			Eventually(workers).Should(Equal(before))
		})
	})
})
