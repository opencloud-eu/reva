package decomposedfs

import (
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
)

var _ = Describe("removeSpaceRoot", func() {
	var parent, spaceRoot string

	BeforeEach(func() {
		parent = GinkgoT().TempDir()
		spaceRoot = filepath.Join(parent, "space")
		Expect(os.MkdirAll(filepath.Join(spaceRoot, "a", "b"), 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(spaceRoot, "a", "b", "file"), []byte("x"), 0600)).To(Succeed())
	})

	It("removes the space root and leaves no tombstone", func() {
		Expect(removeSpaceRoot(spaceRoot)).To(Succeed())
		entries, err := os.ReadDir(parent)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("succeeds when the space root is already gone", func() {
		Expect(os.RemoveAll(spaceRoot)).To(Succeed())
		Expect(removeSpaceRoot(spaceRoot)).To(Succeed())
	})

	It("is not disturbed by lock files created while it runs", func() {
		for range 50 {
			Expect(os.MkdirAll(filepath.Join(spaceRoot, "a", "b"), 0700)).To(Succeed())
			for i := range 200 {
				Expect(os.WriteFile(filepath.Join(spaceRoot, "a", "b", "f"+string(rune('a'+i%26))+string(rune('a'+i/26))), nil, 0600)).To(Succeed())
			}

			// mimic HybridBackend.Lock: create the metadata dirs level by level and a lock file
			stop := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				locks := filepath.Join(spaceRoot, lookup.MetadataDir, "locks")
				for {
					select {
					case <-stop:
						return
					default:
					}
					for _, dir := range []string{filepath.Dir(locks), locks} {
						_ = os.Mkdir(dir, 0700)
					}
					if f, err := os.OpenFile(filepath.Join(locks, "node.mlock"), os.O_RDWR|os.O_CREATE, 0600); err == nil {
						_ = f.Close()
					}
				}
			}()

			err := removeSpaceRoot(spaceRoot)
			close(stop)
			wg.Wait()
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(spaceRoot)
			Expect(os.IsNotExist(err)).To(BeTrue(), "space root must not be recreated")
			entries, err := os.ReadDir(parent)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty())
		}
	})
})
