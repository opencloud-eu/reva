package audit

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/nats-io/nats.go"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

type fakePublisher struct {
	mu    sync.Mutex
	msgs  []*nats.Msg
	err   error
	block chan struct{}
}

func (f *fakePublisher) Publish(ctx context.Context, msg *nats.Msg) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *fakePublisher) published() []*nats.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*nats.Msg(nil), f.msgs...)
}

func userContext() context.Context {
	u := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: "u-123",
			TenantId: "t-42",
			Type:     userpb.UserType_USER_TYPE_PRIMARY,
		},
		Username:    "alice",
		DisplayName: "Alice",
	}
	return ctxpkg.ContextSetUser(context.Background(), u)
}

func TestCatalog(t *testing.T) {
	defs := Catalog()
	assert.Len(t, defs, 76)

	name := regexp.MustCompile(`^opencloud\.[a-z_]+(\.[a-z_]+)+$`)
	codes := map[string]string{}
	for _, d := range defs {
		assert.Regexp(t, name, d.Name)
		assert.NotEmpty(t, d.Category, d.Name)
		assert.NotEmpty(t, d.TargetType, d.Name)
		assert.NotZero(t, d.OCSFClass, d.Name)
		assert.NotZero(t, d.OCSFActivity, d.Name)
		code := d.Code(OutcomeSuccess)
		assert.NotContains(t, codes, code, "%s and %s share a code", codes[code], d.Name)
		codes[code] = d.Name
	}

	d, ok := Lookup("opencloud.space.member.add")
	require.True(t, ok)
	assert.Equal(t, SpaceMemberAdd, d)
}

func TestCodes(t *testing.T) {
	assert.Equal(t, "A001S", AuthLogin.Code(OutcomeSuccess))
	assert.Equal(t, "A001F", AuthLogin.Code(OutcomeFailure))
	assert.Equal(t, "A001D", AuthLogin.Code(OutcomeDenied))
}

func TestRequestContextRoundTrip(t *testing.T) {
	rc := RequestContext{
		ClientAddress:  "203.0.113.7",
		SessionID:      "s-1",
		AuthMethod:     "oidc",
		ClientID:       "web",
		ImpersonatedBy: "admin",
	}
	assert.Equal(t, rc, ParseRequestContext(rc.Encode()))
	assert.Equal(t, RequestContext{}, ParseRequestContext("%zz"))
	assert.Equal(t, "", RequestContext{}.Encode())
}

func TestRequestContextPropagation(t *testing.T) {
	rc := RequestContext{ClientAddress: "203.0.113.7", AuthMethod: "app_token"}

	ctx := ContextSetRequest(context.Background(), rc)
	got, ok := ContextGetRequest(ctx)
	require.True(t, ok)
	assert.Equal(t, rc, got)

	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{rc.Encode()}, md.Get(Header))

	// the next service only has the incoming metadata
	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs(Header, rc.Encode()))
	got, ok = ContextGetRequest(incoming)
	require.True(t, ok)
	assert.Equal(t, rc, got)

	assert.Equal(t, context.Background(), ContextSetRequest(context.Background(), RequestContext{}))
}

func TestRecord(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{Service: Service{Name: "graph", Version: "5.1.0"}, Mode: ModeStrict})
	e.now = func() time.Time { return time.Date(2026, 10, 28, 9, 14, 3, 512_000_000, time.FixedZone("CET", 3600)) }

	ctx := userContext()
	ctx = ContextSetRequest(ctx, RequestContext{ClientAddress: "203.0.113.7", SessionID: "s-1", AuthMethod: "oidc", ClientID: "web"})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(ctxpkg.UserAgentHeader, "Mozilla/5.0"))
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	ctx = trace.ContextWithSpanContext(ctx, sc)

	rec, err := e.Record(ctx, Event{
		Definition: FileDownload,
		Target:     Target{ID: "space$node!item", SpaceID: "space$node", Name: "report.pdf", Path: "/report.pdf"},
	})
	require.NoError(t, err)

	assert.Len(t, rec.EventID, 36)
	assert.Equal(t, "opencloud.file.download", rec.EventName)
	assert.Equal(t, "F002S", rec.EventCode)
	assert.Equal(t, "1.0", rec.SchemaVersion)
	assert.Equal(t, "2026-10-28T08:14:03.512Z", rec.Time)
	assert.Equal(t, OutcomeSuccess, rec.Outcome)
	assert.Equal(t, SeverityInfo, rec.Severity)
	assert.Equal(t, "t-42", rec.TenantID)
	assert.Equal(t, ActorUser, rec.ActorType)
	assert.Equal(t, "u-123", rec.ActorID)
	assert.Equal(t, "Alice", rec.ActorName)
	assert.Equal(t, "203.0.113.7", rec.ClientAddress)
	assert.Equal(t, "s-1", rec.SessionID)
	assert.Equal(t, "oidc", rec.AuthMethod)
	assert.Equal(t, "web", rec.AuthClientID)
	assert.Equal(t, "Mozilla/5.0", rec.UserAgent)
	assert.Equal(t, "file", rec.TargetType)
	assert.Equal(t, "space$node!item", rec.TargetID)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", rec.TraceID)
	assert.Equal(t, "00f067aa0ba902b7", rec.SpanID)
	assert.Equal(t, "graph", rec.ServiceName)
	assert.Equal(t, 6006, rec.OCSFClassUID)
	assert.Equal(t, 2, rec.OCSFActivityID)
}

