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

package utils

import (
	"testing"

	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"google.golang.org/protobuf/proto"
)

var skipTests = []struct {
	name string
	url  string
	base []string
	out  bool
}{
	{"valid subpath", "/a/b/c/d", []string{"/a/b/"}, true},
	{"invalid subpath", "/a/b/c", []string{"/a/b/c/d"}, false},
	{"equal values", "/a/b/c", []string{"/a/b/c"}, true},
}

func TestSkip(t *testing.T) {
	for _, tt := range skipTests {
		t.Run(tt.name, func(t *testing.T) {
			r := Skip(tt.url, tt.base)
			if r != tt.out {
				t.Errorf("expected %v, want %v", r, tt.out)
			}
		})
	}
}
func TestIsRelativeReference(t *testing.T) {
	tests := []struct {
		ref      *provider.Reference
		expected bool
	}{
		{
			&provider.Reference{},
			false,
		},
		{
			&provider.Reference{
				Path: ".",
			},
			false,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{
					StorageId: "storageId",
					OpaqueId:  "opaqueId",
				},
				Path: "/folder",
			},
			false,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{
					StorageId: "storageId",
					OpaqueId:  "opaqueId",
				},
				Path: "./folder",
			},
			true,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{},
				Path:       "./folder",
			},
			true,
		},
	}

	for _, tt := range tests {
		result := IsRelativeReference(tt.ref)
		if result != tt.expected {
			t.Errorf("IsRelativeReference: ref %v expected %t got %t", tt.ref, tt.expected, result)
		}
	}
}
func TestIsAbsolutReference(t *testing.T) {
	tests := []struct {
		ref      *provider.Reference
		expected bool
	}{
		{
			&provider.Reference{},
			false,
		},
		{
			&provider.Reference{
				Path: ".",
			},
			false,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{
					StorageId: "storageId",
					OpaqueId:  "opaqueId",
				},
				Path: "/folder",
			},
			false,
		},
		{
			&provider.Reference{
				Path: "/folder",
			},
			true,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{},
			},
			true,
		},
		{
			&provider.Reference{
				ResourceId: &provider.ResourceId{
					StorageId: "storageId",
					OpaqueId:  "opaqueId",
				},
			},
			true,
		},
	}

	for _, tt := range tests {
		result := IsAbsoluteReference(tt.ref)
		if result != tt.expected {
			t.Errorf("IsAbsolutReference: ref %v expected %t got %t", tt.ref, tt.expected, result)
		}
	}
}

func TestMakeRelativePath(t *testing.T) {
	tests := []struct {
		path    string
		relPath string
	}{
		{"", "."},
		{"/", "."},
		{"..", "."},
		{"/folder", "./folder"},
		{"/folder/../folder2", "./folder2"},
		{"folder", "./folder"},
	}
	for _, tt := range tests {
		rel := MakeRelativePath(tt.path)
		if rel != tt.relPath {
			t.Errorf("expected %s, got %s", tt.relPath, rel)
		}
	}
}

func TestCanonicalUserID(t *testing.T) {
	tests := []struct {
		name     string
		id       *userpb.UserId
		expected string
	}{
		{name: "nil", expected: ""},
		{name: "regular user", id: &userpb.UserId{OpaqueId: "MixedCase"}, expected: "MixedCase"},
		{name: "guest", id: &userpb.UserId{OpaqueId: "Guest@Example.COM", Type: userpb.UserType_USER_TYPE_GUEST}, expected: "guest@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := CanonicalUserID(tt.id); actual != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestGrantsOpaque(t *testing.T) {
	grants := []*provider.Grant{
		{
			Grantee: &provider.Grantee{
				Type: provider.GranteeType_GRANTEE_TYPE_USER,
				Id:   &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "guest@example.com", Type: userpb.UserType_USER_TYPE_GUEST}},
			},
			Permissions: &provider.ResourcePermissions{Stat: true},
		},
		{
			Grantee: &provider.Grantee{
				Type: provider.GranteeType_GRANTEE_TYPE_GROUP,
				Id:   &provider.Grantee_GroupId{GroupId: &grouppb.GroupId{OpaqueId: "group"}},
			},
			Permissions: &provider.ResourcePermissions{},
		},
	}

	o, err := AppendGrantsToOpaque(nil, "grants", grants)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	read, err := ReadGrantsFromOpaque(o, "grants")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(read) != len(grants) {
		t.Fatalf("expected %d grants, got %d", len(grants), len(read))
	}
	for i := range grants {
		if !proto.Equal(read[i], grants[i]) {
			t.Errorf("grant %d: expected %v, got %v", i, grants[i], read[i])
		}
	}

	if _, err := ReadGrantsFromOpaque(o, "missing"); err == nil {
		t.Error("expected an error for a missing key")
	}

	// ids that are not valid UTF-8 can't be marshaled, the opaque must stay untouched
	invalid := []*provider.Grant{{
		Grantee: &provider.Grantee{
			Type: provider.GranteeType_GRANTEE_TYPE_USER,
			Id:   &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "\xff"}},
		},
	}}
	o, err = AppendGrantsToOpaque(o, "invalid", invalid)
	if err == nil {
		t.Error("expected an error for an invalid grant")
	}
	if ExistsInOpaque(o, "invalid") || !ExistsInOpaque(o, "grants") {
		t.Error("expected the opaque to be unchanged")
	}
}
