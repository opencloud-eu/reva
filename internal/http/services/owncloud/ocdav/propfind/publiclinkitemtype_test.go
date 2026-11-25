package propfind_test

import (
	"context"
	"strings"

	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v3/internal/http/services/owncloud/ocdav/config"
	"github.com/opencloud-eu/reva/v3/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v3/internal/http/services/owncloud/ocdav/propfind"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("public-link-item-type", func() {
	request := func() *propfind.XML {
		pf, status, err := propfind.ReadPropfind(strings.NewReader(
			`<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><oc:public-link-item-type/></d:prop></d:propfind>`))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(0))

		return &pf
	}

	answerFor := func(resourceType sprovider.ResourceType, opaque map[string]*typesv1beta1.OpaqueEntry) string {
		info := &sprovider.ResourceInfo{
			Id:   &sprovider.ResourceId{StorageId: "s", SpaceId: "p", OpaqueId: "o"},
			Path: "item",
			Type: resourceType,
		}
		if opaque != nil {
			info.Opaque = &typesv1beta1.Opaque{Map: opaque}
		}

		ctx := context.WithValue(context.Background(), net.CtxKeyBaseURI, "/dav/public-files/token")

		res, err := propfind.MultistatusResponse(ctx, request(), []*sprovider.ResourceInfo{info},
			"http://localhost", "", nil, false, nil, propfind.PreviewMimeTypes(&config.Config{}))
		Expect(err).ToNot(HaveOccurred())

		return string(res)
	}

	linkShare := func() *typesv1beta1.OpaqueEntry {
		return &typesv1beta1.OpaqueEntry{Decoder: "json", Value: []byte(`{}`)}
	}

	It("is folder for a folder link root", func() {
		Expect(answerFor(sprovider.ResourceType_RESOURCE_TYPE_CONTAINER, map[string]*typesv1beta1.OpaqueEntry{
			"link-share": linkShare(),
		})).To(ContainSubstring("<oc:public-link-item-type>folder</oc:public-link-item-type>"))
	})

	It("is file for a file link", func() {
		Expect(answerFor(sprovider.ResourceType_RESOURCE_TYPE_FILE, map[string]*typesv1beta1.OpaqueEntry{
			"link-share": linkShare(),
		})).To(ContainSubstring("<oc:public-link-item-type>file</oc:public-link-item-type>"))
	})

	It("is file for the collection of a file link carrying the item type marker", func() {
		Expect(answerFor(sprovider.ResourceType_RESOURCE_TYPE_CONTAINER, map[string]*typesv1beta1.OpaqueEntry{
			"link-share":            linkShare(),
			"public-link-item-type": {Decoder: "plain", Value: []byte("file")},
		})).To(ContainSubstring("<oc:public-link-item-type>file</oc:public-link-item-type>"))
	})

	It("is not reported without a link share", func() {
		res := answerFor(sprovider.ResourceType_RESOURCE_TYPE_CONTAINER, nil)
		Expect(res).To(ContainSubstring("HTTP/1.1 404 Not Found"))
		Expect(res).ToNot(ContainSubstring("folder"))
	})
})