func TestRecordOutcomes(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{Service: Service{Name: "proxy"}, Mode: ModeStrict})

	rec, err := e.Record(context.Background(), Event{
		Definition: PublicLinkAccess,
		Outcome:    OutcomeDenied,
		Reason:     "invalid_password",
		Actor:      &Actor{Type: ActorPublicLink, ID: "share-1"},
		Target:     Target{ID: "share-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "S009D", rec.EventCode)
	assert.Equal(t, SeverityWarning, rec.Severity)
	assert.Equal(t, 6004, rec.OCSFClassUID)
	assert.Equal(t, 2, rec.OCSFActivityID, "a denied access is Access Deny")
	assert.Equal(t, ActorPublicLink, rec.ActorType)
	assert.Equal(t, "", rec.TenantID)

	rec, err = e.Record(context.Background(), Event{Definition: FileMalwareDetect, Actor: &Actor{Type: ActorSystem, ID: "antivirus"}, Target: Target{ID: "f"}})
	require.NoError(t, err)
	assert.Equal(t, SeverityAlert, rec.Severity, "an alert stays an alert")

	rec, err = e.Record(context.Background(), Event{Definition: AuthLogin, Target: Target{ID: "unknown"}})
	require.NoError(t, err)
	assert.Equal(t, ActorAnonymous, rec.ActorType)
}

func TestRecordValidation(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{Mode: ModeStrict})

	_, err := e.Record(context.Background(), Event{Target: Target{ID: "x"}})
	assert.ErrorIs(t, err, ErrInvalidEvent)

	_, err = e.Record(context.Background(), Event{Definition: FileDelete})
	assert.ErrorIs(t, err, ErrInvalidEvent, "target id is required")

	_, err = e.Record(context.Background(), Event{Definition: AuthLogin, Outcome: OutcomeFailure, Target: Target{ID: "u"}})
	assert.ErrorIs(t, err, ErrInvalidEvent, "failures need a reason")
}

func TestEmitStrict(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, Options{Service: Service{Name: "graph"}, Mode: ModeStrict})

	require.NoError(t, e.Emit(userContext(), Event{Definition: UserCreate, Target: Target{ID: "u-2"}}))
	msgs := pub.published()
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, "audit.t-42.identity", msg.Subject)
	var rec Record
	require.NoError(t, json.Unmarshal(msg.Data, &rec))
	assert.Equal(t, rec.EventID, msg.Header.Get(nats.MsgIdHdr))
	assert.Equal(t, rec.EventID, msg.Header.Get("ce-id"))
	assert.Equal(t, "opencloud.user.create", msg.Header.Get("ce-type"))
	assert.Equal(t, "/opencloud/graph", msg.Header.Get("ce-source"))
	assert.Equal(t, "t-42", msg.Header.Get("ce-partitionkey"))

	pub.err = errors.New("nats down")
	err := e.Emit(userContext(), Event{Definition: UserCreate, Target: Target{ID: "u-3"}})
	assert.ErrorIs(t, err, ErrDropped)
}

func TestEmitBestEffort(t *testing.T) {
	pub := &fakePublisher{block: make(chan struct{})}
	e := NewEmitter(pub, Options{Service: Service{Name: "frontend"}, BufferSize: 1})

	ev := Event{Definition: FileUpload, Target: Target{ID: "f"}}
	// the worker takes the first event and blocks in Publish, the second fills
	// the buffer, the third is dropped; none of them blocks or fails the caller
	for i := 0; i < 3; i++ {
		assert.NoError(t, e.Emit(context.Background(), ev))
	}
	close(pub.block)
	e.Close()

	assert.GreaterOrEqual(t, len(pub.published()), 1)
	assert.LessOrEqual(t, len(pub.published()), 2)
}

func TestSubject(t *testing.T) {
	assert.Equal(t, "audit._.files", Subject("", "files"))
	assert.Equal(t, "audit.a_b_c_.files", Subject("a.b c*", "files"))
}
