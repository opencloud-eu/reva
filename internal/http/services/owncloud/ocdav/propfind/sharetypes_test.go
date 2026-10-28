package propfind_test

import (
	"context"
	"strings"

	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/propfind"
	"github.com/opencloud-eu/reva/v2/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("share-types", func() {
	request := func() *propfind.XML {
		pf, status, err := propfind.ReadPropfind(strings.NewReader(
			`<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><oc:share-types/></d:prop></d:propfind>`))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(0))

		return &pf
	}

	answerFor := func(shareTypes string) string {
		info := &sprovider.ResourceInfo{
			Id:     &sprovider.ResourceId{StorageId: "s", SpaceId: "p", OpaqueId: "o"},
			Path:   "file",
			Type:   sprovider.ResourceType_RESOURCE_TYPE_FILE,
			Opaque: utils.AppendPlainToOpaque(nil, "share-types", shareTypes),
		}

		ctx := context.WithValue(context.Background(), net.CtxKeyBaseURI, "/dav/files")

		res, err := propfind.MultistatusResponse(ctx, request(), []*sprovider.ResourceInfo{info},
			"http://localhost", "", nil, false, nil, nil)
		Expect(err).ToNot(HaveOccurred())

		return string(res)
	}

	It("renders a guest share", func() {
		Expect(answerFor("4")).To(ContainSubstring("<oc:share-type>4</oc:share-type>"))
	})

	It("renders a regular user and a guest share", func() {
		answer := answerFor("1,4")
		Expect(answer).To(ContainSubstring("<oc:share-type>0</oc:share-type>"))
		Expect(answer).To(ContainSubstring("<oc:share-type>4</oc:share-type>"))
	})
})
