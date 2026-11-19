// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocm_test

import (
	"context"
	"errors"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	ocmpb "github.com/cs3org/go-cs3apis/cs3/sharing/ocm/v1beta1"
	memr "github.com/go-micro/plugins/v4/registry/memory"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	mreg "go-micro.dev/v4/registry"
	"google.golang.org/grpc"

	ocm "github.com/opencloud-eu/reva/v3/pkg/ocm/storage/received"
	"github.com/opencloud-eu/reva/v3/pkg/registry"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	cs3mocks "github.com/opencloud-eu/reva/v3/tests/cs3mocks/mocks"
)

func TestListStorageSpacesFollowsGatewayToNewAddress(t *testing.T) {
	const (
		gatewaySVC = "ocmreceived-test-gateway"
		oldAddr    = "192.0.2.1:9142"
		newAddr    = "192.0.2.2:9142"
	)
	ctx := context.Background()

	reg := memr.NewRegistry()
	require.NoError(t, registry.Init(reg))
	node := func(addr string) *mreg.Service {
		return &mreg.Service{Name: gatewaySVC, Nodes: []*mreg.Node{{Id: addr, Address: addr}}}
	}
	require.NoError(t, reg.Register(node(oldAddr)))

	oldGateway := &cs3mocks.GatewayAPIClient{}
	oldGateway.On("ListReceivedOCMShares", mock.Anything, mock.Anything).
		Return(nil, errors.New("gateway is gone"))
	newGateway := &cs3mocks.GatewayAPIClient{}
	newGateway.On("ListReceivedOCMShares", mock.Anything, mock.Anything).
		Return(&ocmpb.ListReceivedOCMSharesResponse{Status: status.NewOK(ctx)}, nil)
	newGateway.On("ListOCMShares", mock.Anything, mock.Anything).
		Return(&ocmpb.ListOCMSharesResponse{Status: status.NewOK(ctx)}, nil)
	gateways := map[string]gateway.GatewayAPIClient{oldAddr: oldGateway, newAddr: newGateway}

	pool.RemoveSelector("GatewaySelector" + gatewaySVC)
	pool.GetSelector("GatewaySelector", gatewaySVC, func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
		return gateways[cc.(*grpc.ClientConn).Target()]
	})

	fs, err := ocm.New(map[string]any{"gatewaysvc": gatewaySVC}, nil, nil)
	require.NoError(t, err)

	// the gateway restarts on another address after the driver was built
	require.NoError(t, reg.Deregister(node(oldAddr)))
	require.NoError(t, reg.Register(node(newAddr)))

	spaces, err := fs.ListStorageSpaces(ctx, nil, false)
	require.NoError(t, err)
	require.Empty(t, spaces)
	newGateway.AssertExpectations(t)
}
