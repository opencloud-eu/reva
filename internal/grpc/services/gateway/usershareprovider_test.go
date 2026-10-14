package gateway

import (
	"context"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("RemoveShare", func() {
	var (
		ctx               context.Context
		collaborationMock *cs3mocks.CollaborationAPIClient
		s                 *svc
		req               = &collaboration.RemoveShareRequest{
			Ref: &collaboration.ShareReference{
				Spec: &collaboration.ShareReference_Id{
					Id: &collaboration.ShareId{OpaqueId: "share-id"},
				},
			},
		}
	)

	BeforeEach(func() {
		ctx = context.Background()
		collaborationMock = &cs3mocks.CollaborationAPIClient{}
		s = newTestSvc(&cs3mocks.StorageRegistryAPIClient{}, &cs3mocks.SpacesAPIClient{}, collaborationMock)
		s.c.CommitShareToStorageGrant = true
	})

	It("returns not found when the share does not exist", func() {
		collaborationMock.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.GetShareResponse{
			Status: status.NewNotFound(ctx, "share not found"),
		}, nil)

		res, err := s.RemoveShare(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.GetStatus().GetCode()).To(Equal(rpc.Code_CODE_NOT_FOUND))
		collaborationMock.AssertNotCalled(GinkgoT(), "RemoveShare", mock.Anything, mock.Anything)
	})

	It("returns internal when the share can't be loaded", func() {
		collaborationMock.On("GetShare", mock.Anything, mock.Anything).Return(&collaboration.GetShareResponse{
			Status: status.NewInternal(ctx, "storage error"),
		}, nil)

		res, err := s.RemoveShare(ctx, req)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.GetStatus().GetCode()).To(Equal(rpc.Code_CODE_INTERNAL))
		collaborationMock.AssertNotCalled(GinkgoT(), "RemoveShare", mock.Anything, mock.Anything)
	})
})
