package tree_test

import (
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/xattr"

	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
)

var _ = Describe("WarmupIDCache", func() {
	var (
		env       *posixhelpers.TestEnv
		tmpDir    string
		spaceRoot string
	)

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		tmpDir = env.Root
		spaceRoot = filepath.Join(tmpDir, "users", "username")
	})

	AfterEach(func() {
		env.Cleanup()
	})

	It("returns nil for an empty directory", func() {
		err := env.Tree.WarmupIDCache(spaceRoot, false, false)
		Expect(err).ToNot(HaveOccurred())
	})

	It("picks up new files and directories", func() {
		subDir := filepath.Join(spaceRoot, "sub")
		err := os.MkdirAll(subDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		filePath := filepath.Join(subDir, "test.txt")
		err = os.WriteFile(filePath, []byte("hello world"), 0644)
		Expect(err).ToNot(HaveOccurred())

		err = env.Tree.WarmupIDCache(spaceRoot, false, false)
		Expect(err).ToNot(HaveOccurred())
	})

	It("verifies tree sizes and recursion", func() {
		subDir := filepath.Join(spaceRoot, "sub2")
		err := os.MkdirAll(subDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		nestedDir := filepath.Join(subDir, "nested")
		err = os.MkdirAll(nestedDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		filePath := filepath.Join(nestedDir, "test.txt")
		err = os.WriteFile(filePath, []byte("hello world"), 0644) // 11 bytes
		Expect(err).ToNot(HaveOccurred())

		err = env.Tree.WarmupIDCache(spaceRoot, true, false)
		Expect(err).ToNot(HaveOccurred())

		// verify that tree sizes are updated
		// Since we used assimilate=true, the treesize xattr on sub2 and nested should be 11.
		b, err := xattr.Get(subDir, "user.oc.treesize")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(b)).To(Equal("11"))

		b, err = xattr.Get(nestedDir, "user.oc.treesize")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(b)).To(Equal("11"))
	})

	treesize := func(path string) int64 {
		b, err := xattr.Get(path, "user.oc.treesize")
		Expect(err).ToNot(HaveOccurred())
		size, err := strconv.ParseInt(string(b), 10, 64)
		Expect(err).ToNot(HaveOccurred())
		return size
	}

	Context("with onlyDirty", func() {
		var (
			clean, dirty string
			rootSize     int64
		)

		BeforeEach(func() {
			clean = filepath.Join(spaceRoot, "clean")
			dirty = filepath.Join(spaceRoot, "dirty")
			Expect(os.MkdirAll(filepath.Join(clean, "nested"), 0755)).To(Succeed())
			Expect(os.MkdirAll(dirty, 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(clean, "nested", "a.txt"), []byte("hello world"), 0644)).To(Succeed()) // 11 bytes
			Expect(os.WriteFile(filepath.Join(dirty, "b.txt"), []byte("hello"), 0644)).To(Succeed())                 // 5 bytes
			Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())
			rootSize = treesize(spaceRoot)
		})

		It("keeps the tree size of clean directories it skips", func() {
			Expect(os.Remove(filepath.Join(dirty, "b.txt"))).To(Succeed())
			Expect(xattr.Set(dirty, "user.oc.dirty", []byte("true"))).To(Succeed())
			Expect(xattr.Set(spaceRoot, "user.oc.dirty", []byte("true"))).To(Succeed())

			Expect(env.Tree.WarmupIDCache(spaceRoot, true, true)).To(Succeed())

			Expect(treesize(clean)).To(Equal(int64(11)))
			Expect(treesize(filepath.Join(clean, "nested"))).To(Equal(int64(11)))
			Expect(treesize(dirty)).To(Equal(int64(0)))
			Expect(treesize(spaceRoot)).To(Equal(rootSize - 5))
		})

		It("walks a clean directory without a valid tree size", func() {
			Expect(xattr.Set(clean, "user.oc.treesize", []byte("-1"))).To(Succeed())
			Expect(xattr.Set(spaceRoot, "user.oc.dirty", []byte("true"))).To(Succeed())

			Expect(env.Tree.WarmupIDCache(spaceRoot, true, true)).To(Succeed())

			Expect(treesize(clean)).To(Equal(int64(11)))
			Expect(treesize(spaceRoot)).To(Equal(rootSize))
		})

		It("does not change anything when the root is clean", func() {
			Expect(env.Tree.WarmupIDCache(clean, false, true)).To(Succeed())

			Expect(treesize(clean)).To(Equal(int64(11)))
			Expect(treesize(spaceRoot)).To(Equal(rootSize))
		})
	})
})

var _ = Describe("WarmupIDCache with a space path template that starts with an action", func() {
	var (
		env       *posixhelpers.TestEnv
		spaceRoot string
	)

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(map[string]interface{}{
			"generalspacepath_template": "{{.SpaceType}}/{{.SpaceId}}",
		})
		Expect(err).ToNot(HaveOccurred())
		space, err := env.CreateTestStorageSpace("project", nil)
		Expect(err).ToNot(HaveOccurred())
		spaceRoot = filepath.Join(env.Root, "project", space.SpaceId)
		Expect(spaceRoot).To(BeADirectory())
	})

	AfterEach(func() {
		env.Cleanup()
	})

	It("does not assimilate the internal directories of a space", func() {
		Expect(os.MkdirAll(filepath.Join(spaceRoot, "sub"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(spaceRoot, "sub", "test.txt"), []byte("hello world"), 0644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(spaceRoot, ".Trash", "files"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(spaceRoot, ".Trash", "files", "deleted.txt"), []byte("hello"), 0644)).To(Succeed())
		name, err := xattr.Get(spaceRoot, "user.oc.name")
		Expect(err).ToNot(HaveOccurred())

		Expect(env.Tree.WarmupIDCache(env.Root, true, false)).To(Succeed())

		_, err = xattr.Get(filepath.Join(spaceRoot, "sub", "test.txt"), "user.oc.id")
		Expect(err).ToNot(HaveOccurred())
		for _, p := range []string{".Trash", ".Trash/files", ".Trash/files/deleted.txt", ".oc-nodes"} {
			_, err = xattr.Get(filepath.Join(spaceRoot, p), "user.oc.id")
			Expect(err).To(HaveOccurred(), p)
		}
		// the space root keeps its name and the directory above it is not a node
		Expect(xattr.Get(spaceRoot, "user.oc.name")).To(Equal(name))
		_, err = xattr.Get(filepath.Join(env.Root, "project"), "user.oc.id")
		Expect(err).To(HaveOccurred())
		// file1 of the test env (1234 bytes) and test.txt (11 bytes), without the trash
		Expect(xattr.Get(spaceRoot, "user.oc.treesize")).To(Equal([]byte("1245")))
	})
})
