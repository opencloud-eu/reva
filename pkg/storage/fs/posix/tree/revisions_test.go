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

	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"

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
		revs, err := non_watching_env.Tree.ListRevisionsUnchecked(non_watching_env.Ctx, n)
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
			keys = append(keys, n.ID+node.RevisionIDDelimiter+version)
		}
	})

	Describe("ListRevisionsUnchecked", func() {
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

	Describe("revisioncleanup.Cleaner", func() {
		It("purges the revisions the strategy wants to delete", func() {
			// keep everything younger than 7 days, delete everything older than 30 days, thin out in between
			cleaner, err := revisioncleanup.New("7d, auto, 30d")
			Expect(err).ToNot(HaveOccurred())

			cleaner.Cleanup(non_watching_env.Ctx, non_watching_env.Lookup, non_watching_env.Tree, n)

			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
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
			revs, err := fsEnv.Tree.ListRevisionsUnchecked(fsEnv.Ctx, n)
			Expect(err).ToNot(HaveOccurred())
			keys := make([]string, len(revs))
			for i, r := range revs {
				keys[i] = r.Key
			}
			return keys
		}
		// backdates the copy of the current content, it becomes a revision with that timestamp on the next change:
		// assimilation names the revision after the mtime of '.CURRENT' when renaming it. This
		// is the only way to get old revisions from changes on disk without waiting for days.
		backdateCurrent := func(age time.Duration) string {
			currentPath := fsEnv.Lookup.CurrentPath(n.SpaceID, n.ID)
			t := time.Now().Add(-age)
			Expect(os.Chtimes(currentPath, t, t)).To(Succeed())
			fi, err := os.Stat(currentPath)
			Expect(err).ToNot(HaveOccurred())
			return n.ID + node.RevisionIDDelimiter + fi.ModTime().UTC().Format(time.RFC3339Nano)
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
			retained := backdateCurrent(10 * 24 * time.Hour)
			changeAndAssimilate("v2")
			Expect(listKeys()).To(ContainElement(retained))

			purged := backdateCurrent(100 * 24 * time.Hour)
			changeAndAssimilate("v3")
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
