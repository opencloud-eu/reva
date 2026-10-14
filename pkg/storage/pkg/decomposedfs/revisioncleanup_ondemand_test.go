// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/spaceidindex"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Tests for the on-demand revision cleanup of whole spaces, folders or files, as used by
// command line tools (Decomposedfs.CleanupRevisionsUnchecked).
var _ = Describe("On-demand revision cleanup", func() {
	// the context of a command line tool, without a user, unlike env.Ctx
	toolCtx := context.Background()
	var (
		env       *helpers.DecomposedTestEnv
		spaceID   string
		file1     *node.Node // dir1/file1, with 2 revisions
		file2     *node.Node // dir1/subdir1/file2, with 1 revision
		deletedMu sync.Mutex
		deleted   []string // the blob ids deleted from the blobstore
		results   []revisioncleanup.Result
	)

	// addRevisions gives n revisions of the given ages, each with a blob of its own
	addRevisions := func(n *node.Node, ages ...time.Duration) {
		for i, age := range ages {
			version := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
			_, err := env.Tree.CreateRevision(env.Ctx, n, version)
			Expect(err).ToNot(HaveOccurred())
			rev := node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+version, env.Lookup)
			Expect(env.Lookup.MetadataBackend().Set(env.Ctx, rev, prefixes.BlobIDAttr, fmt.Appendf(nil, "%s-rev-%d", n.Name, i))).To(Succeed())
		}
	}
	revisionCount := func(n *node.Node) int {
		revs, err := env.Tree.ListRevisionsUnchecked(env.Ctx, n)
		Expect(err).ToNot(HaveOccurred())
		return len(revs)
	}
	deletedBlobs := func() []string {
		deletedMu.Lock()
		defer deletedMu.Unlock()
		return append([]string{}, deleted...)
	}
	var resultsMu sync.Mutex
	collect := func(result revisioncleanup.Result) {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		results = append(results, result)
	}
	safeResults := func() []revisioncleanup.Result {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		return append([]revisioncleanup.Result{}, results...)
	}
	resultOf := func(n *node.Node) *revisioncleanup.Result {
		for i := range results {
			if results[i].NodeID == n.ID {
				return &results[i]
			}
		}
		return nil
	}

	BeforeEach(func() {
		var err error
		// no strategy configured, the requests give one
		env, err = helpers.NewTestEnv(map[string]any{"revisions_cleanup_async": false})
		Expect(err).ToNot(HaveOccurred())
		spaceID = env.SpaceRootRes.SpaceId
		deleted, results = nil, nil
		env.Blobstore.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
			deletedMu.Lock()
			defer deletedMu.Unlock()
			deleted = append(deleted, args.Get(0).(*node.Node).BlobID)
		}).Return(nil)

		file1, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/file1"})
		Expect(err).ToNot(HaveOccurred())
		subdir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"})
		Expect(err).ToNot(HaveOccurred())
		file2, err = env.CreateTestFile("file2", "file2-blobid", subdir1.ID, subdir1.SpaceID, 10)
		Expect(err).ToNot(HaveOccurred())

		addRevisions(file1, 48*time.Hour, 72*time.Hour)
		addRevisions(file2, 48*time.Hour)
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("cleans up all files of a space, recursively", func() {
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0"}, collect)).To(Succeed())

		Expect(revisionCount(file1)).To(BeZero())
		Expect(revisionCount(file2)).To(BeZero())
		Expect(deletedBlobs()).To(ConsistOf("file1-rev-0", "file1-rev-1", "file2-rev-0"))
		Expect(resultOf(file1)).ToNot(BeNil())
		Expect(resultOf(file1).Revisions).To(HaveLen(2))
		Expect(resultOf(file2)).ToNot(BeNil())
		Expect(resultOf(file1).Path).To(Equal("/dir1/file1"))
		Expect(resultOf(file2).Path).To(Equal("/dir1/subdir1/file2"))
		// folders are not cleaned up themselves
		for _, result := range results {
			Expect(result.NodeID).ToNot(Equal(spaceID))
		}
	})

	It("cleans up a space given more than once only once", func() {
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID, spaceID}, Strategy: "0"}, collect)).To(Succeed())

		reported := 0
		for _, result := range results {
			if result.NodeID == file1.ID {
				reported++
			}
		}
		Expect(reported).To(Equal(1))
	})

	It("switches to the group of the space it walks with a SpaceScoper", func() {
		root, err := env.Lookup.NodeFromSpaceID(env.Ctx, spaceID)
		Expect(err).ToNot(HaveOccurred())
		cleaner, err := revisioncleanup.New("0")
		Expect(err).ToNot(HaveOccurred())
		w := &scopingWalker{Walker: env.Tree}

		Expect(revisioncleanup.Run(toolCtx, env.Lookup, w, []*node.Node{root}, cleaner, revisioncleanup.RunOptions{}, collect, nil)).To(Succeed())

		Expect(revisionCount(file1)).To(BeZero())
		Expect(w.scoped).To(ContainElement(spaceID))
		Expect(w.unscoped).To(Equal(len(w.scoped)))
		Expect(w.listedScoped).ToNot(BeZero())
		Expect(w.listedUnscoped).To(BeZero(), "every folder is listed switched to the group of its space")
	})

	It("cleans up all spaces", func() {
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{All: true, Strategy: "0"}, collect)).To(Succeed())

		Expect(revisionCount(file1)).To(BeZero())
		Expect(revisionCount(file2)).To(BeZero())
	})

	Describe("all spaces", func() {
		// newSpace creates a space of the given type with a file with one revision in it
		newSpace := func(spaceType string) (string, *node.Node) {
			id, err := env.CreateTestStorageSpace(spaceType, nil)
			Expect(err).ToNot(HaveOccurred())
			f, err := env.CreateTestFile("file-in-"+spaceType, spaceType+"-blobid", id.OpaqueId, id.SpaceId, 10)
			Expect(err).ToNot(HaveOccurred())
			addRevisions(f, 48*time.Hour)
			Expect(revisionCount(f)).To(Equal(1))
			return id.SpaceId, f
		}

		It("include spaces of any type", func() {
			customID, f := newSpace("custom")
			// the "share" index lists all spaces with grants, which the test helper adds: take it
			// out, so that only its own type lists it, like an unshared space of that type
			index := spaceidindex.New(filepath.Join(env.Root, lookup.IndexesDir), "by-type")
			Expect(index.Remove("share", customID)).To(Succeed())

			Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{All: true, Strategy: "0"}, collect)).To(Succeed())

			Expect(revisionCount(f)).To(BeZero())
			Expect(revisionCount(file1)).To(BeZero())
		})

		It("skip disabled spaces, and fail for a disabled space requested by its id", func() {
			disabledID, f := newSpace("project")
			root, err := env.Lookup.NodeFromSpaceID(env.Ctx, disabledID)
			Expect(err).ToNot(HaveOccurred())
			dtime := time.Now()
			Expect(root.SetDTime(env.Ctx, &dtime)).To(Succeed())

			Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{All: true, Strategy: "0"}, collect)).To(Succeed())
			Expect(resultOf(root)).ToNot(BeNil())
			Expect(resultOf(root).Skipped).To(ContainSubstring("disabled"))
			Expect(revisionCount(f)).To(Equal(1))
			Expect(revisionCount(file1)).To(BeZero(), "the other spaces were cleaned up")

			err = env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{disabledID}, Strategy: "0"}, collect)
			Expect(err).To(MatchError(ContainSubstring("disabled")))
		})

		It("report a space that can't be found, and clean up the others", func() {
			index := spaceidindex.New(filepath.Join(env.Root, lookup.IndexesDir), "by-type")
			Expect(index.Add("project", "missing", "../../spaces/mi/ssing")).To(Succeed())

			Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{All: true, Strategy: "0"}, collect)).To(Succeed())

			missing := resultOf(&node.Node{BaseNode: node.BaseNode{ID: "missing"}})
			Expect(missing).ToNot(BeNil())
			Expect(missing.Err).To(HaveOccurred())
			Expect(revisionCount(file1)).To(BeZero())
		})
	})

	It("cleans up only the given file or folder", func() {
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Path: "/dir1/subdir1", Strategy: "0"}, collect)).To(Succeed())

		Expect(revisionCount(file1)).To(Equal(2))
		Expect(revisionCount(file2)).To(BeZero())
		Expect(resultOf(file1)).To(BeNil())
		Expect(resultOf(file2).Path).To(Equal("/dir1/subdir1/file2"), "the path within the space, not within the folder")

		results = nil
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Path: "dir1/file1", Strategy: "0"}, collect)).To(Succeed())
		Expect(revisionCount(file1)).To(BeZero())
		Expect(results).To(HaveLen(1))
		Expect(results[0].Path).To(Equal("/dir1/file1"))
	})

	It("only reports in a dry run", func() {
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0", DryRun: true}, collect)).To(Succeed())

		Expect(revisionCount(file1)).To(Equal(2))
		Expect(revisionCount(file2)).To(Equal(1))
		Expect(deletedBlobs()).To(BeEmpty())
		Expect(resultOf(file1).DryRun).To(BeTrue())
		count, _ := resultOf(file1).DeletedCount()
		Expect(count).To(Equal(2))
	})

	It("uses the strategy of the request", func() {
		// keep the newest revision of each file
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "1"}, collect)).To(Succeed())

		Expect(revisionCount(file1)).To(Equal(1))
		Expect(revisionCount(file2)).To(Equal(1))
		Expect(deletedBlobs()).To(ConsistOf("file1-rev-1"))
	})

	It("runs in the background with the AsyncOptions of the request, and waits for it", func() {
		async := &revisioncleanup.AsyncOptions{Enabled: true, Workers: 2, QueueSize: 1}
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0", Async: async}, collect)).To(Succeed())

		// all cleanups are done when it returns
		Expect(revisionCount(file1)).To(BeZero())
		Expect(revisionCount(file2)).To(BeZero())
		Expect(resultOf(file1)).ToNot(BeNil())
		Expect(resultOf(file2)).ToNot(BeNil())
		Expect(resultOf(file1).Path).To(Equal("/dir1/file1"))
		Expect(resultOf(file2).Path).To(Equal("/dir1/subdir1/file2"))
	})

	It("doesn't drop files in a full queue, unless the request says so", func() {
		// a configured queue that is full all the time, with "drop"
		env.Cleanup()
		var err error
		env, err = helpers.NewTestEnv(map[string]any{
			"revisions_cleanup_async":            true,
			"revisions_cleanup_async_workers":    1,
			"revisions_cleanup_async_queue_size": 1,
			"revisions_cleanup_async_queue_full": "drop",
		})
		Expect(err).ToNot(HaveOccurred())
		spaceID = env.SpaceRootRes.SpaceId
		// every deletion takes a while, so that the walk fills the queue
		env.Blobstore.On("Delete", mock.Anything).Run(func(mock.Arguments) {
			time.Sleep(20 * time.Millisecond)
		}).Return(nil)
		dir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1"})
		Expect(err).ToNot(HaveOccurred())
		var files []*node.Node
		for i := range 5 {
			f, err := env.CreateTestFile(fmt.Sprintf("file-%d", i), fmt.Sprintf("file-%d-blobid", i), dir1.ID, dir1.SpaceID, 10)
			Expect(err).ToNot(HaveOccurred())
			addRevisions(f, 48*time.Hour)
			files = append(files, f)
		}

		// without AsyncOptions in the request, the run waits for room
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0"}, collect)).To(Succeed())
		for _, f := range files {
			Expect(resultOf(f)).ToNot(BeNil())
			Expect(resultOf(f).Skipped).To(BeEmpty())
			Expect(revisionCount(f)).To(BeZero())
		}

		// with AsyncOptions in the request, they apply as given
		for _, f := range files {
			addRevisions(f, 48*time.Hour)
		}
		results = nil
		async := &revisioncleanup.AsyncOptions{Enabled: true, Workers: 1, QueueSize: 1, QueueFull: revisioncleanup.QueueFullDrop}
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0", Async: async}, collect)).To(Succeed())
		dropped := 0
		for _, f := range files {
			if r := resultOf(f); r != nil && r.Skipped != "" {
				Expect(r.Skipped).To(ContainSubstring("queue was full"))
				dropped++
			}
		}
		Expect(dropped).ToNot(BeZero())

		// as does a timeout of "block"
		results = nil
		async = &revisioncleanup.AsyncOptions{Enabled: true, Workers: 1, QueueSize: 1, QueueFull: revisioncleanup.QueueFullBlock, QueueFullTimeout: time.Millisecond}
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0", Async: async}, collect)).To(Succeed())
		dropped = 0
		for _, f := range files {
			if r := resultOf(f); r != nil && r.Skipped != "" {
				Expect(r.Skipped).To(ContainSubstring("queue was still full after 1ms"))
				dropped++
			}
		}
		Expect(dropped).ToNot(BeZero())
	})

	It("skips a file whose space was disabled in the meantime", func() {
		root, err := env.Lookup.NodeFromSpaceID(env.Ctx, spaceID)
		Expect(err).ToNot(HaveOccurred())
		dtime := time.Now()
		Expect(root.SetDTime(env.Ctx, &dtime)).To(Succeed())
		cleaner, err := revisioncleanup.New("0")
		Expect(err).ToNot(HaveOccurred())
		w, ok := any(env.Tree).(revisioncleanup.Walker)
		Expect(ok).To(BeTrue())
		log := zerolog.Nop()

		// file1 was found before the space was disabled, the worker reads it again
		o := revisioncleanup.RunOptions{Async: revisioncleanup.AsyncOptions{Enabled: true, Workers: 1, QueueSize: 1}}
		Expect(revisioncleanup.Run(toolCtx, env.Lookup, w, []*node.Node{file1}, cleaner, o, collect, &log)).To(Succeed())

		Expect(resultOf(file1)).ToNot(BeNil())
		Expect(resultOf(file1).Err).ToNot(HaveOccurred())
		Expect(resultOf(file1).Skipped).To(Equal("the space is disabled"))
		Expect(revisionCount(file1)).To(Equal(2))
	})

	It("reports files it skipped", func() {
		Expect(file1.SetXattrString(env.Ctx, prefixes.StatusPrefix, node.ProcessingStatus+"some-upload")).To(Succeed())

		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0"}, collect)).To(Succeed())

		Expect(resultOf(file1).Skipped).To(ContainSubstring("still being processed"))
		Expect(resultOf(file1).Path).To(Equal("/dir1/file1"))
		Expect(revisionCount(file1)).To(Equal(2))
		Expect(revisionCount(file2)).To(BeZero())
	})

	Describe("stopped through its context", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
		BeforeEach(func() {
			ctx, cancel = context.WithCancel(toolCtx)
			DeferCleanup(func() { cancel() })
			// stop the run while the first revision is being deleted
			env.Blobstore.ExpectedCalls = nil
			env.Blobstore.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
				deletedMu.Lock()
				defer deletedMu.Unlock()
				deleted = append(deleted, args.Get(0).(*node.Node).BlobID)
				cancel()
			}).Return(nil)
		})
		// every file was either cleaned up completely, reported as skipped, or not reached
		expectStoppedGracefully := func() {
			for n, untouched := range map[*node.Node]int{file1: 2, file2: 1} {
				result := resultOf(n)
				switch {
				case result == nil:
					Expect(revisionCount(n)).To(Equal(untouched))
				case result.Skipped != "":
					Expect(result.Skipped).To(ContainSubstring("stopped"))
				default:
					Expect(result.Err).ToNot(HaveOccurred())
					for _, rev := range result.Revisions {
						Expect(rev.Err).ToNot(HaveOccurred())
					}
					Expect(revisionCount(n)).To(BeZero())
				}
			}
		}

		It("finishes the file it is cleaning up and stops", func() {
			err := env.Fs.CleanupRevisionsUnchecked(ctx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0"}, collect)

			Expect(err).To(MatchError(context.Canceled))
			// files without revisions may come first, the run stops after the first file with some
			withDeletions := 0
			for _, result := range results {
				if count, _ := result.DeletedCount(); count > 0 {
					withDeletions++
				}
			}
			Expect(withDeletions).To(Equal(1))
			count, _ := results[len(results)-1].DeletedCount()
			Expect(count).ToNot(BeZero(), "the run stops after the file it was cleaning up")
			expectStoppedGracefully()
		})

		It("finishes the running cleanups and reports the queued ones as skipped, in the background", func() {
			async := &revisioncleanup.AsyncOptions{Enabled: true, Workers: 1, QueueSize: 1}
			err := env.Fs.CleanupRevisionsUnchecked(ctx, revisioncleanup.Request{SpaceIDs: []string{spaceID}, Strategy: "0", Async: async}, collect)

			Expect(err).To(MatchError(context.Canceled))
			Expect(results).ToNot(BeEmpty())
			expectStoppedGracefully()
		})
	})

	Describe("run directly, with a tree that controls the timing", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
			roots  []*node.Node
		)
		BeforeEach(func() {
			ctx, cancel = context.WithCancel(toolCtx)
			DeferCleanup(func() { cancel() })
			root, err := env.Lookup.NodeFromSpaceID(env.Ctx, spaceID)
			Expect(err).ToNot(HaveOccurred())
			roots = []*node.Node{root}
		})
		run := func(w revisioncleanup.Walker, async revisioncleanup.AsyncOptions) error {
			cleaner, err := revisioncleanup.New("0")
			Expect(err).ToNot(HaveOccurred())
			return revisioncleanup.Run(ctx, env.Lookup, w, roots, cleaner, revisioncleanup.RunOptions{Async: async}, collect, nil)
		}

		It("doesn't stop in the middle of a file", func() {
			w := &stoppingTree{Tree: env.Tree, onList: func(fileCtx context.Context) error {
				cancel()
				return fileCtx.Err()
			}}

			Expect(run(w, revisioncleanup.AsyncOptions{})).To(MatchError(context.Canceled))
			Expect(w.errs).To(HaveLen(1), "the run stopped after the first file")
			Expect(w.errs[0]).ToNot(HaveOccurred(), "the context of the file's cleanup ended")
		})

		It("doesn't report a folder whose listing was stopped as an error", func() {
			w := &stoppingTree{Tree: env.Tree, onList: func(context.Context) error { return nil },
				onListFolder: func(ctx context.Context) error {
					cancel()
					return ctx.Err()
				}}

			Expect(run(w, revisioncleanup.AsyncOptions{})).To(MatchError(context.Canceled))
			Expect(results).To(BeEmpty())
		})

		It("reports a folder that could not be listed, with its location on disk", func() {
			w := &stoppingTree{Tree: env.Tree, onList: func(context.Context) error { return nil },
				onListFolder: func(context.Context) error { return fmt.Errorf("permission denied") }}

			Expect(run(w, revisioncleanup.AsyncOptions{})).To(Succeed())
			Expect(results).To(HaveLen(1))
			Expect(results[0].NodeID).To(Equal(roots[0].ID))
			Expect(results[0].Err).To(MatchError(And(
				ContainSubstring(fmt.Sprintf("%q", roots[0].InternalPath())),
				ContainSubstring("permission denied"))))
		})

		It("stops a walk that waits for room in the full queue", func() {
			// a third file, so that one fills the queue and the next one waits for room
			subdir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"})
			Expect(err).ToNot(HaveOccurred())
			_, err = env.CreateTestFile("file3", "file3-blobid", subdir1.ID, subdir1.SpaceID, 10)
			Expect(err).ToNot(HaveOccurred())

			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			w := &stoppingTree{Tree: env.Tree, onList: func(context.Context) error {
				once.Do(func() { close(entered) }) // the first file blocks the only worker
				<-release
				return nil
			}}
			returned := make(chan error, 1)
			go func() {
				returned <- run(w, revisioncleanup.AsyncOptions{Enabled: true, Workers: 1, QueueSize: 1, QueueFull: revisioncleanup.QueueFullBlock})
			}()

			Eventually(entered).Should(BeClosed())
			time.Sleep(200 * time.Millisecond) // the walk fills the queue and waits for room
			cancel()
			// the waiting walk gives up and reports its file right away, while the worker is still busy
			Eventually(func() []revisioncleanup.Result { return safeResults() }).Should(ContainElement(
				HaveField("Skipped", ContainSubstring("stopped"))))
			close(release)
			Eventually(returned).Should(Receive(MatchError(context.Canceled)))
		})
	})

	It("shuffles the spaces on request", func() {
		// the first space it tries is the one the error is about
		spaceIDs := []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"}
		firstSpace := func(shuffle bool) string {
			err := env.Fs.CleanupRevisionsUnchecked(toolCtx, revisioncleanup.Request{SpaceIDs: spaceIDs, Strategy: "0", Shuffle: shuffle}, collect)
			Expect(err).To(HaveOccurred())
			for _, id := range spaceIDs {
				if strings.Contains(err.Error(), `"`+id+`"`) {
					return id
				}
			}
			Fail("no space in " + err.Error())
			return ""
		}

		seen := map[string]bool{}
		for range 20 {
			Expect(firstSpace(false)).To(Equal("s1"))
			seen[firstSpace(true)] = true
		}
		Expect(len(seen)).To(BeNumerically(">", 1))
		Expect(spaceIDs[0]).To(Equal("s1"), "the request is left alone")
	})

	It("shuffles the files and folders within a folder on request", func() {
		subdir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/subdir1"})
		Expect(err).ToNot(HaveOccurred())
		for i := range 5 {
			_, err := env.CreateTestFile(fmt.Sprintf("more%d", i), fmt.Sprintf("more%d-blobid", i), subdir1.ID, subdir1.SpaceID, 10)
			Expect(err).ToNot(HaveOccurred())
		}
		cleaner, err := revisioncleanup.New("0")
		Expect(err).ToNot(HaveOccurred())
		// the tree lists folders in parallel, in no particular order: sort them, so that only the shuffle changes it
		w := &stoppingTree{Tree: env.Tree, onList: func(context.Context) error { return nil }, sortFolders: true}
		// the order in which the files are cleaned up, in synchronous mode
		order := func(shuffle bool) string {
			results = nil
			o := revisioncleanup.RunOptions{DryRun: true, Shuffle: shuffle}
			Expect(revisioncleanup.Run(toolCtx, env.Lookup, w, []*node.Node{subdir1}, cleaner, o, collect, nil)).To(Succeed())
			Expect(results).To(HaveLen(6))
			ids := make([]string, len(results))
			for i, result := range results {
				ids[i] = result.NodeID
			}
			return strings.Join(ids, ",")
		}

		unshuffled := order(false)
		shuffled := map[string]bool{}
		for range 10 {
			Expect(order(false)).To(Equal(unshuffled))
			shuffled[order(true)] = true
		}
		Expect(len(shuffled)).To(BeNumerically(">", 1))

		// a shuffled request runs as well; its order can't be checked, as the tree lists folders in no particular order
		results = nil
		req := revisioncleanup.Request{SpaceIDs: []string{spaceID}, Path: "dir1/subdir1", Strategy: "0", DryRun: true, Shuffle: true}
		Expect(env.Fs.CleanupRevisionsUnchecked(toolCtx, req, collect)).To(Succeed())
		Expect(results).To(HaveLen(6))
	})

	DescribeTable("rejects invalid requests",
		func(req revisioncleanup.Request, errorContains string) {
			if req.SpaceIDs != nil && req.SpaceIDs[0] == "the space" {
				req.SpaceIDs[0] = spaceID
			}
			err := env.Fs.CleanupRevisionsUnchecked(toolCtx, req, collect)
			Expect(err).To(MatchError(ContainSubstring(errorContains)))
			Expect(revisionCount(file1)).To(Equal(2))
		},
		Entry("without a scope", revisioncleanup.Request{Strategy: "0"}, "no space"),
		Entry("with all and specific spaces", revisioncleanup.Request{All: true, SpaceIDs: []string{"the space"}, Strategy: "0"}, "not both"),
		Entry("with a path but without a space", revisioncleanup.Request{All: true, Path: "dir1", Strategy: "0"}, "exactly one space"),
		Entry("with a path in two spaces", revisioncleanup.Request{SpaceIDs: []string{"the space", "other"}, Path: "dir1", Strategy: "0"}, "exactly one space"),
		Entry("with a path on disk and a space", revisioncleanup.Request{FSPath: "/data/dir1", SpaceIDs: []string{"the space"}, Strategy: "0"}, "path on disk"),
		Entry("with a path on disk and all spaces", revisioncleanup.Request{FSPath: "/data/dir1", All: true, Strategy: "0"}, "path on disk"),
		Entry("with a path on disk and a path", revisioncleanup.Request{FSPath: "/data/dir1", Path: "dir1", Strategy: "0"}, "path on disk"),
		Entry("with a path on disk, which only posix supports", revisioncleanup.Request{FSPath: "/data/dir1", Strategy: "0"}, "only the posix driver"),
		Entry("with an unknown path", revisioncleanup.Request{SpaceIDs: []string{"the space"}, Path: "nope", Strategy: "0"}, "nope"),
		Entry("without any strategy", revisioncleanup.Request{SpaceIDs: []string{"the space"}}, "no revisions cleanup strategy"),
		Entry("with the strategy disabled", revisioncleanup.Request{SpaceIDs: []string{"the space"}, Strategy: "disabled"}, "no revisions cleanup strategy"),
		Entry("with an invalid strategy", revisioncleanup.Request{SpaceIDs: []string{"the space"}, Strategy: "whenever"}, "invalid revisions cleanup strategy"),
		Entry("with invalid async options", revisioncleanup.Request{SpaceIDs: []string{"the space"}, Strategy: "0",
			Async: &revisioncleanup.AsyncOptions{Enabled: true, QueueFull: "wait"}}, "queue full policy"),
	)
})

