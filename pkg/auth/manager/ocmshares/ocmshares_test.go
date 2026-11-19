// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocmshares_test

import (
	"context"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	ocm "github.com/cs3org/go-cs3apis/cs3/sharing/ocm/v1beta1"
	memr "github.com/go-micro/plugins/v4/registry/memory"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	mreg "go-micro.dev/v4/registry"
	"google.golang.org/grpc"

	"github.com/opencloud-eu/reva/v3/pkg/auth/manager/ocmshares"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/registry"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	cs3mocks "github.com/opencloud-eu/reva/v3/tests/cs3mocks/mocks"
)

func TestAuthenticateFollowsGatewayToNewAddress(t *testing.T) {
	const (
		gatewaySVC = "ocmshares-test-gateway"
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
	oldGateway.On("GetOCMShareByToken", mock.Anything, mock.Anything).
		Return(&ocm.GetOCMShareByTokenResponse{Status: status.NewInternal(ctx, "gateway is gone")}, nil)
	newGateway := &cs3mocks.GatewayAPIClient{}
	newGateway.On("GetOCMShareByToken", mock.Anything, mock.Anything).
		Return(&ocm.GetOCMShareByTokenResponse{Status: status.NewNotFound(ctx, "no such share")}, nil)
	gateways := map[string]gateway.GatewayAPIClient{oldAddr: oldGateway, newAddr: newGateway}

	pool.RemoveSelector("GatewaySelector" + gatewaySVC)
	pool.GetSelector("GatewaySelector", gatewaySVC, func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
		return gateways[cc.(*grpc.ClientConn).Target()]
	})

	mgr, err := ocmshares.New(map[string]any{"gatewaysvc": gatewaySVC}, nil)
	require.NoError(t, err)

	// the gateway restarts on another address after the manager was built
	require.NoError(t, reg.Deregister(node(oldAddr)))
	require.NoError(t, reg.Register(node(newAddr)))

	_, _, err = mgr.Authenticate(ctx, "share-id", "secret")
	require.ErrorAs(t, err, new(errtypes.NotFound))
	newGateway.AssertExpectations(t)
}
