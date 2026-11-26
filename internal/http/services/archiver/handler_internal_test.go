// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package archiver

import (
	"context"
	"errors"
	"strings"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v3/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	cs3mocks "github.com/opencloud-eu/reva/v3/tests/cs3mocks/mocks"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
)

func TestSanitizeArchiveName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Documents", "Documents"},
		{"spaces and umlauts kept", "Pröbe Ördner 308", "Pröbe Ördner 308"},
		{"double quote removed", `a"b`, "ab"},
		{"slashes removed", "a/b\\c", "abc"},
		{"crlf removed", "line\r\nbreak", "linebreak"},
		{"control chars removed", "a\x00\x07b", "ab"},
		{"del removed (0x7f, lower bound of the c1 strip)", "a\x7fb", "ab"},
		{"surrounding space trimmed", "  spaced  ", "spaced"},
		{"dot only", ".", ""},
		{"dotdot only", "..", ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeArchiveName(tc.in); got != tc.want {
				t.Errorf("sanitizeArchiveName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// fakeSelector is a minimal pool.Selectable returning a fixed gateway client (or error),
// so resourceName can be tested without the real gateway pool.
type fakeSelector struct {
	client gateway.GatewayAPIClient
	err    error
}

func (f fakeSelector) Next(_ ...pool.Option) (gateway.GatewayAPIClient, error) {
	return f.client, f.err
}

func TestResourceName(t *testing.T) {
	ok := func(info *provider.ResourceInfo) *provider.StatResponse {
		return &provider.StatResponse{Status: &rpc.Status{Code: rpc.Code_CODE_OK}, Info: info}
	}

	cases := []struct {
		name    string
		resp    *provider.StatResponse
		statErr error
		selErr  error
		want    string
		wantErr bool
	}{
		{name: "ok uses name", resp: ok(&provider.ResourceInfo{Name: "Documents"}), want: "Documents"},
		{name: "empty name falls back to path base", resp: ok(&provider.ResourceInfo{Path: "/space/Reports"}), want: "Reports"},
		{name: "name is sanitized", resp: ok(&provider.ResourceInfo{Name: `a"b/c`}), want: "abc"},
		{name: "name sanitizing to empty keeps default", resp: ok(&provider.ResourceInfo{Name: "/"}), want: ""},
		{name: "empty name and path keep default", resp: ok(&provider.ResourceInfo{}), want: ""},
		{name: "stat error returns error", statErr: errors.New("boom"), wantErr: true},
		{name: "non-OK status keeps default", resp: &provider.StatResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}}, want: "", wantErr: true},
		{name: "selector error keeps default", selErr: errors.New("no gateway"), want: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := cs3mocks.NewGatewayAPIClient(t)
			if tc.selErr == nil {
				gw.EXPECT().Stat(mock.Anything, mock.Anything).Return(tc.resp, tc.statErr).Once()
			}
			log := zerolog.Nop()
			s := &svc{gatewaySelector: fakeSelector{client: gw, err: tc.selErr}, log: &log}

			got, err := s.resourceName(context.Background(), &provider.ResourceId{OpaqueId: "x"})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if got != tc.want {
				t.Errorf("resourceName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestArchiveNameContentDisposition(t *testing.T) {
	// A name with umlauts survives sanitization and still encodes correctly:
	// net.ContentDispositionAttachment emits both the RFC 6266 filename* form and the raw filename.
	got := net.ContentDispositionAttachment(sanitizeArchiveName("Pröbe Ördner 308"))
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Errorf("expected RFC 6266 filename* form, got %q", got)
	}
	if !strings.Contains(got, `filename="Pröbe Ördner 308"`) {
		t.Errorf("expected raw filename, got %q", got)
	}
}

// recorder keeps the audit events emitted by the archiver.
type recorder struct{ events []audit.Event }

func (r *recorder) Emit(_ context.Context, ev audit.Event) error {
	r.events = append(r.events, ev)
	return nil
}

func TestAuditArchive(t *testing.T) {
	ok := &rpc.Status{Code: rpc.Code_CODE_OK}
	file := &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "file"}
	folder := &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "folder"}

	gw := cs3mocks.NewGatewayAPIClient(t)
	gw.EXPECT().Stat(mock.Anything, mock.MatchedBy(func(r *provider.StatRequest) bool { return r.GetRef().GetResourceId() == file })).
		Return(&provider.StatResponse{Status: ok, Info: &provider.ResourceInfo{Name: "report.pdf", Path: "/projects/report.pdf", Type: provider.ResourceType_RESOURCE_TYPE_FILE}}, nil)
	gw.EXPECT().Stat(mock.Anything, mock.MatchedBy(func(r *provider.StatRequest) bool { return r.GetRef().GetResourceId() == folder })).
		Return(&provider.StatResponse{Status: ok, Info: &provider.ResourceInfo{Name: "docs", Path: ".", Type: provider.ResourceType_RESOURCE_TYPE_CONTAINER}}, nil)

	log := zerolog.Nop()
	rec := &recorder{}
	s := &svc{gatewaySelector: fakeSelector{client: gw}, log: &log, emitter: rec}
	s.auditArchive(context.Background(), []*provider.ResourceId{file, folder}, audit.OutcomeSuccess, "")

	if len(rec.events) != 2 {
		t.Fatalf("got %d events, want 2", len(rec.events))
	}
	if ev := rec.events[0]; ev.Definition != audit.FileDownload || ev.Target.Name != "report.pdf" || ev.Target.Path != "./projects/report.pdf" || ev.Target.ID != "storage$space!file" {
		t.Errorf("file: got %s %+v", ev.Definition.Name, ev.Target)
	}
	if ev := rec.events[1]; ev.Definition != audit.FolderDownload || ev.Target.Name != "docs" || ev.Target.Path != "" {
		t.Errorf("space root without a path: got %s %+v", ev.Definition.Name, ev.Target)
	}
}

func TestAuditArchiveOff(t *testing.T) {
	// the mock fails the test on any call, with audit off there is no Stat
	gw := cs3mocks.NewGatewayAPIClient(t)
	log := zerolog.Nop()
	s := &svc{gatewaySelector: fakeSelector{client: gw}, log: &log, emitter: audit.Discard{}}
	s.auditArchive(context.Background(), []*provider.ResourceId{{OpaqueId: "file"}}, audit.OutcomeSuccess, "")
}

func TestAuditArchiveAborted(t *testing.T) {
	file := &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "file"}
	gw := cs3mocks.NewGatewayAPIClient(t)
	gw.EXPECT().Stat(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), mock.Anything).
		Return(&provider.StatResponse{Status: &rpc.Status{Code: rpc.Code_CODE_OK}, Info: &provider.ResourceInfo{Name: "report.pdf", Type: provider.ResourceType_RESOURCE_TYPE_FILE}}, nil)

	// the client aborted the download, the request ctx is cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	log := zerolog.Nop()
	rec := &recorder{}
	s := &svc{gatewaySelector: fakeSelector{client: gw}, log: &log, emitter: rec}
	s.auditArchive(ctx, []*provider.ResourceId{file}, audit.OutcomeFailure, "incomplete")

	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	if ev := rec.events[0]; ev.Definition != audit.FileDownload || ev.Target.Name != "report.pdf" {
		t.Errorf("got %s %+v, want a file download with the name", ev.Definition.Name, ev.Target)
	}
}
