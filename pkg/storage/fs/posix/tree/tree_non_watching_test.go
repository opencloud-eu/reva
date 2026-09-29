package tree_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
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
			return time.Unix(fi.Sys().(*syscall.Stat_t).Ctim.Unix())
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
})
