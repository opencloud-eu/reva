package jsoncs3

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewDefault", func() {
	newManager := func(skip bool) *Manager {
		mgr, err := NewDefault(map[string]interface{}{
			"gateway_addr":    "127.0.0.1:1",
			"provider_addr":   "127.0.0.1:1",
			"skip_migrations": skip,
		}, nil)
		Expect(err).ToNot(HaveOccurred())
		return mgr.(*Manager)
	}

	It("does not block writes when migrations are skipped", func() {
		Expect(newManager(true).migrationsRunning()).To(BeFalse())
	})

	It("blocks writes until migrations are done by default", func() {
		Expect(newManager(false).migrationsRunning()).To(BeTrue())
	})
})
