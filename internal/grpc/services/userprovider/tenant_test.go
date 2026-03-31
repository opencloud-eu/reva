package userprovider_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	tenantpb "github.com/cs3org/go-cs3apis/cs3/identity/tenant/v1beta1"
	rpcpb "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	"github.com/opencloud-eu/reva/v2/internal/grpc/services/userprovider"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc"
	_ "github.com/opencloud-eu/reva/v2/pkg/tenant/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/user/manager/loader"
)

var _ = Describe("the tenant api service", func() {
	var (
		config   map[string]any
		svc      rgrpc.Service
		provider tenantpb.TenantAPIServer
	)

	BeforeEach(func() {
		var err error
		config = map[string]any{
			"driver": "memory",
			"tenants": []map[string]any{
				{
					"id":         "id1",
					"externalid": "externalid1",
					"name":       "tenant1",
				},
				{
					"id":         "id2",
					"externalid": "externalid2",
					"name":       "tenant2",
				},
			},
		}
		svc, err = userprovider.New(config, nil, nil)
		Expect(err).To(BeNil())
		Expect(svc).ToNot(BeNil())
		provider = svc.(tenantpb.TenantAPIServer)
		Expect(provider).ToNot(BeNil())
	})

	It("returns a not found error for unknown tenants", func() {
		resp, err := provider.GetTenant(context.Background(),
			&tenantpb.GetTenantRequest{
				TenantId: "test",
			},
		)
		Expect(err).To(BeNil())
		Expect(resp).ToNot(BeNil())
		Expect(resp.GetStatus().GetCode()).To(Equal(rpcpb.Code_CODE_NOT_FOUND))
	})
})
