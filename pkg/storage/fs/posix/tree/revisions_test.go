// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	helpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/revisioncleanup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Revisions", func() {
	var (
		n   *node.Node
		now = time.Now()
		// One revision per age, chosen around the "7d, auto, 30d" strategy used below:
		// within the 7 day retention, between retention and deletion (where "auto" keeps
		// it, as it is the only one in its slot), and beyond the 30 day deletion threshold. The ages end up in the revision keys, which is what
		// the cleanup takes the revision time from.
		ages = []time.Duration{time.Hour, 10 * 24 * time.Hour, 100 * 24 * time.Hour}
		keys []string
	)

	revisionNode := func(key string) *node.BaseNode {
		return node.NewBaseNode(n.SpaceID, key, non_watching_env.Lookup)
	}
	listKeys := func() []string {
		revs, err := non_watching_env.Tree.ListRevisions(non_watching_env.Ctx, n)
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
			key := n.ID + node.RevisionIDDelimiter + version
			// the cleanup goes by the time a revision was created, which CreateRevision sets to now
			Expect(node.SetRevisionCreated(env.Ctx, env.Lookup.MetadataBackend(), node.NewBaseNode(n.SpaceID, key, env.Lookup), now.Add(-age))).To(Succeed())
			keys = append(keys, key)
		}
	})

	Describe("ListRevisions", func() {
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

	Describe("the on-demand cleanup", func() {
		It("cleans up the revisions of a file in the posix layout", func() {
			req := revisioncleanup.Request{
				SpaceIDs: []string{non_watching_env.SpaceRootRes.SpaceId},
				Path:     "dir1/" + n.Name,
				Strategy: "0", // deletes all revisions
			}
			var results []revisioncleanup.Result

			// a dry run first: nothing is deleted
			req.DryRun = true
			Expect(non_watching_env.Fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { results = append(results, r) })).To(Succeed())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Path).To(Equal("/dir1/" + n.Name))
			Expect(results[0].Revisions).To(HaveLen(len(keys)))
			Expect(listKeys()).To(ConsistOf(keys))

			req.DryRun = false
			results = nil
			Expect(non_watching_env.Fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { results = append(results, r) })).To(Succeed())
			Expect(results).To(HaveLen(1))
			Expect(listKeys()).To(BeEmpty())
		})

		It("creates no lock files in a dry run", func() {
			env := non_watching_env
			locks := filepath.Dir(env.Lookup.MetadataBackend().LockfilePath(n))
			lockfiles := func() []string {
				files, err := filepath.Glob(filepath.Join(locks, "*.mlock"))
				Expect(err).ToNot(HaveOccurred())
				return files
			}
			// as for files created on disk and assimilated, which were never locked
			for _, f := range lockfiles() {
				Expect(os.Remove(f)).To(Succeed())
			}

			req := revisioncleanup.Request{SpaceIDs: []string{env.SpaceRootRes.SpaceId}, Strategy: "0", DryRun: true}
			var results []revisioncleanup.Result
			Expect(env.Fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { results = append(results, r) })).To(Succeed())
			Expect(results).ToNot(BeEmpty())
			Expect(lockfiles()).To(BeEmpty())
			Expect(listKeys()).To(ConsistOf(keys))
		})

		// The background workers read each node again, and miss the cache e.g. when its entry
		// expired, or with no cache at all, as here.
		It("creates no lock files in a dry run in the background, also when reading misses the cache", func() {
			env, err := helpers.NewTestEnv(map[string]any{
				"watch_fs":                false,
				"revisions_cleanup_async": true,
				"filemetadatacache":       map[string]any{"cache_store": "noop"},
			})
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(env.Cleanup)

			root, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes})
			Expect(err).ToNot(HaveOccurred())
			// as for files created on disk and assimilated, which were never locked
			locks := filepath.Dir(env.Lookup.MetadataBackend().LockfilePath(root))
			Expect(os.RemoveAll(locks)).To(Succeed())

			req := revisioncleanup.Request{SpaceIDs: []string{env.SpaceRootRes.SpaceId}, Strategy: "0", DryRun: true}
			var results []revisioncleanup.Result
			Expect(env.Fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { results = append(results, r) })).To(Succeed())
			Expect(results).ToNot(BeEmpty())
			for _, r := range results {
				Expect(r.Err).ToNot(HaveOccurred())
			}
			Expect(locks).ToNot(BeADirectory())
		})

		It("finds files and folders by their path on disk", func() {
			env := non_watching_env
			spaceID := env.SpaceRootRes.SpaceId
			dir := filepath.Dir(n.InternalPath())

			for fsPath, path := range map[string]string{
				n.InternalPath():  "dir1/" + n.Name,
				dir:               "dir1",
				dir + "/":         "dir1",
				dir + "/../dir1":  "dir1",
				filepath.Dir(dir): "", // the space root
			} {
				s, p, err := env.Lookup.SpacePathFromFSPath(env.Ctx, fsPath)
				Expect(err).ToNot(HaveOccurred(), fsPath)
				Expect(s).To(Equal(spaceID), fsPath)
				Expect(p).To(Equal(path), fsPath)
			}

			_, _, err := env.Lookup.SpacePathFromFSPath(env.Ctx, filepath.Dir(env.Lookup.InternalRoot()))
			Expect(err).To(BeAssignableToTypeOf(errtypes.BadRequest("")))
			_, _, err = env.Lookup.SpacePathFromFSPath(env.Ctx, filepath.Join(dir, "unknown"))
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))

			// what posixFS does with a request with a path on disk
			s, p, err := env.Lookup.SpacePathFromFSPath(env.Ctx, n.InternalPath())
			Expect(err).ToNot(HaveOccurred())
			req := revisioncleanup.Request{SpaceIDs: []string{s}, Path: p, Strategy: "0"}
			Expect(env.Fs.CleanupRevisionsUnchecked(context.Background(), req, nil)).To(Succeed())
			Expect(listKeys()).To(BeEmpty())
		})
	})

	Describe("the on-demand cleanup of a folder with a file the storage doesn't know yet", func() {
		It("ignores the file instead of registering it", func() {
			env := non_watching_env
			unknown := filepath.Join(filepath.Dir(n.InternalPath()), uuid.NewString())
			Expect(os.WriteFile(unknown, []byte("unknown"), 0600)).To(Succeed())
			isKnown := func() bool {
				_, _, err := env.Lookup.IDsForPath(env.Ctx, unknown)
				return err == nil
			}
			Expect(isKnown()).To(BeFalse())

			for _, dryRun := range []bool{true, false} {
				req := revisioncleanup.Request{SpaceIDs: []string{env.SpaceRootRes.SpaceId}, Path: "dir1", Strategy: "0", DryRun: dryRun}
				var paths []string
				Expect(env.Fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { paths = append(paths, r.Path) })).To(Succeed())
				Expect(isKnown()).To(BeFalse(), "dry run %v registered the file", dryRun)
				Expect(paths).To(ContainElement("/dir1/"+n.Name), "the paths of the files in the folder")
			}
			Expect(listKeys()).To(BeEmpty(), "the known file was cleaned up")

			// unlike a regular listing, which registers it
			dir1, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "dir1"})
			Expect(err).ToNot(HaveOccurred())
			_, err = env.Tree.ListFolder(env.Ctx, dir1)
			Expect(err).ToNot(HaveOccurred())
			Expect(isKnown()).To(BeTrue())
		})
	})

	Describe("revisioncleanup.Cleaner", func() {
		It("purges the revisions the strategy wants to delete", func() {
			// keep everything younger than 7 days, delete everything older than 30 days, thin out in between
			cleaner, err := revisioncleanup.New("7d, auto, 30d")
			Expect(err).ToNot(HaveOccurred())

			cleaner.Cleanup(non_watching_env.Ctx, non_watching_env.Lookup, non_watching_env.Tree, n)

			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
		})
	})

	Describe("a revision deleted by another process", func() {
		var ref *provider.Reference

		BeforeEach(func() {
			env := non_watching_env
			ref = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: n.SpaceID, OpaqueId: n.ID}}
			call := env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
				Stat:                 true,
				ListFileVersions:     true,
				InitiateFileDownload: true,
				RestoreFileVersion:   true,
			}, nil)
			DeferCleanup(call.Unset)

			// Delete the oldest revision the way another process does, e.g. the revision cleanup
			// on demand: its file is gone, but this process's metadata cache still has its
			// entry, from reading it before (as listing the versions does).
			rn := revisionNode(keys[2])
			_, err := env.Lookup.MetadataBackend().All(env.Ctx, rn)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.Remove(rn.InternalPath())).To(Succeed())
		})

		It("is not restored, and the file keeps its content", func() {
			env := non_watching_env
			_, err := env.Fs.RestoreRevision(env.Ctx, ref, keys[2])
			_, notFound := err.(errtypes.IsNotFound)
			Expect(notFound).To(BeTrue(), "expected a not found error, got %v", err)

			fi, err := os.Stat(n.InternalPath())
			Expect(err).ToNot(HaveOccurred())
			Expect(fi.Size()).To(Equal(int64(16)))
			// and the failed restore left no revision of the current content behind
			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
		})

		It("is not downloaded", func() {
			env := non_watching_env
			_, _, err := env.Fs.DownloadRevision(env.Ctx, ref, keys[2], func(*provider.ResourceInfo) bool { return true })
			_, notFound := err.(errtypes.IsNotFound)
			Expect(notFound).To(BeTrue(), "expected a not found error, got %v", err)
		})
	})
})

