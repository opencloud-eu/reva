package tree_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

var _ = Describe("Revisions", func() {
	var (
		renv *helpers.TestEnv
	)

	BeforeEach(func() {
		var err error
		renv, err = helpers.NewTestEnv(map[string]any{"watch_fs": false})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if renv != nil {
			renv.Cleanup()
		}
	})

	// createFile creates a file in the space root with the given content
	createFile := func(name string, content []byte) *node.Node {
		n, err := renv.CreateTestFile(name, "blobid-"+name, renv.SpaceRootRes.OpaqueId, renv.SpaceRootRes.SpaceId, int64(len(content)))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(n.InternalPath(), content, 0600)).To(Succeed())
		return n
	}

	// createRevision stores the current content of n as a revision and returns the revision node
	createRevision := func(n *node.Node, version string) metadata.MetadataNode {
		_, err := renv.Tree.CreateRevision(renv.Ctx, n, version)
		Expect(err).ToNot(HaveOccurred())
		return node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+version, renv.Lookup)
	}

	tmpDirEntries := func() []os.DirEntry {
		entries, _ := os.ReadDir(filepath.Join(renv.Lookup.InternalSpaceRoot(renv.SpaceRootRes.SpaceId), blobstore.TMPDir))
		return entries
	}

	Describe("RestoreRevision", func() {
		It("restores the content and the blob metadata of the revision", func() {
			n := createFile("file.txt", []byte("old content"))
			rev := createRevision(n, "2020-01-01T00:00:00Z")
			Expect(os.WriteFile(n.InternalPath(), []byte("new content, longer"), 0600)).To(Succeed())
			Expect(renv.Lookup.MetadataBackend().Set(renv.Ctx, n, prefixes.BlobsizeAttr, []byte("19"))).To(Succeed())

			Expect(renv.Tree.RestoreRevision(renv.Ctx, rev, n, time.Now())).To(Succeed())

			Expect(os.ReadFile(n.InternalPath())).To(Equal([]byte("old content")))
			blobsize, err := renv.Lookup.MetadataBackend().GetInt64(renv.Ctx, n, prefixes.BlobsizeAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(blobsize).To(Equal(int64(len("old content"))))
			// the node metadata survives the restore
			id, err := renv.Lookup.MetadataBackend().Get(renv.Ctx, n, prefixes.IDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(id)).To(Equal(n.ID))
			Expect(tmpDirEntries()).To(BeEmpty())
		})

		It("leaves the target untouched when copying the revision fails", func() {
			n := createFile("file.txt", []byte("current content"))
			// a revision that cannot be read: its path is a directory, so reading it fails
			broken := node.NewBaseNode(n.SpaceID, n.ID+node.RevisionIDDelimiter+"2020-01-01T00:00:00Z", renv.Lookup)
			Expect(os.MkdirAll(broken.InternalPath(), 0700)).To(Succeed())

			Expect(renv.Tree.RestoreRevision(renv.Ctx, broken, n, time.Now())).ToNot(Succeed())

			Expect(os.ReadFile(n.InternalPath())).To(Equal([]byte("current content")))
			id, err := renv.Lookup.MetadataBackend().Get(renv.Ctx, n, prefixes.IDAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(id)).To(Equal(n.ID))
			Expect(tmpDirEntries()).To(BeEmpty())
		})

		It("never lets a concurrent reader see a partially restored file", func() {
			const size = 8 << 20
			contentA := bytes.Repeat([]byte("a"), size)
			contentB := bytes.Repeat([]byte("b"), size)

			n := createFile("big.bin", contentA)
			revA := createRevision(n, "2020-01-01T00:00:00Z")
			Expect(os.WriteFile(n.InternalPath(), contentB, 0600)).To(Succeed())
			revB := createRevision(n, "2020-01-02T00:00:00Z")

			var (
				stop    atomic.Bool
				bad     atomic.Int64
				reads   atomic.Int64
				wg      sync.WaitGroup
				badSize atomic.Int64
			)
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for !stop.Load() {
						f, err := os.Open(n.InternalPath())
						if err != nil {
							continue
						}
						data, err := io.ReadAll(f)
						_ = f.Close()
						if err != nil {
							continue
						}
						reads.Add(1)
						if !bytes.Equal(data, contentA) && !bytes.Equal(data, contentB) {
							bad.Add(1)
							badSize.Store(int64(len(data)))
						}
					}
				}()
			}

			for i := 0; i < 10; i++ {
				rev := revA
				if i%2 == 1 {
					rev = revB
				}
				Expect(renv.Tree.RestoreRevision(renv.Ctx, rev, n, time.Now())).To(Succeed())
			}
			stop.Store(true)
			wg.Wait()

			Expect(reads.Load()).To(BeNumerically(">", 0))
			Expect(bad.Load()).To(BeZero(), "%d of %d reads saw neither the old nor the new content (e.g. %d bytes)", bad.Load(), reads.Load(), badSize.Load())
		})
	})
})
