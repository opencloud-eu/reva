package download

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/audit"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFS only implements Download, every other call panics.
type fakeFS struct {
	storage.FS
	content string
	err     error
}

type seekableContent struct{ *strings.Reader }

func (seekableContent) Close() error { return nil }

func (f fakeFS) Download(ctx context.Context, ref *provider.Reference, openReaderFunc func(*provider.ResourceInfo) bool) (*provider.ResourceInfo, io.ReadCloser, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	md := &provider.ResourceInfo{
		Id:   &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "item"},
		Path: "/docs/report.pdf",
		Size: uint64(len(f.content)),
		Etag: "etag",
	}
	if !openReaderFunc(md) {
		return md, nil, nil
	}
	return md, seekableContent{strings.NewReader(f.content)}, nil
}

type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Emit(_ context.Context, ev audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func download(t *testing.T, fs storage.FS, method string, header http.Header, ctx context.Context) *recorder {
	t.Helper()
	rec := &recorder{}
	r := httptest.NewRequest(method, "/docs/report.pdf", nil)
	for k, v := range header {
		r.Header[k] = v
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r = r.WithContext(audit.ContextSetEmitter(ctx, rec))
	GetOrHeadFile(httptest.NewRecorder(), r, fs, "storage$space")
	return rec
}

var fromClient = http.Header{http.CanonicalHeaderKey(audit.Header): {"ip=203.0.113.7"}}

func TestDownloadIsAuditedWhenDone(t *testing.T) {
	rec := download(t, fakeFS{content: "hello"}, http.MethodGet, fromClient, nil)

	require.Len(t, rec.events, 1)
	ev := rec.events[0]
	assert.Equal(t, audit.FileDownload, ev.Definition)
	assert.Equal(t, audit.OutcomeSuccess, ev.Outcome)
	assert.Equal(t, "storage$space!item", ev.Target.ID)
	assert.Equal(t, "report.pdf", ev.Target.Name)
	assert.Nil(t, ev.Actor)
}

func TestDownloadsThatAreNotAudited(t *testing.T) {
	for name, tc := range map[string]struct {
		method string
		header http.Header
	}{
		"internal read without the header": {http.MethodGet, nil},
		"HEAD request":                     {http.MethodHead, fromClient},
		"range request not from the start": {http.MethodGet, http.Header{
			http.CanonicalHeaderKey(audit.Header): {"ip=203.0.113.7"},
			"Range":                               {"bytes=2-3"},
		}},
		"not modified": {http.MethodGet, http.Header{
			http.CanonicalHeaderKey(audit.Header): {"ip=203.0.113.7"},
			"If-None-Match":                       {"etag"},
		}},
	} {
		rec := download(t, fakeFS{content: "hello"}, tc.method, tc.header, nil)
		assert.Empty(t, rec.events, name)
	}

	rec := download(t, fakeFS{content: "hello"}, http.MethodGet, http.Header{
		http.CanonicalHeaderKey(audit.Header): {"ip=203.0.113.7"},
		"Range":                               {"bytes=0-1"},
	}, nil)
	assert.Len(t, rec.events, 1, "the range from the start counts")
}

func TestDeniedDownloadIsAudited(t *testing.T) {
	rec := download(t, fakeFS{err: errtypes.PermissionDenied("no")}, http.MethodGet, fromClient, nil)

	require.Len(t, rec.events, 1)
	assert.Equal(t, audit.OutcomeDenied, rec.events[0].Outcome)
	assert.Equal(t, "permission_denied", rec.events[0].Reason)
}

func TestPublicLinkDownloadHasTheLinkAsActor(t *testing.T) {
	ctx := ctxpkg.ContextSetScopes(context.Background(), map[string]*authpb.Scope{"publicshare:l-1": {}})

	rec := download(t, fakeFS{content: "hello"}, http.MethodGet, fromClient, ctx)

	require.Len(t, rec.events, 1)
	require.NotNil(t, rec.events[0].Actor)
	assert.Equal(t, audit.ActorPublicLink, rec.events[0].Actor.Type)
	assert.Equal(t, "l-1", rec.events[0].Actor.ID)
}