var _ = Describe("Revisions created from changes on disk, cleaned up right away", Ordered, changesOnDisk(false))

var _ = Describe("Revisions created from changes on disk, cleaned up in the background", Ordered, changesOnDisk(true))

// changesOnDisk tests the revision cleanup of revisions that assimilation creates from
// changes on disk, with the cleanup running right away or in background workers.
func changesOnDisk(async bool) func() {
	return func() {
		var (
			fsEnv  *helpers.TestEnv
			parent *node.Node
			path   string
			n      *node.Node
		)

		listKeys := func() []string {
			revs, err := fsEnv.Tree.ListRevisions(fsEnv.Ctx, n)
			Expect(err).ToNot(HaveOccurred())
			keys := make([]string, len(revs))
			for i, r := range revs {
				keys[i] = r.Key
			}
			return keys
		}
		// returns the key of the revision the next change creates: assimilation names it after the
		// mtime of '.CURRENT' when renaming it
		nextRevisionKey := func() string {
			fi, err := os.Stat(fsEnv.Lookup.CurrentPath(n.SpaceID, n.ID))
			Expect(err).ToNot(HaveOccurred())
			return n.ID + node.RevisionIDDelimiter + fi.ModTime().UTC().Format(time.RFC3339Nano)
		}
		// checks that assimilation recorded when the revision was created, and backdates that
		// time: the cleanup goes by it, and this is the only way to get old revisions from
		// changes on disk without waiting for days
		backdateRevision := func(key string, age time.Duration) {
			rev := node.NewBaseNode(n.SpaceID, key, fsEnv.Lookup)
			t, err := node.RevisionCreated(fsEnv.Ctx, fsEnv.Lookup.MetadataBackend(), rev)
			Expect(err).ToNot(HaveOccurred())
			Expect(t).To(BeTemporally("~", time.Now(), time.Minute))
			Expect(node.SetRevisionCreated(fsEnv.Ctx, fsEnv.Lookup.MetadataBackend(), rev, time.Now().Add(-age))).To(Succeed())
		}
		// changes the file on disk and assimilates it like the watcher would,
		// then waits for the asynchronous copy of the new content to the current file
		// Every change gets a strictly later mtime: assimilation skips a file whose mtime equals
		// the one it recorded, and two writes in quick succession can get the same mtime, as the
		// kernel takes file timestamps from a coarse clock.
		var changeTime time.Time
		changeAndAssimilate := func(content string) {
			Expect(os.WriteFile(path, []byte(content), 0600)).To(Succeed())
			if changeTime.IsZero() {
				changeTime = time.Now()
			}
			changeTime = changeTime.Add(time.Second)
			Expect(os.Chtimes(path, changeTime, changeTime)).To(Succeed())
			Expect(fsEnv.Tree.AssimilateForTest(path)).To(Succeed())
			Eventually(func(g Gomega) {
				b, err := os.ReadFile(fsEnv.Lookup.CurrentPath(n.SpaceID, n.ID))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(string(b)).To(Equal(content))
			}).WithTimeout(15 * time.Second).ProbeEvery(100 * time.Millisecond).Should(Succeed())
		}

		BeforeAll(func() {
			var err error
			// no watching: starting another inotify watcher would kill the one of the watching test env,
			// changes are assimilated explicitly instead (see export_test.go)
			fsEnv, err = helpers.NewTestEnv(map[string]any{
				"enable_fs_revisions": true,
				// keep everything younger than 7 days, delete everything older than 30 days, thin out in between
				"revisions_cleanup_strategy": "7d, auto, 30d",
				"revisions_cleanup_async":    async,
			})
			Expect(err).ToNot(HaveOccurred())

			parent, err = fsEnv.Lookup.NodeFromResource(fsEnv.Ctx, &provider.Reference{
				ResourceId: fsEnv.SpaceRootRes,
				Path:       "dir1",
			})
			Expect(err).ToNot(HaveOccurred())

			name := uuid.NewString()
			path = parent.InternalPath() + "/" + name
			Expect(os.WriteFile(path, []byte("v1"), 0600)).To(Succeed())
			_, err = fsEnv.Tree.ListFolder(fsEnv.Ctx, parent)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				n, err = fsEnv.Lookup.NodeFromResource(fsEnv.Ctx, &provider.Reference{
					ResourceId: fsEnv.SpaceRootRes,
					Path:       "dir1/" + name,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(n.Exists).To(BeTrue())
				g.Expect(fsEnv.Lookup.CurrentPath(n.SpaceID, n.ID)).To(BeAnExistingFile())
			}).WithTimeout(15 * time.Second).ProbeEvery(100 * time.Millisecond).Should(Succeed())
		})

		AfterAll(func() {
			if fsEnv != nil {
				// stop the background cleanup workers before their files are removed
				Expect(fsEnv.Fs.Shutdown(context.Background())).To(Succeed())
				fsEnv.Cleanup()
			}
		})

		It("keeps revisions the strategy wants to keep and purges the others", func() {
			retained := nextRevisionKey()
			changeAndAssimilate("v2")
			Expect(listKeys()).To(ContainElement(retained))
			backdateRevision(retained, 10*24*time.Hour)

			purged := nextRevisionKey()
			changeAndAssimilate("v3")
			Expect(listKeys()).To(ContainElement(purged))
			backdateRevision(purged, 100*24*time.Hour)

			// the next change cleans up the older ones
			changeAndAssimilate("v4")
			cleanedUp := And(ContainElement(retained), Not(ContainElement(purged)))
			// the cleanup runs once the new content has been copied to the current file, and in
			// the background, a worker runs it eventually
			Eventually(listKeys).WithTimeout(15 * time.Second).ProbeEvery(100 * time.Millisecond).Should(cleanedUp)
		})
	}
}

