package auditctx

import (
	"context"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryPassesRequestContextOn(t *testing.T) {
	rc := audit.RequestContext{ClientAddress: "203.0.113.7", SessionID: "s-1"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(audit.Header, rc.Encode()))

	var got context.Context
	_, err := NewUnary()(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		got = ctx
		return nil, nil
	})
	require.NoError(t, err)

	stored, ok := audit.ContextGetRequest(got)
	require.True(t, ok)
	assert.Equal(t, rc, stored)

	out, ok := metadata.FromOutgoingContext(got)
	require.True(t, ok)
	assert.Equal(t, []string{rc.Encode()}, out.Get(audit.Header))
}

func TestUnaryWithoutHeader(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-user-agent", "test"))

	var got context.Context
	_, err := NewUnary()(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req interface{}) (interface{}, error) {
		got = ctx
		return nil, nil
	})
	require.NoError(t, err)

	_, ok := audit.ContextGetRequest(got)
	assert.False(t, ok)
	_, ok = metadata.FromOutgoingContext(got)
	assert.False(t, ok)
}
