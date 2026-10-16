package propfind_test

import (
	"context"
	"strings"

	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/config"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/propfind"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("has-preview", func() {
	request := func() *propfind.XML {
		pf, status, err := propfind.ReadPropfind(strings.NewReader(
			`<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><oc:has-preview/></d:prop></d:propfind>`))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(0))

		return &pf
	}

	answerFor := func(mimeType string, previewMimeTypes []string) string {
		info := &sprovider.ResourceInfo{
			Id:       &sprovider.ResourceId{StorageId: "s", SpaceId: "p", OpaqueId: "o"},
			Path:     "file",
			Type:     sprovider.ResourceType_RESOURCE_TYPE_FILE,
			MimeType: mimeType,
		}

		ctx := context.WithValue(context.Background(), net.CtxKeyBaseURI, "/dav/files")

		res, err := propfind.MultistatusResponse(ctx, request(), []*sprovider.ResourceInfo{info},
			"http://localhost", "", nil, false, nil, propfind.PreviewMimeTypes(&config.Config{PreviewMimeTypes: previewMimeTypes}))
		Expect(err).ToNot(HaveOccurred())

		return string(res)
	}

	It("is 1 for a configured type", func() {
		Expect(answerFor("image/heic", []string{"image/png", "image/heic"})).To(ContainSubstring("<oc:has-preview>1</oc:has-preview>"))
	})

	It("is 0 for a type nobody configured", func() {
		Expect(answerFor("application/json", []string{"image/png"})).To(ContainSubstring("<oc:has-preview>0</oc:has-preview>"))
	})

	It("is 0 for every type when none is configured", func() {
		Expect(answerFor("image/png", nil)).To(ContainSubstring("<oc:has-preview>0</oc:has-preview>"))
	})
})
