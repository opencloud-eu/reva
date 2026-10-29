package audit

import (
	"context"
	"net/url"

	"google.golang.org/grpc/metadata"
)

// Header carries the request context between services, as HTTP header and as
// gRPC metadata. Everything else the audit record needs (user, tenant, user
// agent, initiator, trace) already travels in its own header.
const Header = "x-audit-context"

// Short keys keep the header small, it is sent with every call.
const (
	keyClientAddress  = "ip"
	keySessionID      = "sid"
	keyAuthMethod     = "am"
	keyClientID       = "cid"
	keyImpersonatedBy = "imp"
)

// RequestContext is what the proxy knows about a request and the audit
// record needs, but no other header carries.
type RequestContext struct {
	// ClientAddress is the real client address, resolved behind trusted proxies.
	ClientAddress string
	// SessionID identifies the login session.
	SessionID string
	// AuthMethod is how the actor authenticated, e.g. oidc, app_token, public_link, basic.
	AuthMethod string
	// ClientID is the OIDC client.
	ClientID string
	// ImpersonatedBy is set when someone acts on behalf of the user.
	ImpersonatedBy string
}

// IsZero reports whether no field is set.
func (rc RequestContext) IsZero() bool {
	return rc == RequestContext{}
}

// Encode returns the header value.
func (rc RequestContext) Encode() string {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set(keyClientAddress, rc.ClientAddress)
	set(keySessionID, rc.SessionID)
	set(keyAuthMethod, rc.AuthMethod)
	set(keyClientID, rc.ClientID)
	set(keyImpersonatedBy, rc.ImpersonatedBy)
	return v.Encode()
}

// ParseRequestContext reads a header value. Unknown keys are ignored, a
// broken value returns an empty context.
func ParseRequestContext(s string) RequestContext {
	v, err := url.ParseQuery(s)
	if err != nil {
		return RequestContext{}
	}
	return RequestContext{
		ClientAddress:  v.Get(keyClientAddress),
		SessionID:      v.Get(keySessionID),
		AuthMethod:     v.Get(keyAuthMethod),
		ClientID:       v.Get(keyClientID),
		ImpersonatedBy: v.Get(keyImpersonatedBy),
	}
}

type requestContextKey struct{}

// ContextSetRequest stores the request context in ctx and adds it to the
// outgoing gRPC metadata, so it reaches the next service.
func ContextSetRequest(ctx context.Context, rc RequestContext) context.Context {
	if rc.IsZero() {
		return ctx
	}
	ctx = context.WithValue(ctx, requestContextKey{}, rc)
	return metadata.AppendToOutgoingContext(ctx, Header, rc.Encode())
}

// ContextGetRequest returns the request context from ctx, falling back to the
// incoming gRPC metadata.
func ContextGetRequest(ctx context.Context) (RequestContext, bool) {
	if rc, ok := ctx.Value(requestContextKey{}).(RequestContext); ok {
		return rc, true
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if lst := md.Get(Header); len(lst) > 0 {
			rc := ParseRequestContext(lst[0])
			return rc, !rc.IsZero()
		}
	}
	return RequestContext{}, false
}

type emitterKey struct{}

// ContextSetEmitter stores the emitter in ctx, for code that is reached
// through layers which don't pass it on, like the data transfer handlers.
func ContextSetEmitter(ctx context.Context, e Emitter) context.Context {
	return context.WithValue(ctx, emitterKey{}, e)
}

// ContextGetEmitter returns the emitter from ctx, an emitter that drops
// everything when there is none.
func ContextGetEmitter(ctx context.Context) Emitter {
	if e, ok := ctx.Value(emitterKey{}).(Emitter); ok && e != nil {
		return e
	}
	return Discard{}
}
