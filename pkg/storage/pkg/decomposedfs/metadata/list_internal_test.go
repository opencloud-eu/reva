// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/xattr"
)

var _ = Describe("listXattr", func() {
	Describe("isRangeError", func() {
		It("reports ERANGE as a range error", func() {
			Expect(isRangeError(&xattr.Error{Op: "xattr.list", Err: syscall.ERANGE})).To(BeTrue())
		})

		It("reports E2BIG as a range error", func() {
			Expect(isRangeError(&xattr.Error{Op: "xattr.list", Err: syscall.E2BIG})).To(BeTrue())
		})

		It("does not report ENOENT as a range error", func() {
			Expect(isRangeError(&xattr.Error{Op: "xattr.list", Err: syscall.ENOENT})).To(BeFalse())
		})

		It("does not report a nil error as a range error", func() {
			Expect(isRangeError(nil)).To(BeFalse())
		})
	})

	// xattr.List is non-atomic (a listxattr size query followed by a read into a
	// buffer sized from that query), so a concurrent write that grows the name
	// list makes the raw call return ERANGE. The wrapper must retry and never
	// surface that transient error.
	It("survives a concurrent writer that grows the attribute name list", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "f")
		Expect(os.WriteFile(path, nil, 0600)).To(Succeed())

		if err := xattr.Set(path, "user.seed", []byte("1")); err != nil {
			Skip(fmt.Sprintf("extended attributes not supported on %s: %v", dir, err))
		}

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				key := fmt.Sprintf("user.k%d", i%16)
				if i%2 == 0 {
					_ = xattr.Set(path, key, []byte("value"))
				} else {
					_ = xattr.Remove(path, key)
				}
			}
		}()
		defer func() {
			close(stop)
			wg.Wait()
		}()

		for i := 0; i < 20000; i++ {
			_, err := listXattr(path)
			Expect(err).ToNot(HaveOccurred(), "listXattr must retry transient ERANGE from concurrent writes")
		}
	})
})
