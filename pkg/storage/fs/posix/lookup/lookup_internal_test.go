// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package lookup

import "testing"

func TestRelWithin(t *testing.T) {
	for _, tc := range []struct {
		name, root, p string
		rel           string
		ok            bool
	}{
		{"the root itself", "/data", "/data", ".", true},
		{"a child", "/data", "/data/users/einstein", "users/einstein", true},
		{"a child starting with ..", "/data", "/data/..foo", "..foo", true},
		{"the parent", "/data", "/", "", false},
		{"a sibling", "/data", "/elsewhere", "", false},
		{"a sibling sharing the prefix", "/data", "/data2/file", "", false},
		{"a path climbing out", "/data", "/data/../etc", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel, ok := relWithin(tc.root, tc.p)
			if rel != tc.rel || ok != tc.ok {
				t.Errorf("relWithin(%q, %q) = %q, %v, want %q, %v", tc.root, tc.p, rel, ok, tc.rel, tc.ok)
			}
		})
	}
}
