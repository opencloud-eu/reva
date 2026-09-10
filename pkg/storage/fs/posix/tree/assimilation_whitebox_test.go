// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("assimilationNode", func() {
	It("keeps LockHeld scoped to the owning goroutine", func() {
		n := &assimilationNode{spaceID: "space", nodeId: "node", path: "/tmp/node"}

		// The owning goroutine takes the lock.
		n.SetLockHeld(true)
		Expect(n.LockHeld()).To(BeTrue())

		// A different goroutine must not observe the same instance as lock-held.
		otherObservedHeld := make(chan bool, 1)
		go func() {
			otherObservedHeld <- n.LockHeld()
		}()
		Expect(<-otherObservedHeld).To(BeFalse())

		// Releasing in the owning goroutine clears ownership.
		n.SetLockHeld(false)
		Expect(n.LockHeld()).To(BeFalse())
	})
})

var _ = Describe("assimilation failures", func() {
	var (
		t       *Tree
		path    string
		failure = errors.New("permission denied")
	)

	lstat := func() os.FileInfo {
		fi, err := os.Lstat(path)
		Expect(err).ToNot(HaveOccurred())
		return fi
	}

	BeforeEach(func() {
		t = &Tree{assimilationFailures: expirable.NewLRU[string, assimilationFailure](0, nil, 0)}
		path = filepath.Join(GinkgoT().TempDir(), "file")
		Expect(os.WriteFile(path, []byte("data"), 0600)).To(Succeed())
	})

	It("skips an unchanged item until its retry delay has passed", func() {
		t.recordAssimilation(path, lstat(), failure)
		Expect(t.recentAssimilationFailure(path, lstat())).To(MatchError(failure))

		f, _ := t.assimilationFailures.Peek(path)
		f.retryAt = time.Now().Add(-time.Second)
		t.assimilationFailures.Add(path, f)
		Expect(t.recentAssimilationFailure(path, lstat())).To(Succeed())
	})

	It("doubles the retry delay on every failure up to a maximum", func() {
		t.recordAssimilation(path, lstat(), failure)
		t.recordAssimilation(path, lstat(), failure)
		f, _ := t.assimilationFailures.Peek(path)
		Expect(f.delay).To(Equal(2 * assimilationRetryMinDelay))

		for range 20 {
			t.recordAssimilation(path, lstat(), failure)
		}
		f, _ = t.assimilationFailures.Peek(path)
		Expect(f.delay).To(Equal(assimilationRetryMaxDelay))
	})

	It("retries an item as soon as it changed or once it was assimilated", func() {
		t.recordAssimilation(path, lstat(), failure)
		Expect(os.Chmod(path, 0400)).To(Succeed())
		Expect(t.recentAssimilationFailure(path, lstat())).To(Succeed())

		// replaced by another file with the same size, mode and mtime
		t.recordAssimilation(path, lstat(), failure)
		fi := lstat()
		Expect(os.WriteFile(path+".new", []byte("data"), fi.Mode())).To(Succeed())
		Expect(os.Chtimes(path+".new", fi.ModTime(), fi.ModTime())).To(Succeed())
		Expect(os.Rename(path+".new", path)).To(Succeed())
		Expect(t.recentAssimilationFailure(path, lstat())).To(Succeed())

		t.recordAssimilation(path, lstat(), failure)
		t.recordAssimilation(path, lstat(), nil)
		Expect(t.recentAssimilationFailure(path, lstat())).To(Succeed())
	})
})
