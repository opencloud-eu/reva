package eventsmiddleware

import (
	"context"
	"fmt"
	"testing"
	"time"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func TestPublicLinkAccess(t *testing.T) {
	share := &link.PublicShare{Id: &link.PublicShareId{OpaqueId: "l-1"}, ResourceId: rid}
	withPassword := &link.PublicShareAuthentication{Spec: &link.PublicShareAuthentication_Password{Password: "secret"}}
	withSignature := &link.PublicShareAuthentication{Spec: &link.PublicShareAuthentication_Signature{Signature: &link.ShareSignature{Signature: "sig"}}}

	for _, tc := range []struct {
		name     string
		req      *link.GetPublicShareByTokenRequest
		res      *link.GetPublicShareByTokenResponse
		outcome  audit.Outcome
		reason   string
		targetID string
	}{
		{
			name: "access", outcome: audit.OutcomeSuccess, targetID: "l-1",
			req: &link.GetPublicShareByTokenRequest{Token: "t"},
			res: &link.GetPublicShareByTokenResponse{Status: ok, Share: share},
		},
		{
			name: "wrong password", outcome: audit.OutcomeDenied, reason: "invalid_password", targetID: "token:" + tokenHash("t"),
			req: &link.GetPublicShareByTokenRequest{Token: "t", Authentication: withPassword},
			res: &link.GetPublicShareByTokenResponse{Status: denied},
		},
		{
			name: "wrong signature", outcome: audit.OutcomeDenied, reason: "invalid_signature", targetID: "token:" + tokenHash("t"),
			req: &link.GetPublicShareByTokenRequest{Token: "t", Authentication: withSignature},
			res: &link.GetPublicShareByTokenResponse{Status: denied},
		},
		{
			name: "unknown token", outcome: audit.OutcomeFailure, reason: "not_found", targetID: "token:" + tokenHash("guessed"),
			req: &link.GetPublicShareByTokenRequest{Token: "guessed"},
			res: &link.GetPublicShareByTokenResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, mapped := auditEvent(tc.req, tc.res, nil)
			assert.True(t, mapped)
			assert.Equal(t, audit.PublicLinkAccess, ev.Definition)
			assert.Equal(t, tc.outcome, ev.Outcome)
			assert.Equal(t, tc.reason, ev.Reason)
			assert.Equal(t, tc.targetID, ev.Target.ID)
			assert.Equal(t, audit.ActorPublicLink, ev.Actor.Type)
			assert.NotEqual(t, "token:"+tc.req.GetToken(), ev.Target.ID, "the token itself is never in the record")
		})
	}
	assert.NotEqual(t, "t", tokenHash("t"))
	assert.Len(t, tokenHash("t"), 16)
}

func TestOnceWithin(t *testing.T) {
	o := newOnceWithin(30*time.Minute, 100)
	now := time.Now()

	assert.True(t, o.first("l-1|203.0.113.7", now))
	assert.False(t, o.first("l-1|203.0.113.7", now.Add(10*time.Minute)), "the same access")
	assert.True(t, o.first("l-1|198.51.100.1", now.Add(10*time.Minute)), "another client")
	assert.True(t, o.first("l-1|203.0.113.7", now.Add(41*time.Minute)), "a new access after the window")
}

func TestOnceWithinIsBounded(t *testing.T) {
	o := newOnceWithin(10*time.Second, 3)
	now := time.Now()

	for i := 0; i < 10; i++ {
		assert.True(t, o.first(fmt.Sprintf("attempt-%d", i), now))
	}
	assert.Len(t, o.seen, 3, "a full map stops growing")
	assert.True(t, o.first("attempt-9", now), "and reports again what it couldn't remember")

	assert.True(t, o.first("later", now.Add(11*time.Second)))
	assert.Len(t, o.seen, 1, "expired keys are removed once per window")
}

func TestFirstLinkAccessOnlyDropsRepeatedSuccesses(t *testing.T) {
	o := newLinkAccesses()
	ok := audit.Event{Definition: audit.PublicLinkAccess, Outcome: audit.OutcomeSuccess, Target: audit.Target{ID: "l-1"}}
	failed := audit.Event{Definition: audit.PublicLinkAccess, Outcome: audit.OutcomeDenied, Target: audit.Target{ID: "l-1"}}
	other := audit.Event{Definition: audit.FileLock, Outcome: audit.OutcomeSuccess, Target: audit.Target{ID: "f"}}

	client := audit.ContextSetRequest(context.Background(), audit.RequestContext{ClientAddress: "203.0.113.7"})
	assert.True(t, firstLinkAccess(client, o, ok))
	assert.False(t, firstLinkAccess(client, o, ok))
	assert.False(t, firstLinkAccess(context.Background(), o, audit.Event{Definition: audit.PublicLinkAccess, Outcome: audit.OutcomeSuccess, Target: audit.Target{ID: "l-2"}}), "a service resolving the link is no access")
	assert.True(t, firstLinkAccess(context.Background(), o, failed), "a failure without trace counts")
	assert.True(t, firstLinkAccess(context.Background(), o, failed))

	inTrace := func(id byte) context.Context {
		return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{id}, SpanID: trace.SpanID{1},
		}))
	}
	assert.True(t, firstLinkAccess(inTrace(1), o, failed), "the proxy checks the password")
	assert.False(t, firstLinkAccess(inTrace(1), o, failed), "ocdav checks it again in the same request")
	assert.True(t, firstLinkAccess(inTrace(2), o, failed), "the next attempt counts")
	assert.True(t, firstLinkAccess(context.Background(), o, other))
	assert.True(t, firstLinkAccess(context.Background(), o, other))
}
