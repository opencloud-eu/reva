// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Revisions", func() {
	var (
		env *helpers.DecomposedTestEnv
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
		return node.NewBaseNode(n.SpaceID, key, env.Lookup)
	}
	blobOfRevision := func(i int) string {
		return fmt.Sprintf("revision-%d-blobid", i)
	}
	listKeys := func() []string {
		revs, err := env.Tree.ListRevisionsUnchecked(env.Ctx, n)
		Expect(err).ToNot(HaveOccurred())
		keys := make([]string, len(revs))
		for i, r := range revs {
			keys[i] = r.Key
		}
		return keys
	}
	expectBlobDeletion := func(blobIDs ...string) {
		for _, blobID := range blobIDs {
			env.Blobstore.On("Delete", mock.MatchedBy(func(bn *node.Node) bool {
				return bn.BlobID == blobID && bn.SpaceID == n.SpaceID
			})).Return(nil).Once()
		}
	}

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		n, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "dir1/file1",
		})
		Expect(err).ToNot(HaveOccurred())

		keys = nil
		for i, age := range ages {
			version := now.Add(-age).UTC().Format(time.RFC3339Nano)
			_, err := env.Tree.CreateRevision(env.Ctx, n, version)
			Expect(err).ToNot(HaveOccurred())
			key := n.ID + node.RevisionIDDelimiter + version
			// give every revision its own blob, CreateRevision copies the one of the current node
			Expect(env.Lookup.MetadataBackend().Set(env.Ctx, revisionNode(key), prefixes.BlobIDAttr, []byte(blobOfRevision(i)))).To(Succeed())
			keys = append(keys, key)
		}
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	Describe("ListRevisionsUnchecked", func() {
		It("lists all revisions", func() {
			Expect(listKeys()).To(ConsistOf(keys))
		})
	})

	Describe("revisioncleanup.PurgeRevisionUnchecked", func() {
		It("removes the revision, its metadata and its blob", func() {
			expectBlobDeletion(blobOfRevision(0))

			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0])).To(Succeed())

			rn := revisionNode(keys[0])
			Expect(rn.InternalPath()).ToNot(BeAnExistingFile())
			Expect(env.Lookup.MetadataBackend().MetadataPath(rn)).ToNot(BeAnExistingFile())
			Expect(env.Lookup.MetadataBackend().LockfilePath(rn)).ToNot(BeAnExistingFile())
			Expect(listKeys()).To(ConsistOf(keys[1:]))
			env.Blobstore.AssertExpectations(GinkgoT())
		})

		It("leaves the current version alone", func() {
			expectBlobDeletion(blobOfRevision(0))

			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0])).To(Succeed())

			Expect(n.InternalPath()).To(BeAnExistingFile())
			blobID, _, err := env.Lookup.ReadBlobIDAndSizeAttr(env.Ctx, n, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(blobID).To(Equal("file1-blobid"))
		})

		It("rejects revision keys of other nodes", func() {
			other := &node.Node{BaseNode: node.BaseNode{SpaceID: n.SpaceID, ID: "some-other-node"}}
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, other, keys[0])).ToNot(Succeed())
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, n.ID)).ToNot(Succeed())
			Expect(revisioncleanup.PurgeRevisionUnchecked(env.Ctx, env.Tree, n, keys[0]+"/../x")).ToNot(Succeed())
			Expect(listKeys()).To(ConsistOf(keys))
			env.Blobstore.AssertNotCalled(GinkgoT(), "Delete", mock.Anything)
		})
	})

	Describe("revisioncleanup.Cleaner", func() {
		var cleaner *revisioncleanup.Cleaner

		BeforeEach(func() {
			var err error
			// keep everything younger than 7 days, delete everything older than 30 days, thin out in between
			cleaner, err = revisioncleanup.New("7d, auto, 30d")
			Expect(err).ToNot(HaveOccurred())
		})

		It("purges the revisions the strategy wants to delete", func() {
			expectBlobDeletion(blobOfRevision(2))
			revs, err := env.Tree.ListRevisionsUnchecked(env.Ctx, n)
			Expect(err).ToNot(HaveOccurred())
			var size uint64
			for _, r := range revs {
				if r.Key == keys[2] {
					size = r.Size
				}
			}
			Expect(size).ToNot(BeZero())
			deleted, bytes, failures := counterValue(revisionsDeleted), counterValue(bytesDeleted), counterValue(purgeFailures)

			cleaner.Cleanup(env.Ctx, env.Lookup, env.Tree, n)

			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
			env.Blobstore.AssertExpectations(GinkgoT())
			Expect(counterValue(revisionsDeleted)).To(Equal(deleted + 1))
			Expect(counterValue(bytesDeleted)).To(Equal(bytes + float64(size)))
			Expect(counterValue(purgeFailures)).To(Equal(failures))
		})

		It("logs why it purged a revision", func() {
			expectBlobDeletion(blobOfRevision(2))
			var buf bytes.Buffer
			logger := zerolog.New(&buf).Level(zerolog.DebugLevel)

			cleaner.Cleanup(appctx.WithLogger(env.Ctx, &logger), env.Lookup, env.Tree, n)

			var purged map[string]any
			for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				var entry map[string]any
				Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed())
				if entry["message"] == "revision cleanup: purged revision" {
					purged = entry
				}
			}
			Expect(purged).ToNot(BeNil(), buf.String())
			Expect(purged["revision"]).To(Equal(keys[2]))
			// keys[2] is 100 days old, beyond the 30 days of "7d, auto, 30d"
			Expect(purged["reason"]).To(HavePrefix(`deleted by "30d": older than `))
		})

		It("counts revisions whose blob it failed to delete as failures", func() {
			env.Blobstore.On("Delete", mock.Anything).Return(errors.New("blobstore unavailable")).Once()
			deleted, bytes, failures := counterValue(revisionsDeleted), counterValue(bytesDeleted), counterValue(purgeFailures)

			cleaner.Cleanup(env.Ctx, env.Lookup, env.Tree, n)

			// the revision is gone, only its blob is left behind
			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
			Expect(counterValue(purgeFailures)).To(Equal(failures + 1))
			Expect(counterValue(revisionsDeleted)).To(Equal(deleted))
			Expect(counterValue(bytesDeleted)).To(Equal(bytes))
		})

		It("never purges revisions it was asked to keep", func() {
			cleaner.Cleanup(env.Ctx, env.Lookup, env.Tree, n, keys[2])

			Expect(listKeys()).To(ConsistOf(keys))
			env.Blobstore.AssertNotCalled(GinkgoT(), "Delete", mock.Anything)
		})

		It("does nothing while the node is processing", func() {
			Expect(n.SetXattrString(env.Ctx, prefixes.StatusPrefix, node.ProcessingStatus+"some-upload")).To(Succeed())

			cleaner.Cleanup(env.Ctx, env.Lookup, env.Tree, n)

			Expect(listKeys()).To(ConsistOf(keys))
			env.Blobstore.AssertNotCalled(GinkgoT(), "Delete", mock.Anything)
		})

		It("deletes the blobs only after releasing the node lock", func() {
			// the lock of the node is not reentrant for another node object
			locked := func() bool {
				acquired := make(chan func() error, 1)
				go func() {
					if unlock, err := env.Lookup.MetadataBackend().Lock(node.NewBaseNode(n.SpaceID, n.ID, env.Lookup)); err == nil {
						acquired <- unlock
					}
				}()
				select {
				case unlock := <-acquired:
					_ = unlock()
					return false
				case <-time.After(time.Second):
					return true
				}
			}
			var lockedWhileDeleting []bool
			env.Blobstore.On("Delete", mock.Anything).Run(func(mock.Arguments) {
				lockedWhileDeleting = append(lockedWhileDeleting, locked())
			}).Return(nil).Once()

			cleaner.Cleanup(env.Ctx, env.Lookup, env.Tree, n)

			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
			Expect(lockedWhileDeleting).To(Equal([]bool{false}))
		})

		It("switches to the group of the space with a SpaceScoper", func() {
			expectBlobDeletion(blobOfRevision(2))
			st := &scopingTree{Tree: env.Tree}

			cleaner.Cleanup(env.Ctx, env.Lookup, st, n)

			Expect(st.scoped).To(Equal([]string{n.SpaceID}))
			Expect(st.unscoped).To(Equal(1))
			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
		})

		It("is disabled when not configured", func() {
			for _, config := range []string{"", "disabled", "  disabled "} {
				c, err := revisioncleanup.New(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(c).ToNot(BeNil())
				Expect(c.Enabled()).To(BeFalse())
				c.Cleanup(env.Ctx, env.Lookup, env.Tree, n)
			}
			// a nil Cleaner behaves like a disabled one
			var none *revisioncleanup.Cleaner
			Expect(none.Enabled()).To(BeFalse())
			none.Cleanup(env.Ctx, env.Lookup, env.Tree, n)
			Expect(listKeys()).To(ConsistOf(keys))
			Expect(cleaner.Enabled()).To(BeTrue())
		})

		It("rejects invalid configurations", func() {
			_, err := revisioncleanup.New("whenever")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("revisioncleanup.Service", func() {
		var cleaner *revisioncleanup.Cleaner

		BeforeEach(func() {
			var err error
			cleaner, err = revisioncleanup.New("7d, auto, 30d")
			Expect(err).ToNot(HaveOccurred())
		})

		newService := func(opts revisioncleanup.AsyncOptions) *revisioncleanup.Service {
			s, err := revisioncleanup.NewService(cleaner, env.Lookup, env.Tree, opts, nil)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				Expect(s.Shutdown(context.Background())).To(Succeed())
			})
			return s
		}

		It("cleans up right away when not running asynchronously", func() {
			expectBlobDeletion(blobOfRevision(2))
			s := newService(revisioncleanup.AsyncOptions{Enabled: false})

			s.HandleRevisionCreated(env.Ctx, n)

			Expect(listKeys()).To(ConsistOf(keys[0], keys[1]))
			env.Blobstore.AssertExpectations(GinkgoT())
		})

		It("cleans up in the background when running asynchronously", func() {
			expectBlobDeletion(blobOfRevision(2))
			s := newService(revisioncleanup.AsyncOptions{Enabled: true, Workers: 1})

			s.HandleRevisionCreated(env.Ctx, n)

			Eventually(listKeys).Should(ConsistOf(keys[0], keys[1]))
			Eventually(func() bool { return env.Blobstore.AssertExpectations(&noopT{}) }).Should(BeTrue())
		})

		It("skips nodes that were deleted before the cleanup ran", func() {
			expectBlobDeletion(blobOfRevision(2))
			s := newService(revisioncleanup.AsyncOptions{Enabled: true, Workers: 1})
			gone := &node.Node{BaseNode: node.BaseNode{SpaceID: n.SpaceID, ID: "does-not-exist"}}

			s.HandleRevisionCreated(env.Ctx, gone)
			s.HandleRevisionCreated(env.Ctx, n) // processed after gone, by the only worker

			Eventually(listKeys).Should(ConsistOf(keys[0], keys[1]))
			Eventually(func() bool { return env.Blobstore.AssertExpectations(&noopT{}) }).Should(BeTrue())
		})
	})
})

