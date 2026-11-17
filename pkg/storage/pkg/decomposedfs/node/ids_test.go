// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ids", func() {
	DescribeTable("IsValidID",
		func(id string, valid bool) {
			Expect(node.IsValidID(id)).To(Equal(valid))
		},
		Entry("uuid", "a5b1c4e2-7f3d-4b8a-9c6e-1d2f3a4b5c6d", true),
		Entry("revision key", "a5b1c4e2-7f3d.REV.2024-01-01T00:00:00.123456789Z", true),
		Entry("single dot", "a.b", true),
		Entry("separator", "abc/def", false),
		Entry("parent", "../abc", false),
		Entry("dots inside a pathify chunk", "ab..cdef", false),
		Entry("revision traversal", "abc.REV.x/../../etc", false),
		Entry("dots at the end", "abcdefgh..", false),
		Entry("NUL", "abc\x00def", false),
	)
})
