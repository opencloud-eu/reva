package propfind_test

import (
	"bytes"
	"context"
	"strings"

	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/propfind"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/rs/zerolog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("per-entry log context", func() {
	// HandleSpacesPropfind builds this shape when the Stat carries no space: a
	// fake space whose RootInfo is the root entry, attached to every entry.
	cyclicInfos := func() []*sprovider.ResourceInfo {
		root := &sprovider.ResourceInfo{
			Id:   &sprovider.ResourceId{StorageId: "s", SpaceId: "p", OpaqueId: "root"},
			Path: "/p",
			Type: sprovider.ResourceType_RESOURCE_TYPE_CONTAINER,
		}
		space := &sprovider.StorageSpace{Id: &sprovider.StorageSpaceId{OpaqueId: "p"}, RootInfo: root}
		root.Space = space
		child := &sprovider.ResourceInfo{
			Id:    &sprovider.ResourceId{StorageId: "s", SpaceId: "p", OpaqueId: "child"},
			Path:  "/p/file",
			Type:  sprovider.ResourceType_RESOURCE_TYPE_FILE,
			Space: space,
			// an unparsable link share makes mdToPropResponse log with its context
			Opaque: &typesv1beta1.Opaque{Map: map[string]*typesv1beta1.OpaqueEntry{
				"link-share": {Decoder: "json", Value: []byte("{")},
			}},
		}
		return []*sprovider.ResourceInfo{root, child}
	}

	It("names the entry without encoding the whole ResourceInfo", func() {
		pf, _, err := propfind.ReadPropfind(strings.NewReader(""))
		Expect(err).ToNot(HaveOccurred())

		var buf bytes.Buffer
		logger := zerolog.New(&buf)
		ctx := appctx.WithLogger(context.Background(), &logger)
		ctx = context.WithValue(ctx, net.CtxKeyBaseURI, "/dav/spaces")

		_, err = propfind.MultistatusResponse(ctx, &pf, cyclicInfos(), "http://localhost", "", nil, false, nil, nil)
		Expect(err).ToNot(HaveOccurred())

		out := buf.String()
		Expect(out).To(ContainSubstring("could not unmarshal link json"))
		Expect(out).To(ContainSubstring(`"md_id":"child"`))
		Expect(out).ToNot(ContainSubstring(`"md":`))
		Expect(out).ToNot(ContainSubstring("cycle"))
	})
})
