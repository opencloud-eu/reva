package datatx

import (
	"context"
	"errors"
	"testing"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/audit"
	ctxpkg "github.com/opencloud-eu/reva/v3/pkg/ctx"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
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
	node      = &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "node-1"}
	executant = &userpb.User{Id: &userpb.UserId{OpaqueId: "u-1", TenantId: "t-1"}, Username: "alan", DisplayName: "Alan Turing"}
	header    = audit.RequestContext{ClientAddress: "203.0.113.7", UserAgent: "last-patch"}.Encode()
)

func TestUploadWithoutHeaderIsNotAudited(t *testing.T) {
	rec := &recorder{}
	EmitUploadAudit(context.Background(), rec, UploadAudit{Executant: executant, Ref: ref, ID: node})
	assert.Empty(t, rec.events, "writes by OpenCloud itself are not uploads")
}

func TestUploadWithUserInContext(t *testing.T) {
	rec := &recorder{}
	ctx := ctxpkg.ContextSetUser(context.Background(), &userpb.User{Id: &userpb.UserId{OpaqueId: "u-2"}})

	EmitUploadAudit(ctx, rec, UploadAudit{Header: header, Executant: executant, Ref: ref, ID: node})

	require.Len(t, rec.events, 1)
	ev := rec.events[0]
	assert.Equal(t, audit.FileUpload, ev.Definition)
	assert.Equal(t, "storage$space!node-1", ev.Target.ID, "the node, not the path")
	assert.Equal(t, "./docs/report.pdf", ev.Target.Path)
	assert.Equal(t, "storage$space", ev.Target.SpaceID)
	assert.Equal(t, "report.pdf", ev.Target.Name)
	assert.Nil(t, ev.Actor, "the emitter takes the user from the context")
	u, _ := ctxpkg.ContextGetUser(rec.ctxs[0])
	assert.Equal(t, "u-2", u.GetId().GetOpaqueId(), "the user of the request wins over the session")

	rc, ok := audit.ContextGetRequest(rec.ctxs[0])
	require.True(t, ok)
	assert.Equal(t, "203.0.113.7", rc.ClientAddress)
}

func TestTusUploadUsesTheSession(t *testing.T) {
	rec := &recorder{}
	initial := audit.RequestContext{ClientAddress: "198.51.100.1", AuthMethod: "oidc", SessionID: "sid-1", UserAgent: "creating-post"}

	EmitUploadAudit(context.Background(), rec, UploadAudit{
		Header: header, Initial: initial, Executant: executant, Ref: ref, ID: node, Name: "report.pdf",
	})

	require.Len(t, rec.events, 1)
	assert.Nil(t, rec.events[0].Actor, "the emitter takes the session user from the context")
	u, ok := ctxpkg.ContextGetUser(rec.ctxs[0])
	require.True(t, ok)
	assert.Equal(t, "Alan Turing", u.GetDisplayName(), "with its name, not only the ID")

	rc, _ := audit.ContextGetRequest(rec.ctxs[0])
	assert.Equal(t, "oidc", rc.AuthMethod, "from the request that created the upload")
	assert.Equal(t, "sid-1", rc.SessionID)
	assert.Equal(t, "203.0.113.7", rc.ClientAddress, "from the request that finished it")
	assert.Equal(t, "last-patch", rc.UserAgent)
}

func TestUploadThroughPublicLink(t *testing.T) {
	rec := &recorder{}
	ctx := ctxpkg.ContextSetScopes(context.Background(), map[string]*authpb.Scope{"publicshare:l-1": {}})

	EmitUploadAudit(ctx, rec, UploadAudit{Header: header, Executant: executant, Ref: ref})

	require.Len(t, rec.events, 1)
	assert.Equal(t, "storage$space!space/docs/report.pdf", rec.events[0].Target.ID, "without a node ID the reference is all there is")
	require.NotNil(t, rec.events[0].Actor)
	assert.Equal(t, audit.ActorPublicLink, rec.events[0].Actor.Type)
	assert.Equal(t, "l-1", rec.events[0].Actor.ID)
}

func TestTusUploadThroughPublicLink(t *testing.T) {
	rec := &recorder{}

	// the data requests have no scopes, the session knows the link
	EmitUploadAudit(context.Background(), rec, UploadAudit{
		Header: header, Executant: executant, PublicShareID: "l-1", Ref: ref, ID: node,
	})

	require.Len(t, rec.events, 1)
	require.NotNil(t, rec.events[0].Actor, "not the owner of the link")
	assert.Equal(t, audit.ActorPublicLink, rec.events[0].Actor.Type)
	assert.Equal(t, "l-1", rec.events[0].Actor.ID)
}

// pathFS answers a stat by ID with the path in the space, as the drivers do.
type pathFS struct {
	storage.FS
	path string
	user string
}

func (f *pathFS) GetMD(ctx context.Context, ref *provider.Reference, _ []string, _ []string) (*provider.ResourceInfo, error) {
	if u, ok := ctxpkg.ContextGetUser(ctx); ok {
		f.user = u.GetId().GetOpaqueId()
	}
	if ref.GetPath() != "" {
		return nil, errors.New("only by ID")
	}
	return &provider.ResourceInfo{Id: ref.GetResourceId(), Path: f.path}, nil
}

func TestUploadIntoAShareHasThePathInTheSpace(t *testing.T) {
	rec := &recorder{}
	fs := &pathFS{path: "/projects/shared/docs/report.pdf"}
	// the session only knows the path below what the recipient may see
	inShare := &provider.Reference{ResourceId: ref.GetResourceId(), Path: "./shared/docs/report.pdf"}

	EmitUploadAudit(context.Background(), rec, UploadAudit{Header: header, Executant: executant, Ref: inShare, ID: node, FS: fs})

	require.Len(t, rec.events, 1)
	assert.Equal(t, "./projects/shared/docs/report.pdf", rec.events[0].Target.Path)
	assert.Equal(t, executant.GetId().GetOpaqueId(), fs.user, "looked up as the uploader")
}

func TestUploadWithAuditOffLooksNothingUp(t *testing.T) {
	fs := &pathFS{path: "/projects/shared/docs/report.pdf"}

	// the proxy sent the header, but the data provider has audit off
	EmitUploadAudit(context.Background(), audit.Discard{}, UploadAudit{Header: header, Executant: executant, Ref: ref, ID: node, FS: fs})

	assert.Empty(t, fs.user, "no path lookup")
}
