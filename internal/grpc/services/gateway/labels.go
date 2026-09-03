package gateway

import (
	"context"
	"fmt"

	labels "github.com/cs3org/go-cs3apis/cs3/labels/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"
)

func (s *svc) AddLabel(ctx context.Context, req *labels.AddLabelRequest) (*labels.AddLabelResponse, error) {
	_, p, ref, err := s.findAndUnwrap(ctx, req.Ref)
	if err != nil {
		return &labels.AddLabelResponse{
			Status: status.NewStatusFromErrType(ctx, fmt.Sprintf("gateway could not find space for ref=%+v", req.Ref), err),
		}, nil
	}
	req.Ref = ref

	lc, err := s.getLabelsProviderClient(p.Address)
	if err != nil {
		return nil, errors.Wrap(err, "gateway: error getting labels client")
	}
	return lc.AddLabel(ctx, req)
}

func (s *svc) RemoveLabel(ctx context.Context, req *labels.RemoveLabelRequest) (*labels.RemoveLabelResponse, error) {
	_, p, ref, err := s.findAndUnwrap(ctx, req.Ref)
	if err != nil {
		return &labels.RemoveLabelResponse{
			Status: status.NewStatusFromErrType(ctx, fmt.Sprintf("gateway could not find space for ref=%+v", req.Ref), err),
		}, nil
	}
	req.Ref = ref

	lc, err := s.getLabelsProviderClient(p.Address)
	if err != nil {
		return nil, errors.Wrap(err, "gateway: error getting labels client")
	}
	return lc.RemoveLabel(ctx, req)
}

func (s *svc) ListLabels(ctx context.Context, req *labels.ListLabelsRequest) (*labels.ListLabelsResponse, error) {
	return nil, gstatus.Errorf(codes.Unimplemented, "ListLabels not yet implemented")
}

func (s *svc) ListResourcesForLabel(ctx context.Context, req *labels.ListResourcesForLabelRequest) (*labels.ListResourcesForLabelResponse, error) {
	return nil, gstatus.Errorf(codes.Unimplemented, "ListResourcesForLabel not yet implemented")
}

func (s *svc) getLabelsProviderClient(address string) (labels.LabelsAPIClient, error) {
	c, err := pool.GetLabelsProviderServiceClient(address)
	if err != nil {
		return nil, err
	}
	return c, nil
}
