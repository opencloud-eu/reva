// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

var _ = Describe("BaseNode lock ownership", func() {
	It("keeps LockHeld scoped to the owning goroutine", func() {
		n := node.NewBaseNode("spaceid", "nodeid", nil)

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

	It("stays race-free under concurrent LockHeld reads", func() {
		n := node.NewBaseNode("spaceid", "nodeid", nil)

		var wg sync.WaitGroup
		errs := make(chan error, 16)

		// Readers from other goroutines: they are not owners and must always observe false.
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 1000 {
					if n.LockHeld() {
						errs <- fmt.Errorf("non-owning goroutine unexpectedly observed LockHeld() == true")
						return
					}
				}
			}()
		}

		// The owner repeatedly acquires and releases.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				n.SetLockHeld(true)
				_ = n.LockHeld()
				n.SetLockHeld(false)
			}
		}()

		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).ToNot(HaveOccurred())
		}
	})
})
