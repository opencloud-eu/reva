package tree_test

import (
	"os"
	"path/filepath"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

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
})
