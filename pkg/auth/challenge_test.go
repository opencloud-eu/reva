// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/auth"
)

var _ = Describe("Challenge", func() {
	DescribeTable("builds an escaped WWW-Authenticate challenge",
		func(scheme, realm string, params []string, expected string) {
			Expect(auth.Challenge(scheme, realm, params...)).To(Equal(expected))
		},
		Entry("plain realm", "Basic", "cloud.example.com", nil, `Basic realm="cloud.example.com"`),
		Entry("extra params", "Basic", "cloud.example.com", []string{`charset="UTF-8"`}, `Basic realm="cloud.example.com", charset="UTF-8"`),
		Entry("quote in realm", "Bearer", `a", foo="b`, nil, `Bearer realm="a\", foo=\"b"`),
		Entry("backslash in realm", "Basic", `a\`, nil, `Basic realm="a\\"`),
	)
})
