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
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
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
	}{
		{name: "ok uses name", resp: ok(&provider.ResourceInfo{Name: "Documents"}), want: "Documents"},
		{name: "empty name falls back to path base", resp: ok(&provider.ResourceInfo{Path: "/space/Reports"}), want: "Reports"},
		{name: "name is sanitized", resp: ok(&provider.ResourceInfo{Name: `a"b/c`}), want: "abc"},
		{name: "name sanitizing to empty keeps default", resp: ok(&provider.ResourceInfo{Name: "/"}), want: ""},
		{name: "empty name and path keep default", resp: ok(&provider.ResourceInfo{}), want: ""},
		{name: "stat error keeps default", statErr: errors.New("boom"), want: ""},
		{name: "non-OK status keeps default", resp: &provider.StatResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}}, want: ""},
		{name: "selector error keeps default", selErr: errors.New("no gateway"), want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := cs3mocks.NewGatewayAPIClient(t)
			if tc.selErr == nil {
				gw.EXPECT().Stat(mock.Anything, mock.Anything).Return(tc.resp, tc.statErr).Once()
			}
			log := zerolog.Nop()
			s := &svc{gatewaySelector: fakeSelector{client: gw, err: tc.selErr}, log: &log}

			got := s.resourceName(context.Background(), &provider.ResourceId{OpaqueId: "x"})
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
