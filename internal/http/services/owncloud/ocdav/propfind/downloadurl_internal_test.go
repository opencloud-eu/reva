// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package propfind

import (
	"context"

	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

// Regression test for https://github.com/opencloud-eu/opencloud/issues/2852:
// downloadURL must percent-encode the path so a filename with a literal "%"
// or "#" round-trips. (Rationale at the call site in propfind.go.)
var _ = Describe("downloadURL", func() {
	// net.EncodePath runs before the branch switch and is shared by all
	// branches. The public, non-password-protected branch is the deterministic
	// observation point: it returns publicURL+baseURI+encodedPath without a
	// signer. This is not a claim that public links are affected; the
	// user-facing impact is on the signed personal/space URLs.
	const (
		publicURL = "https://cloud.example"
		baseURI   = "/remote.php/dav/public-files/tok"
	)

	DescribeTable("percent-encodes reserved characters in the name",
		func(path, wantTail string) {
			got := downloadURL(context.Background(), zerolog.Nop(), true, path, &link.PublicShare{}, publicURL, baseURI, nil)
			Expect(got).To(Equal(publicURL + baseURI + wantTail))
		},
		Entry("percent followed by hex digits in name", "/dir/firstword%20secondword.pdf", "/dir/firstword%2520secondword.pdf"),
		Entry("percent not followed by hex in name", "/dir/50%off.pdf", "/dir/50%25off.pdf"),
		Entry("hash in name", "/dir/a#b.pdf", "/dir/a%23b.pdf"),
		Entry("plain space in name", "/dir/foo bar.pdf", "/dir/foo%20bar.pdf"),
	)
})
