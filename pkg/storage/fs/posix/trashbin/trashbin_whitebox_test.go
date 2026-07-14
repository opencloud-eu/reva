// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package trashbin

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTrashbinWhitebox(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Trashbin Whitebox Suite")
}

var _ = Describe("trashNode", func() {
	It("keeps LockHeld scoped to the owning goroutine", func() {
		n := &trashNode{spaceID: "space", id: "node", path: "/tmp/node"}

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