// scopingTree records the calls of the revision cleanup to switch to the group of a space.
type scopingTree struct {
	*tree.Tree
	scoped   []string
	unscoped int
}

func (t *scopingTree) ScopeToSpace(ctx context.Context, spaceID string) (context.Context, func() error, error) {
	t.scoped = append(t.scoped, spaceID)
	return ctx, func() error {
		t.unscoped++
		return nil
	}, nil
}

// noopT lets mock assertions be polled without failing the test on the first attempt.
type noopT struct{}

func (noopT) Errorf(string, ...any) {}
func (noopT) Logf(string, ...any)   {}
func (noopT) FailNow()              {}

// names of the revision cleanup counters, see revisioncleanup/metrics.go
const (
	revisionsDeleted = "reva_revision_cleanup_revisions_deleted_total"
	bytesDeleted     = "reva_revision_cleanup_bytes_deleted_total"
	purgeFailures    = "reva_revision_cleanup_purge_failures_total"
)

// counterValue reads a counter through the default gatherer, like a scrape would.
// The counters are global to the process, so tests compare against the value from before.
func counterValue(name string) float64 {
	families, err := prometheus.DefaultGatherer.Gather()
	Expect(err).ToNot(HaveOccurred())
	for _, family := range families {
		if family.GetName() == name && len(family.GetMetric()) > 0 {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}
	return 0
}
