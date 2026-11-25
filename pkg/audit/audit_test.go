package audit

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/nats-io/nats.go"
	ctxpkg "github.com/opencloud-eu/reva/v3/pkg/ctx"
	"github.com/opencloud-eu/reva/v3/pkg/events/raw"
	"github.com/prometheus/client_golang/prometheus"
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
		ClientAddress: "203.0.113.7",
		SessionID:     "s-1",
		AuthMethod:    "oidc",
		ClientID:      "web",
		UserAgent:     "Mozilla/5.0 (Macintosh) Safari/605.1.15",
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

func TestRecordUserAgent(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{})
	ev := Event{Definition: ShareUpdate, Target: Target{ID: "share-1"}}
	record := func(ctx context.Context) *Record {
		rec, err := e.Record(ctx, ev)
		require.NoError(t, err)
		return rec
	}

	// a service called by graph only sees the gRPC user-agent of graph
	grpcOnly := metadata.NewIncomingContext(userContext(), metadata.Pairs("user-agent", "grpc-go/1.84.0"))
	assert.Empty(t, record(grpcOnly).UserAgent)

	fromProxy := ContextSetRequest(grpcOnly, RequestContext{UserAgent: "Safari"})
	assert.Equal(t, "Safari", record(fromProxy).UserAgent)

	fromReva := metadata.NewIncomingContext(userContext(), metadata.Pairs(ctxpkg.UserAgentHeader, "Mozilla/5.0"))
	assert.Equal(t, "Mozilla/5.0", record(fromReva).UserAgent)
}

func TestRecord(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{Service: Service{Name: "graph", Version: "5.1.0"}})
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
	e := NewEmitter(&fakePublisher{}, Options{Service: Service{Name: "proxy"}})

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

func TestPublicLinkIsTheActor(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{Service: Service{Name: "storage-users"}})
	owner := &userpb.User{Id: &userpb.UserId{OpaqueId: "owner", TenantId: "t-1"}, Username: "owner"}
	ctx := ctxpkg.ContextSetUser(context.Background(), owner)

	rec, err := e.Record(ctxpkg.ContextSetScopes(ctx, map[string]*authpb.Scope{"publicshare:share-1": {}}), Event{Definition: FolderCreate, Target: Target{ID: "f"}})
	require.NoError(t, err)
	assert.Equal(t, ActorPublicLink, rec.ActorType, "a link acts with the user of its owner")
	assert.Equal(t, "share-1", rec.ActorID)
	assert.Empty(t, rec.ActorName)
	assert.Equal(t, "t-1", rec.TenantID)

	rec, err = e.Record(ctx, Event{Definition: FolderCreate, Target: Target{ID: "f"}})
	require.NoError(t, err)
	assert.Equal(t, ActorUser, rec.ActorType)
	assert.Equal(t, "owner", rec.ActorID)
}

func TestRecordValidation(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, Options{})

	_, err := e.Record(context.Background(), Event{Target: Target{ID: "x"}})
	assert.ErrorIs(t, err, ErrInvalidEvent)

	_, err = e.Record(context.Background(), Event{Definition: FileDelete})
	assert.ErrorIs(t, err, ErrInvalidEvent, "target id is required")

	_, err = e.Record(context.Background(), Event{Definition: AuthLogin, Outcome: OutcomeFailure, Target: Target{ID: "u"}})
	assert.ErrorIs(t, err, ErrInvalidEvent, "failures need a reason")
}

func TestEmit(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, Options{Service: Service{Name: "graph"}})

	require.NoError(t, e.Emit(userContext(), Event{Definition: UserCreate, Target: Target{ID: "u-2"}}))
	e.Close()
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
}

