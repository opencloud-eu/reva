// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

var _ = Describe("CanonicalMail", func() {
	DescribeTable("accepts valid addresses",
		func(in, expected string) {
			actual, err := utils.CanonicalMail(in)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry("plain address", "guest@example.com", "guest@example.com"),
		Entry("lowercases ASCII", "Guest@Example.COM", "guest@example.com"),
		Entry("keeps ASCII domains as they are", "guest@my_host.example.com", "guest@my_host.example.com"),
		Entry("converts international domains to punycode", "bob@bücher.example", "bob@xn--bcher-kva.example"),
		Entry("dotted capital I in the domain", "bob@İnfocorp.com", "bob@xn--infocorp-o0e.com"),
		Entry("look-alike domain becomes visible", "bob@еxample.com", "bob@xn--xample-2of.com"), // Cyrillic e
	)

	DescribeTable("rejects invalid addresses",
		func(in string) {
			_, err := utils.CanonicalMail(in)
			Expect(err).To(HaveOccurred())
		},
		Entry("non-ASCII local part", "bоb@example.com"), // Cyrillic o
		Entry("Kelvin sign in the local part", "Kate@corp.com"),
		Entry("display name", "Guest <guest@example.com>"),
		Entry("quoted local part", `"guest"@example.com`),
		Entry("surrounding whitespace", " guest@example.com"),
		Entry("invalid international domain", "bob@İnfo--corp‍.com"),
	)
})
