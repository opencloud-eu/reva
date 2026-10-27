// Copyright 2018-2026 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package sciencemesh

import (
	"errors"
	"testing"
)

func TestResolveInviteAcceptDialog(t *testing.T) {
	const provider = "https://cloud.example"
	const javascriptPayload = "javascript://host/%0A(function(){var k=1})()//"

	tests := []struct {
		name    string
		base    string
		dialog  string
		want    string
		wantErr error
	}{
		{
			name:   "root path on the provider",
			base:   provider,
			dialog: "/open-cloud-mesh/accept-invite",
			want:   "https://cloud.example/open-cloud-mesh/accept-invite",
		},
		{
			name:   "query on a path is dropped",
			base:   provider,
			dialog: "/accept?token=from-remote",
			want:   "https://cloud.example/accept",
		},
		{
			name:   "http provider keeps its scheme and port",
			base:   "http://203.0.113.5:8000",
			dialog: "/ocm",
			want:   "http://203.0.113.5:8000/ocm",
		},
		{
			name:   "absolute url with no path uses the provider root",
			base:   provider,
			dialog: "https://cloud.example",
			want:   "https://cloud.example/",
		},
		{
			name:   "same-host absolute url is rebuilt without its query",
			base:   provider,
			dialog: "https://cloud.example/accept?drop=1",
			want:   "https://cloud.example/accept",
		},
		{
			name:   "same-host absolute url keeps a non-root path",
			base:   provider + ":8443",
			dialog: "https://cloud.example:8443/apps/accept",
			want:   "https://cloud.example:8443/apps/accept",
		},
		{
			name:   "host comparison is case-insensitive and the result uses the provider host",
			base:   provider,
			dialog: "https://Cloud.Example/accept",
			want:   "https://cloud.example/accept",
		},
		{
			name:    "javascript url with a host",
			base:    provider,
			dialog:  javascriptPayload,
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "javascript url without a host",
			base:    provider,
			dialog:  "javascript:alert(1)",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "protocol-relative url",
			base:    provider,
			dialog:  "//evil.example/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "different host",
			base:    provider,
			dialog:  "https://evil.example/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "scheme does not match the provider",
			base:    provider,
			dialog:  "http://cloud.example/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "port does not match the provider",
			base:    provider,
			dialog:  "https://cloud.example:8443/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "userinfo",
			base:    provider,
			dialog:  "https://user:pass@cloud.example/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "backslash",
			base:    provider,
			dialog:  "/\\evil.example/accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "fragment",
			base:    provider,
			dialog:  "/accept#x",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "path without a leading slash",
			base:    provider,
			dialog:  "accept",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "empty",
			base:    provider,
			dialog:  "   ",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "query without a path",
			base:    provider,
			dialog:  "?foo=1",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "percent-encoded newline in the path",
			base:    provider,
			dialog:  "/accept%0a",
			wantErr: errInvalidInviteDialog,
		},
		{
			name:    "provider url is not http",
			base:    "javascript://cloud.example",
			dialog:  "/accept",
			wantErr: errInvalidProviderURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInviteAcceptDialog(tt.base, tt.dialog)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInviteAcceptDialog() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
