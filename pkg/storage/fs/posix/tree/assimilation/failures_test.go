package assimilation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// chowned changes the owner that Sys reports for a file
type chowned struct {
	os.FileInfo
	st syscall.Stat_t
}

func (fi chowned) Sys() any { return &fi.st }

var _ = Describe("Failures", func() {
	var (
		f       *Failures
		path    string
		failure = errors.New("permission denied")
	)

	lstat := func() os.FileInfo {
		fi, err := os.Lstat(path)
		Expect(err).ToNot(HaveOccurred())
		return fi
	}

	BeforeEach(func() {
		f = NewFailures()
		path = filepath.Join(GinkgoT().TempDir(), "file")
		Expect(os.WriteFile(path, []byte("data"), 0600)).To(Succeed())
	})

	It("skips an unchanged item until its retry delay has passed", func() {
		f.Record(path, lstat(), failure)
		Expect(f.Recent(path, lstat())).To(MatchError(failure))

		last, _ := f.lru.Peek(path)
		last.retryAt = time.Now().Add(-time.Second)
		f.lru.Add(path, last)
		Expect(f.Recent(path, lstat())).To(Succeed())
	})

	It("doubles the retry delay on every failure up to a maximum", func() {
		f.Record(path, lstat(), failure)
		f.Record(path, lstat(), failure)
		last, _ := f.lru.Peek(path)
		Expect(last.delay).To(Equal(2 * minRetryDelay))

		for range 20 {
			f.Record(path, lstat(), failure)
		}
		last, _ = f.lru.Peek(path)
		Expect(last.delay).To(Equal(maxRetryDelay))
	})

	It("retries an item as soon as it changed or once it was assimilated", func() {
		f.Record(path, lstat(), failure)
		Expect(os.Chmod(path, 0400)).To(Succeed())
		Expect(f.Recent(path, lstat())).To(Succeed())

		f.Record(path, lstat(), failure)
		fi := chowned{FileInfo: lstat(), st: *lstat().Sys().(*syscall.Stat_t)}
		Expect(f.Recent(path, fi)).To(MatchError(failure))
		fi.st.Uid++
		Expect(f.Recent(path, fi)).To(Succeed())
		fi.st.Uid--
		fi.st.Gid++
		Expect(f.Recent(path, fi)).To(Succeed())

		f.Record(path, lstat(), failure)
		f.Record(path, lstat(), nil)
		Expect(f.Recent(path, lstat())).To(Succeed())
	})

	It("retries an item after a fix that only shows in its ctime", func() {
		f.Record(path, lstat(), failure)
		Expect(f.Recent(path, lstat())).To(MatchError(failure))

		// setfacl and chattr change none of the attributes above, but they do change the ctime.
		// A chmod to the mode the file already has does the same. Some kernels stamp the ctime
		// from a coarse clock, so repeat the chmod until the ctime really moved.
		before := cTime(lstat())
		Eventually(func() bool {
			Expect(os.Chmod(path, 0600)).To(Succeed())
			return cTime(lstat()).After(before)
		}).Should(BeTrue())
		Expect(f.Recent(path, lstat())).To(Succeed())

		// the fix did not work, and the delay keeps doubling instead of starting over
		f.Record(path, lstat(), failure)
		last, _ := f.lru.Peek(path)
		Expect(last.delay).To(Equal(2 * minRetryDelay))
	})

	It("keeps the stored failures when it is full", func() {
		for i := range maxFailures {
			f.Record(fmt.Sprintf("/failed/%d", i), lstat(), failure)
		}
		f.Record(path, lstat(), failure)
		Expect(f.Recent(path, lstat())).To(Succeed())
		Expect(f.Recent("/failed/0", lstat())).To(MatchError(failure))
	})
})
