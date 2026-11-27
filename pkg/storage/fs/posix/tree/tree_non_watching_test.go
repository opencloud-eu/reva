package tree_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/blobstore"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/lookup"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/timemanager"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/tree"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	"github.com/pkg/xattr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Non-watching tree", func() {
	var (
		subtree string
	)

	BeforeEach(func() {
		SetDefaultEventuallyTimeout(15 * time.Second)

		var err error
		subtree, err = generateRandomString(10)
		subtree = "/" + subtree
		root = non_watching_env.Root + "/users/" + non_watching_env.Owner.Username + subtree
		Expect(err).ToNot(HaveOccurred())
		child, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree,
		})
		Expect(err).ToNot(HaveOccurred())
		err = non_watching_env.Tree.CreateDir(non_watching_env.Ctx, child)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("TouchFile", func() {
		// the propagated attribute, not GetTMTime: that falls back to the dir's own
		// mtime, which creating the file changes
		parentTMTime := func() string {
			parent, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree,
			})
			Expect(err).ToNot(HaveOccurred())
			tmtime, _ := parent.XattrString(non_watching_env.Ctx, prefixes.TreeMTimeAttr)
			return tmtime
		}

		touch := func(ctx context.Context) {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + "/newFile",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(non_watching_env.Tree.TouchFile(ctx, n, false, "")).To(Succeed())
		}

		// A file can sit under the name before the node is known, e.g. one the watcher has not
		// assimilated yet. TouchFile used to truncate it with os.Create.
		It("leaves a file that already has the name alone", func() {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + "/taken",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(n.Exists).To(BeFalse())
			nodePath := filepath.Join(n.ParentPath(), n.Name)
			Expect(os.WriteFile(nodePath, []byte("not ours"), 0600)).To(Succeed())

			err = non_watching_env.Tree.TouchFile(non_watching_env.Ctx, n, false, "")
			Expect(err).To(BeAssignableToTypeOf(errtypes.AlreadyExists("")))

			content, err := os.ReadFile(nodePath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("not ours"))
			_, err = xattr.Get(nodePath, prefixes.IDAttr)
			Expect(err).To(HaveOccurred(), "the file must not get our id")
			_, err = non_watching_env.Lookup.IDCache.Get(non_watching_env.Ctx, n.SpaceID, n.ID)
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")), "our id must not be cached")
			_, err = os.Stat(filepath.Join(n.SpaceRoot.InternalPath(), blobstore.TMPDir, ".new-"+n.ID))
			Expect(os.IsNotExist(err)).To(BeTrue(), "the temp name must be gone")
		})

		It("propagates the new file to its parent", func() {
			before := parentTMTime()
			time.Sleep(10 * time.Millisecond)

			touch(non_watching_env.Ctx)

			Expect(parentTMTime()).ToNot(Equal(before))
		})

		It("does not propagate when the context asks it not to", func() {
			before := parentTMTime()
			time.Sleep(10 * time.Millisecond)

			touch(storage.ContextSkipTouchPropagation(non_watching_env.Ctx))

			Expect(parentTMTime()).To(Equal(before))
		})
	})

	It("updates treesize after ListFolder on subdirectory with new file", func() {
		subDirName := "subdir"
		subDirPath := filepath.Join(root, subDirName)
		child, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/" + subDirName,
		})
		Expect(err).ToNot(HaveOccurred())
		err = non_watching_env.Tree.CreateDir(non_watching_env.Ctx, child)
		Expect(err).ToNot(HaveOccurred())

		// get initial treesize of the parent
		parentNode, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/" + subDirName,
		})
		Expect(err).ToNot(HaveOccurred())
		initialSize, err := parentNode.GetTreeSize(non_watching_env.Ctx)
		Expect(err).ToNot(HaveOccurred())

		// create a file in the subdirectory
		fileName := "testfile"
		content := []byte("some content")
		fileSize := uint64(len(content))
		err = os.WriteFile(subDirPath+"/"+fileName, content, 0600)
		Expect(err).ToNot(HaveOccurred())

		// verify treesize of parent didn't change yet
		parentNode, err = non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/" + subDirName,
		})
		Expect(err).ToNot(HaveOccurred())
		currentSize, err := parentNode.GetTreeSize(non_watching_env.Ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentSize).To(Equal(initialSize))

		// verify treesize of parent didn't change yet
		childNode, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/" + subDirName,
		})
		Expect(err).ToNot(HaveOccurred())
		// call ListFolder for the subdirectory
		_, err = non_watching_env.Tree.ListFolder(non_watching_env.Ctx, childNode)
		Expect(err).ToNot(HaveOccurred())

		// verify new file was assimilated
		Eventually(func(g Gomega) {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + "/" + subDirName + "/" + fileName,
			})
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(n.Exists).To(BeTrue())
		}).Should(Succeed())

		// verify treesize was updated
		Eventually(func(g Gomega) {
			parentNode, err = non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + "/" + subDirName,
			})
			g.Expect(err).ToNot(HaveOccurred())
			newSize, err := parentNode.GetTreeSize(non_watching_env.Ctx)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(newSize).To(Equal(initialSize + fileSize))
		}).Should(Succeed())
	})

	It("retries assimilating a file that failed only after it changed", func() {
		if os.Geteuid() == 0 {
			Skip("root can set extended attributes on read-only files")
		}
		// assimilation reads a read-only file for its checksums, then fails to set its xattrs.
		// The file is big enough that the bytes below can only come from reading it.
		path := filepath.Join(root, "readonly")
		content := make([]byte, 4<<20)
		Expect(os.WriteFile(path, content, 0400)).To(Succeed())

		// reads reports whether fn read the file, based on the bytes this process read. The access
		// time can't tell us, because changing it also changes the ctime, which triggers a retry.
		rchar := func() int64 {
			procIO, err := os.ReadFile("/proc/self/io")
			if err != nil {
				Skip("/proc/self/io is not available")
			}
			var read int64
			_, err = fmt.Sscanf(string(procIO), "rchar: %d", &read)
			Expect(err).ToNot(HaveOccurred())
			return read
		}
		reads := func(fn func()) bool {
			before := rchar()
			fn()
			return rchar()-before >= int64(len(content))
		}
		Expect(reads(func() { _, _ = os.ReadFile(path) })).To(BeTrue())

		listFolder := func() {
			dir, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree,
			})
			Expect(err).ToNot(HaveOccurred())
			_, err = non_watching_env.Tree.ListFolder(non_watching_env.Ctx, dir)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(reads(listFolder)).To(BeTrue())
		Expect(reads(listFolder)).To(BeFalse())

		// a fix that changes none of the attributes the failure cache keeps, a setfacl or a
		// chattr -i for example, still changes the ctime and gets the file read again. Some
		// kernels stamp the ctime from a coarse clock, so chmod until it really moved.
		ctime := func() time.Time {
			fi, err := os.Lstat(path)
			Expect(err).ToNot(HaveOccurred())
			ts := timemanager.StatCTime(fi.Sys().(*syscall.Stat_t))
			//nolint:unconvert
			return time.Unix(int64(ts.Sec), int64(ts.Nsec))
		}
		before := ctime()
		Eventually(func() bool {
			Expect(os.Chmod(path, 0400)).To(Succeed())
			return ctime().After(before)
		}).Should(BeTrue())
		Expect(reads(listFolder)).To(BeTrue())
	})

	It("rejects creation of internal paths", func() {
		spaceRoot := non_watching_env.Root + "/users/" + non_watching_env.Owner.Username

		ignoredParentPath := filepath.Join(spaceRoot, lookup.MetadataDir)
		err := non_watching_env.Lookup.CacheID(non_watching_env.Ctx, non_watching_env.SpaceRootRes.SpaceId, "ignored-parent-id", ignoredParentPath)
		Expect(err).ToNot(HaveOccurred())

		// Test TouchFile and InitNewNode on a file inside the metadata folder
		ignoredFileNode := node.New(
			non_watching_env.SpaceRootRes.SpaceId,
			"some-ignored-file-id",
			"ignored-parent-id",
			"some-file.txt",
			0,
			"",
			provider.ResourceType_RESOURCE_TYPE_FILE,
			non_watching_env.Owner.Id,
			non_watching_env.Lookup,
		)

		// 1. Verify that InitNewNode fails on ignored file node with PermissionDenied
		_, err = non_watching_env.Tree.InitNewNode(non_watching_env.Ctx, ignoredFileNode, 0)
		Expect(err).To(HaveOccurred())
		_, ok := err.(errtypes.IsPermissionDenied)
		Expect(ok).To(BeTrue())

		// 2. Verify that TouchFile fails on ignored file node with PermissionDenied
		err = non_watching_env.Tree.TouchFile(non_watching_env.Ctx, ignoredFileNode, false, "")
		Expect(err).To(HaveOccurred())
		_, ok = err.(errtypes.IsPermissionDenied)
		Expect(ok).To(BeTrue())

		// Test TouchFile and InitNewNode on the metadata folder itself (substituting metadata folder as a node under spaceRoot)
		// Cache the spaceRoot as a parent ID
		err = non_watching_env.Lookup.CacheID(non_watching_env.Ctx, non_watching_env.SpaceRootRes.SpaceId, "space-root-id", spaceRoot)
		Expect(err).ToNot(HaveOccurred())

		ignoredFolderNode := node.New(
			non_watching_env.SpaceRootRes.SpaceId,
			"some-ignored-folder-id",
			"space-root-id",
			lookup.MetadataDir,
			0,
			"",
			provider.ResourceType_RESOURCE_TYPE_CONTAINER,
			non_watching_env.Owner.Id,
			non_watching_env.Lookup,
		)

		// 3. Verify that InitNewNode fails on ignored folder node with PermissionDenied
		_, err = non_watching_env.Tree.InitNewNode(non_watching_env.Ctx, ignoredFolderNode, 0)
		Expect(err).To(HaveOccurred())
		_, ok = err.(errtypes.IsPermissionDenied)
		Expect(ok).To(BeTrue())

		// 4. Verify that TouchFile fails on ignored folder node with PermissionDenied
		err = non_watching_env.Tree.TouchFile(non_watching_env.Ctx, ignoredFolderNode, false, "")
		Expect(err).To(HaveOccurred())
		_, ok = err.(errtypes.IsPermissionDenied)
		Expect(ok).To(BeTrue())

		// 5. Verify that CreateDir fails on ignored folder node with PermissionDenied
		err = non_watching_env.Tree.CreateDir(non_watching_env.Ctx, ignoredFolderNode)
		Expect(err).To(HaveOccurred())
		_, ok = err.(errtypes.IsPermissionDenied)
		Expect(ok).To(BeTrue())
	})

	// The watcher can miss the reservation of a new folder, e.g. when the id cache read fails or
	// comes from a replica that lags behind. The folder must still end up with one id.
	It("takes over the id of a new folder that was assimilated before it got its id", func() {
		n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/folder",
		})
		Expect(err).ToNot(HaveOccurred())
		n.ID = uuid.NewString()
		ourID := n.ID
		path := filepath.Join(n.ParentPath(), n.Name)

		// the watcher finds the folder before it has our id and doesn't see the reservation
		non_watching_env.Tree.OnDirCreatedForTest(func(p string) {
			if p != path {
				return
			}
			Expect(non_watching_env.Lookup.IDCache.DeletePath(non_watching_env.Ctx, p)).To(Succeed())
			Expect(non_watching_env.Tree.AssimilateForTest(p)).To(Succeed())
		})
		DeferCleanup(func() { non_watching_env.Tree.OnDirCreatedForTest(nil) })

		Expect(non_watching_env.Tree.CreateDir(non_watching_env.Ctx, n)).To(Succeed())

		// the folder keeps the id of the assimilation, on disk, in the cache and in the node
		idOnDisk, err := xattr.Get(path, prefixes.IDAttr)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(idOnDisk)).ToNot(Equal(ourID))
		Expect(n.ID).To(Equal(string(idOnDisk)))
		_, cachedID, err := non_watching_env.Lookup.IDsForPath(non_watching_env.Ctx, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cachedID).To(Equal(n.ID))
		_, err = non_watching_env.Lookup.GetCachedID(non_watching_env.Ctx, n.SpaceID, ourID)
		Expect(err).To(HaveOccurred(), "our id must not be cached")

		// with the metadata of the request
		resolved, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
			ResourceId: non_watching_env.SpaceRootRes,
			Path:       subtree + "/folder",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(resolved.ID).To(Equal(n.ID))
		Expect(resolved.Name).To(Equal("folder"))
		Expect(resolved.ParentID).To(Equal(n.ParentID))

		// and both locks are free
		for _, id := range []string{ourID, n.ID} {
			unlock, err := non_watching_env.Lookup.MetadataBackend().Lock(node.New(n.SpaceID, id, "", "", 0, "", provider.ResourceType_RESOURCE_TYPE_CONTAINER, nil, non_watching_env.Lookup))
			Expect(err).ToNot(HaveOccurred())
			Expect(unlock()).To(Succeed())
		}
	})

	It("makes a second assimilation of a new file wait until the first has cached it", func() {
		parentSize := func() uint64 {
			parent, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree,
			})
			Expect(err).ToNot(HaveOccurred())
			size, err := parent.GetTreeSize(non_watching_env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			return size
		}
		initialSize := parentSize()

		path := filepath.Join(root, "file")
		content := make([]byte, 4096)
		Expect(os.WriteFile(path, content, 0600)).To(Succeed())

		// a second listing finds the file once the first one has claimed an id for it
		second := make(chan error, 1)
		secondFinishedEarly := false
		non_watching_env.Tree.OnIDClaimedForTest(func(p string) {
			if p != path {
				return
			}
			go func() { second <- non_watching_env.Tree.AssimilateForTest(path) }()
			select {
			case <-second:
				secondFinishedEarly = true
			case <-time.After(200 * time.Millisecond):
			}
		})
		DeferCleanup(func() { non_watching_env.Tree.OnIDClaimedForTest(nil) })

		Expect(non_watching_env.Tree.AssimilateForTest(path)).To(Succeed())
		Expect(secondFinishedEarly).To(BeFalse(), "the second assimilation didn't wait for the first")
		Eventually(second).Should(Receive(MatchError(tree.ErrSkipAlreadyKnown)))

		Eventually(parentSize).Should(Equal(initialSize + uint64(len(content))))
		Consistently(parentSize, 200*time.Millisecond).Should(Equal(initialSize + uint64(len(content))))
	})

	It("does not change the tree size when a file fails to assimilate", func() {
		if os.Geteuid() == 0 {
			Skip("root can set extended attributes on read-only files")
		}
		// assimilation can't set the extended attributes of a read-only file
		Expect(os.WriteFile(filepath.Join(root, "readonly"), []byte("some content"), 0400)).To(Succeed())

		ref := &provider.Reference{ResourceId: non_watching_env.SpaceRootRes, Path: subtree}
		dir, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		_, err = non_watching_env.Tree.ListFolder(non_watching_env.Ctx, dir)
		Expect(err).ToNot(HaveOccurred())

		dir, err = non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(dir.GetTreeSize(non_watching_env.Ctx)).To(BeZero())
	})
})
