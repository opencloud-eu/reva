package datatx

import (
	"context"
	"testing"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/audit"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	events []audit.Event
	ctxs   []context.Context
}

func (r *recorder) Emit(ctx context.Context, ev audit.Event) error {
	r.events = append(r.events, ev)
	r.ctxs = append(r.ctxs, ctx)
	return nil
}

var (
	ref       = &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "space"}, Path: "./docs/report.pdf"}
	executant = &userpb.UserId{OpaqueId: "u-1", TenantId: "t-1"}
	header    = audit.RequestContext{ClientAddress: "203.0.113.7"}.Encode()
)

func TestUploadWithoutHeaderIsNotAudited(t *testing.T) {
	rec := &recorder{}
	EmitUploadAudit(audit.ContextSetEmitter(context.Background(), rec), "", executant, ref, "")
	assert.Empty(t, rec.events, "writes by OpenCloud itself are not uploads")
}

func TestUploadWithUserInContext(t *testing.T) {
	rec := &recorder{}
	ctx := ctxpkg.ContextSetUser(context.Background(), &userpb.User{Id: executant})

	EmitUploadAudit(audit.ContextSetEmitter(ctx, rec), header, executant, ref, "")

	require.Len(t, rec.events, 1)
	ev := rec.events[0]
	assert.Equal(t, audit.FileUpload, ev.Definition)
	assert.Equal(t, "storage$space!space/docs/report.pdf", ev.Target.ID)
	assert.Equal(t, "storage$space", ev.Target.SpaceID)
	assert.Equal(t, "report.pdf", ev.Target.Name)
	assert.Nil(t, ev.Actor, "the emitter takes the user from the context")

	rc, ok := audit.ContextGetRequest(rec.ctxs[0])
	require.True(t, ok)
	assert.Equal(t, "203.0.113.7", rc.ClientAddress)
}

func TestTusUploadUsesTheSessionUser(t *testing.T) {
	rec := &recorder{}

	EmitUploadAudit(audit.ContextSetEmitter(context.Background(), rec), header, executant, &provider.Reference{ResourceId: ref.ResourceId}, "report.pdf")

	require.Len(t, rec.events, 1)
	ev := rec.events[0]
	require.NotNil(t, ev.Actor)
	assert.Equal(t, audit.ActorUser, ev.Actor.Type)
	assert.Equal(t, "u-1", ev.Actor.ID)
	assert.Equal(t, "t-1", ev.Tenant)
	assert.Equal(t, "report.pdf", ev.Target.Name, "the name from the tus metadata")
}

func TestUploadThroughPublicLink(t *testing.T) {
	rec := &recorder{}
	ctx := ctxpkg.ContextSetScopes(context.Background(), map[string]*authpb.Scope{"publicshare:l-1": {}})

	EmitUploadAudit(audit.ContextSetEmitter(ctx, rec), header, executant, ref, "")

	require.Len(t, rec.events, 1)
	require.NotNil(t, rec.events[0].Actor)
	assert.Equal(t, audit.ActorPublicLink, rec.events[0].Actor.Type)
	assert.Equal(t, "l-1", rec.events[0].Actor.ID)
}
