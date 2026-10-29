package datatx

import (
	"context"
	"path"
	"strings"

	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/audit"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
)

// EmitUploadAudit reports a finished upload. Only uploads a client sent are
// reported: the proxy marks them with the audit header, writes by OpenCloud
// itself, e.g. for a copy, don't carry it. executant is the user of the upload
// session, used when the context has no user, as for tus uploads.
func EmitUploadAudit(ctx context.Context, auditHeader string, executant *userv1beta1.UserId, ref *provider.Reference, name string) {
	if auditHeader == "" {
		return
	}
	ctx = audit.ContextSetRequest(ctx, audit.ParseRequestContext(auditHeader))

	id, _ := storagespace.FormatReference(ref)
	ev := audit.Event{
		Definition: audit.FileUpload,
		Target:     audit.Target{ID: id, Path: ref.GetPath(), Name: name},
	}
	if rid := ref.GetResourceId(); rid != nil {
		ev.Target.SpaceID = storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
	}
	if ev.Target.Name == "" && ref.GetPath() != "" && ref.GetPath() != "." {
		ev.Target.Name = path.Base(ref.GetPath())
	}

	switch shareID := PublicShareID(ctx); {
	case shareID != "":
		// through a public link the user in the context is the owner of the link
		ev.Actor = &audit.Actor{Type: audit.ActorPublicLink, ID: shareID}
	case executant != nil:
		if _, ok := ctxpkg.ContextGetUser(ctx); !ok {
			ev.Actor = &audit.Actor{Type: audit.ActorUser, ID: executant.GetOpaqueId()}
			ev.Tenant = executant.GetTenantId()
		}
	}

	if err := audit.ContextGetEmitter(ctx).Emit(ctx, ev); err != nil {
		appctx.GetLogger(ctx).Error().Err(err).Msg("audit event not emitted")
	}
}

// PublicShareID returns the public share the request is authorized by, if any.
func PublicShareID(ctx context.Context) string {
	scopes, ok := ctxpkg.ContextGetScopes(ctx)
	if !ok {
		return ""
	}
	for k := range scopes {
		if id, found := strings.CutPrefix(k, "publicshare:"); found {
			return id
		}
	}
	return ""
}