var _ = Describe("Revisions of a fresh storage", func() {
	var fsEnv *helpers.TestEnv

	AfterEach(func() {
		if fsEnv != nil {
			fsEnv.Cleanup()
		}
	})

	It("rejects an invalid revisions cleanup strategy at startup", func() {
		var err error
		fsEnv, err = helpers.NewTestEnv(map[string]any{"revisions_cleanup_strategy": "whenever"})
		Expect(err).To(HaveOccurred())
	})

	It("stops the background workers again when the storage can't be set up", func() {
		// the cleanup's workers, by their function in the goroutine dump
		workers := func() int {
			buf := make([]byte, 1<<22)
			return strings.Count(string(buf[:runtime.Stack(buf, true)]), "revisioncleanup.(*Service).work(")
		}
		before := workers()
		// a root that is a file: the tree's storage check fails after the cleanup has started
		root := filepath.Join(GinkgoT().TempDir(), "file")
		Expect(os.WriteFile(root, nil, 0600)).To(Succeed())

		var err error
		fsEnv, err = helpers.NewTestEnv(map[string]any{
			"root":                            root,
			"revisions_cleanup_strategy":      "auto",
			"revisions_cleanup_async":         true,
			"revisions_cleanup_async_workers": 3,
		})
		Expect(err).To(MatchError(ContainSubstring("unfit storage")))
		fsEnv = nil

		Eventually(workers).Should(Equal(before))
	})
})