// droppedCount reads the dropped counter, the metrics are global, so tests
// compare it before and after.
func droppedCount(t *testing.T, service, event, reason string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	want := map[string]string{"service": service, "event": event, "reason": reason}
	for _, mf := range mfs {
		if mf.GetName() != "opencloud_audit_events_dropped_total" {
			continue
		}
	metrics:
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if want[l.GetName()] != l.GetValue() {
					continue metrics
				}
			}
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func TestEmitPublishErrorDoesNotFailTheCaller(t *testing.T) {
	pub := &fakePublisher{err: errors.New("nats down")}
	e := NewEmitter(pub, Options{Service: Service{Name: "graph"}})
	before := droppedCount(t, "graph", UserCreate.Name, "publish")

	assert.NoError(t, e.Emit(userContext(), Event{Definition: UserCreate, Target: Target{ID: "u-3"}}))
	e.Close()
	assert.Empty(t, pub.published())
	assert.Equal(t, before+1, droppedCount(t, "graph", UserCreate.Name, "publish"))
}

func TestEmitBestEffort(t *testing.T) {
	pub := &fakePublisher{block: make(chan struct{})}
	e := NewEmitter(pub, Options{Service: Service{Name: "frontend"}, BufferSize: 1})

	ev := Event{Definition: FileUpload, Target: Target{ID: "f"}}
	before := droppedCount(t, "frontend", FileUpload.Name, "buffer_full")
	// the worker takes the first event and blocks in Publish, the second fills
	// the buffer, the third is dropped; none of them blocks or fails the caller
	for i := 0; i < 3; i++ {
		assert.NoError(t, e.Emit(context.Background(), ev))
	}
	close(pub.block)
	e.Close()

	published := len(pub.published())
	assert.GreaterOrEqual(t, published, 1)
	assert.LessOrEqual(t, published, 2)
	assert.Equal(t, before+float64(3-published), droppedCount(t, "frontend", FileUpload.Name, "buffer_full"))
}

func TestEmitAfterClose(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, Options{Service: Service{Name: "dataprovider"}})
	ev := Event{Definition: FileUpload, Target: Target{ID: "f"}}

	require.NoError(t, e.Emit(context.Background(), ev))
	e.Close()
	assert.Len(t, pub.published(), 1, "Close flushes the buffer")

	before := droppedCount(t, "dataprovider", FileUpload.Name, "closed")
	assert.NotPanics(t, func() {
		assert.NoError(t, e.Emit(context.Background(), ev), "a request still running at shutdown")
	})
	e.Close()
	assert.Len(t, pub.published(), 1)
	assert.Equal(t, before+1, droppedCount(t, "dataprovider", FileUpload.Name, "closed"))
}

func TestSubject(t *testing.T) {
	assert.Equal(t, "audit._.files", Subject("", "files"))
	assert.Equal(t, "audit.a_b_c_.files", Subject("a.b c*", "files"))
}

func TestEmitterFromConfig(t *testing.T) {
	e, err := EmitterFromConfig(context.Background(), "test", EmitterConfig{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.(Discard); !ok {
		t.Errorf("audit off: got %T, want Discard", e)
	}
	if _, err := EmitterFromConfig(context.Background(), "test", EmitterConfig{Enabled: true}, Options{}); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("audit on without endpoint: got %v, want ErrNoEndpoint", err)
	}
	for _, mode := range []string{"strict", "best-effort"} {
		cfg := EmitterConfig{Enabled: true, Mode: mode, Nats: raw.Config{Endpoint: "127.0.0.1:1"}}
		if _, err := EmitterFromConfig(context.Background(), "test", cfg, Options{}); !errors.Is(err, ErrUnknownMode) {
			t.Errorf("mode %q: got %v, want ErrUnknownMode", mode, err)
		}
	}
}

func TestRequestContextMerge(t *testing.T) {
	initial := RequestContext{ClientAddress: "198.51.100.1", AuthMethod: "oidc", SessionID: "sid", ClientID: "web", UserAgent: "post"}
	got := RequestContext{ClientAddress: "203.0.113.7", UserAgent: "patch"}.Merge(initial)
	assert.Equal(t, RequestContext{ClientAddress: "203.0.113.7", AuthMethod: "oidc", SessionID: "sid", ClientID: "web", UserAgent: "patch"}, got)

	own := RequestContext{AuthMethod: "basic"}.Merge(initial)
	assert.Equal(t, "basic", own.AuthMethod, "a value of the request itself is kept")
}
