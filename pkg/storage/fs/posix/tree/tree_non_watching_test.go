package tree_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"

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

	It("finishes a move when the request is cancelled after the rename", func() {
		Expect(os.MkdirAll(root+"/source/dir", 0700)).To(Succeed())
		Expect(os.Mkdir(root+"/target", 0700)).To(Succeed())
		Expect(os.WriteFile(root+"/source/dir/file.txt", []byte("hello world"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())

		nodeFor := func(path string) *node.Node {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + path,
			})
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		treeSize := func(path string) uint64 {
			size, err := nodeFor(path).GetTreeSize(non_watching_env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			return size
		}
		Expect(treeSize("/source")).To(Equal(uint64(11)))

		ctx, cancel := context.WithCancel(non_watching_env.Ctx)
		cancel()
		Expect(non_watching_env.Tree.Move(ctx, nodeFor("/source/dir"), nodeFor("/target/dir"))).To(Succeed())

		Expect(treeSize("/source")).To(Equal(uint64(0)))
		Expect(treeSize("/target")).To(Equal(uint64(11)))
		Expect(treeSize("/target/dir")).To(Equal(uint64(11)))
		Expect(treeSize("")).To(Equal(uint64(11)))
	})

	It("moves the tree size a folder has at the time of the move", func() {
		Expect(os.MkdirAll(root+"/source/dir", 0700)).To(Succeed())
		Expect(os.Mkdir(root+"/target", 0700)).To(Succeed())
		Expect(os.WriteFile(root+"/source/dir/file.txt", []byte("hello world"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())

		nodeFor := func(path string) *node.Node {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + path,
			})
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		treeSize := func(path string) uint64 {
			size, err := nodeFor(path).GetTreeSize(non_watching_env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			return size
		}

		// the request reads the folder before the move, then a change below it propagates
		oldNode := nodeFor("/source/dir")
		size, err := oldNode.GetTreeSize(non_watching_env.Ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(Equal(uint64(11)))
		Expect(non_watching_env.Tree.Propagate(non_watching_env.Ctx, nodeFor("/source/dir/file.txt"), 5)).To(Succeed())
		Expect(treeSize("/source")).To(Equal(uint64(16)))

		Expect(non_watching_env.Tree.Move(non_watching_env.Ctx, oldNode, nodeFor("/target/dir"))).To(Succeed())

		Expect(treeSize("/source")).To(Equal(uint64(0)))
		Expect(treeSize("/target")).To(Equal(uint64(16)))
		Expect(treeSize("")).To(Equal(uint64(16)))
	})

	It("renames an item and updates its parent id under the item's lock", func() {
		Expect(os.Mkdir(root+"/source", 0700)).To(Succeed())
		Expect(os.Mkdir(root+"/target", 0700)).To(Succeed())
		Expect(os.WriteFile(root+"/source/file.txt", []byte("hello world"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())

		nodeFor := func(path string) *node.Node {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + path,
			})
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		oldNode, newNode := nodeFor("/source/file.txt"), nodeFor("/target/file.txt")

		// hold the lock the watcher would take to assimilate the item
		unlock, err := non_watching_env.Lookup.MetadataBackend().Lock(node.NewBaseNode(oldNode.SpaceID, oldNode.ID, non_watching_env.Lookup))
		Expect(err).ToNot(HaveOccurred())
		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			Expect(non_watching_env.Tree.Move(non_watching_env.Ctx, oldNode, newNode)).To(Succeed())
		}()
		// the move waits for the lock before it renames the item
		Consistently(func() error {
			_, err := os.Stat(root + "/source/file.txt")
			return err
		}, 500*time.Millisecond).Should(Succeed())
		Expect(unlock()).To(Succeed())
		Eventually(done).Should(BeClosed())

		_, err = os.Stat(root + "/target/file.txt")
		Expect(err).ToNot(HaveOccurred())
	})

	It("updates the parent id of a moved item whose old parent the walk visits later", func() {
		Expect(os.Mkdir(root+"/z-source", 0700)).To(Succeed())
		Expect(os.Mkdir(root+"/a-target", 0700)).To(Succeed())
		Expect(os.WriteFile(root+"/z-source/file.txt", []byte("hello world"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())

		treeSize := func(path string) uint64 {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + path,
			})
			Expect(err).ToNot(HaveOccurred())
			size, err := n.GetTreeSize(non_watching_env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			return size
		}

		// moved while nothing watched. The id cache has lost the old parent, like a cold cache at startup,
		// so the walk only learns where the old parent is after it visited the new one.
		Expect(os.Rename(root+"/z-source/file.txt", root+"/a-target/file.txt")).To(Succeed())
		Expect(non_watching_env.Lookup.IDCache.DeleteByPath(non_watching_env.Ctx, root+"/z-source")).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())
		Expect(treeSize("/a-target")).To(Equal(uint64(11)))

		// the next assimilation of the file must only add its change in size
		Expect(os.WriteFile(root+"/a-target/file.txt", []byte("hello world!!"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.Assimilate(root + "/a-target/file.txt")).To(Succeed())

		Expect(treeSize("/a-target")).To(Equal(uint64(13)))
		Expect(treeSize("/z-source")).To(Equal(uint64(0)))
		Expect(treeSize("")).To(Equal(uint64(13)))
	})

	It("does not move the size again after a full scan picked up a move on disk", func() {
		Expect(os.Mkdir(root+"/source", 0700)).To(Succeed())
		Expect(os.Mkdir(root+"/target", 0700)).To(Succeed())
		Expect(os.WriteFile(root+"/source/file.txt", []byte("hello world"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())

		nodeFor := func(path string) *node.Node {
			n, err := non_watching_env.Lookup.NodeFromResource(non_watching_env.Ctx, &provider.Reference{
				ResourceId: non_watching_env.SpaceRootRes,
				Path:       subtree + path,
			})
			Expect(err).ToNot(HaveOccurred())
			return n
		}
		treeSize := func(path string) uint64 {
			size, err := nodeFor(path).GetTreeSize(non_watching_env.Ctx)
			Expect(err).ToNot(HaveOccurred())
			return size
		}

		// moved while nothing watched, then picked up by a full scan like the one at startup
		Expect(os.Rename(root+"/source/file.txt", root+"/target/file.txt")).To(Succeed())
		Expect(non_watching_env.Tree.WarmupIDCache(root, true, false)).To(Succeed())
		Expect(treeSize("/source")).To(Equal(uint64(0)))
		Expect(treeSize("/target")).To(Equal(uint64(11)))

		// the next assimilation of the file must only add its change in size
		Expect(os.WriteFile(root+"/target/file.txt", []byte("hello world!!"), 0600)).To(Succeed())
		Expect(non_watching_env.Tree.Assimilate(root + "/target/file.txt")).To(Succeed())

		Expect(treeSize("/target")).To(Equal(uint64(13)))
		Expect(treeSize("/source")).To(Equal(uint64(0)))
		Expect(treeSize("")).To(Equal(uint64(13)))
	})
})