var _ = Describe("Switching to the group of a space for the revision cleanup", func() {
	It("puts the group of the space root into the context with use_space_groups", func() {
		env, err := helpers.NewTestEnv(map[string]any{"use_space_groups": true})
		Expect(err).ToNot(HaveOccurred())
		defer env.Cleanup()
		spaceID := env.SpaceRootRes.SpaceId
		fi, err := os.Stat(env.Lookup.InternalPath(spaceID, spaceID))
		Expect(err).ToNot(HaveOccurred())

		ctx, unscope, err := env.Tree.ScopeToSpace(env.Ctx, spaceID)
		Expect(err).ToNot(HaveOccurred())
		Expect(unscope()).To(Succeed())

		Expect(ctx.Value(decomposedfs.CtxKeySpaceGID)).To(Equal(fi.Sys().(*syscall.Stat_t).Gid))
	})

	It("does nothing without use_space_groups", func() {
		ctx, unscope, err := non_watching_env.Tree.ScopeToSpace(non_watching_env.Ctx, non_watching_env.SpaceRootRes.SpaceId)
		Expect(err).ToNot(HaveOccurred())
		Expect(unscope()).To(Succeed())

		Expect(ctx.Value(decomposedfs.CtxKeySpaceGID)).To(BeNil())
	})
})
