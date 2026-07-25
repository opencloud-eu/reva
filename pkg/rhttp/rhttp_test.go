// Copyright 2018-2021 CERN
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

package rhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripServerPrefix(t *testing.T) {
	tests := []struct {
		prefix, path, want string
	}{
		{prefix: "", path: "/archiver/foo", want: "/archiver/foo"},
		{prefix: "/", path: "/archiver/foo", want: "/archiver/foo"},
		{prefix: "/test/opencloud", path: "/test/opencloud/archiver/foo", want: "/archiver/foo"},
		{prefix: "/test/opencloud", path: "/test/opencloud", want: "/"},
		{prefix: "/test/opencloud", path: "/other/path", want: "/other/path"},
	}

	for _, tt := range tests {
		if got := stripServerPrefix(tt.prefix, tt.path); got != tt.want {
			t.Errorf("stripServerPrefix(%q, %q) = %q, want %q", tt.prefix, tt.path, got, tt.want)
		}
	}
}

// TestGetHandlerPrefix verifies that a request prefixed with the server's
// deployment prefix (config.Prefix, e.g. set from OpenCloud's HTTP.Prefix)
// still correctly dispatches to the registered per-service handler, since
// rhttp's own dispatch only ever shifts a single path segment and has no
// inherent concept of a multi-segment external prefix.
func TestGetHandlerPrefix(t *testing.T) {
	var gotPath string
	fakeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		conf: &config{
			Prefix: "/test/opencloud",
		},
		handlers: map[string]http.Handler{"archiver": fakeHandler},
	}

	req := httptest.NewRequest(http.MethodGet, "/test/opencloud/archiver/foo", nil)
	rec := httptest.NewRecorder()
	s.dispatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected request under the deployment prefix to reach the archiver handler, got status %d", rec.Code)
	}
	if gotPath != "/foo" {
		t.Errorf("archiver handler saw path %q, want %q", gotPath, "/foo")
	}
}
