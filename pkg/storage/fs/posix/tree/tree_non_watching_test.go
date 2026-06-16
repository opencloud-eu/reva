package tree_test

import (
	"os"
	"path/filepath"
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
})
