package tree_test

import (
	"os"
	"path/filepath"
	"time"

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

	It("picks up a file that was renamed on disk", func() {
		filePath := filepath.Join(spaceRoot, "original.txt")
		Expect(os.WriteFile(filePath, []byte("hello world"), 0644)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		b, err := xattr.Get(filePath, "user.oc.name")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(b)).To(Equal("original.txt"))

		renamedPath := filepath.Join(spaceRoot, "renamed.txt")
		Expect(os.Rename(filePath, renamedPath)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		b, err = xattr.Get(renamedPath, "user.oc.name")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(b)).To(Equal("renamed.txt"))
	})

	It("leaves the children of a renamed directory alone", func() {
		dir := filepath.Join(spaceRoot, "dir")
		Expect(os.MkdirAll(dir, 0755)).To(Succeed())
		filePath := filepath.Join(dir, "child.txt")
		Expect(os.WriteFile(filePath, []byte("hello world"), 0644)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		blobID, err := xattr.Get(filePath, "user.oc.blobid")
		Expect(err).ToNot(HaveOccurred())

		renamedDir := filepath.Join(spaceRoot, "renamed")
		Expect(os.Rename(dir, renamedDir)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		// the directory itself is renamed
		b, err := xattr.Get(renamedDir, "user.oc.name")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(b)).To(Equal("renamed"))

		// the file in it did not change, so it keeps its blob id and is not read again
		b, err = xattr.Get(filepath.Join(renamedDir, "child.txt"), "user.oc.blobid")
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(blobID))
	})

	It("picks up a file that changed on disk", func() {
		filePath := filepath.Join(spaceRoot, "changed.txt")
		Expect(os.WriteFile(filePath, []byte("hello world"), 0644)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		mtime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
		Expect(os.Chtimes(filePath, mtime, mtime)).To(Succeed())

		Expect(env.Tree.WarmupIDCache(spaceRoot, true, false)).To(Succeed())

		b, err := xattr.Get(filePath, "user.oc.mtime")
		Expect(err).ToNot(HaveOccurred())
		stored, err := time.Parse(time.RFC3339Nano, string(b))
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.UTC()).To(Equal(mtime.UTC()))
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
})