// scopingWalker records the calls of the revision cleanup to switch to the group of a space, and
// whether the folders are listed with the context it returns.
type scopingWalker struct {
	revisioncleanup.Walker
	mu             sync.Mutex
	scoped         []string
	unscoped       int
	listedScoped   int
	listedUnscoped int
}

type scopedKey struct{}

func (w *scopingWalker) ScopeToSpace(ctx context.Context, spaceID string) (context.Context, func() error, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scoped = append(w.scoped, spaceID)
	return context.WithValue(ctx, scopedKey{}, spaceID), func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.unscoped++
		return nil
	}, nil
}

func (w *scopingWalker) ListFolderKnownUnchecked(ctx context.Context, n *node.Node) ([]*node.Node, error) {
	w.mu.Lock()
	if ctx.Value(scopedKey{}) == n.SpaceID {
		w.listedScoped++
	} else {
		w.listedUnscoped++
	}
	w.mu.Unlock()
	return w.Walker.ListFolderKnownUnchecked(ctx, n)
}

// stoppingTree calls onList whenever the cleanup of a file lists its revisions, with the
// context of that cleanup, and records what it returns. onListFolder, if set, replaces the
// listing of folders: an error it returns is the listing's error. sortFolders lists the
// entries of folders sorted by name.
type stoppingTree struct {
	node.Tree
	onList       func(ctx context.Context) error
	onListFolder func(ctx context.Context) error
	sortFolders  bool
	mu           sync.Mutex
	errs         []error
}

func (t *stoppingTree) ListRevisionsUnchecked(ctx context.Context, n *node.Node) ([]*provider.FileVersion, error) {
	err := t.onList(ctx)
	t.mu.Lock()
	t.errs = append(t.errs, err)
	t.mu.Unlock()
	return t.Tree.ListRevisionsUnchecked(ctx, n)
}

func (t *stoppingTree) ListFolderKnownUnchecked(ctx context.Context, n *node.Node) ([]*node.Node, error) {
	if t.onListFolder != nil {
		if err := t.onListFolder(ctx); err != nil {
			return nil, err
		}
	}
	children, err := t.Tree.(revisioncleanup.Walker).ListFolderKnownUnchecked(ctx, n)
	if t.sortFolders {
		slices.SortFunc(children, func(a, b *node.Node) int { return strings.Compare(a.Name, b.Name) })
	}
	return children, err
}
