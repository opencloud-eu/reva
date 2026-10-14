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

package templates

import (
	"testing"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
)

type testUnit struct {
	expected string
	template string
	user     *userpb.User
}

var tests = []*testUnit{
	{
		expected: "alabasta",
		user: &userpb.User{
			Username: "alabasta",
		},
		template: "{{.Username}}",
	},
	{
		expected: "a/alabasta",
		user: &userpb.User{
			Username: "alabasta",
		},
		template: "{{substr 0 1 .Username}}/{{.Username}}",
	},
	{
		expected: "idp@opaque",
		user: &userpb.User{
			Id: &userpb.UserId{
				Idp:      "idp",
				OpaqueId: "opaque",
			},
		},
		template: "{{.Id.Idp}}@{{.Id.OpaqueId}}",
	},
	{ // test path clean
		expected: "/alabasta",
		user: &userpb.User{
			Username: "alabasta",
		},
		template: "///{{.Username}}",
	},
	{
		expected: "michael",
		user: &userpb.User{
			Username: "MICHAEL",
		},
		template: "{{lower .Username}}",
	},
	{
		expected: "somewhere.com/michael@somewhere.com",
		user: &userpb.User{
			Username: "michael@somewhere.com",
		},
		template: "{{.Email.Domain}}/{{.Username}}",
	},
	{
		expected: "somewhere.com/michael",
		user: &userpb.User{
			Username: "michael@somewhere.com",
		},
		template: "{{.Email.Domain}}/{{.Email.Local}}",
	},
	{
		expected: "_unknown/michael",
		user: &userpb.User{
			Username: "michael",
		},
		template: "{{.Email.Domain}}/{{.Username}}",
	},
}

func TestLayout(t *testing.T) {
	for _, u := range tests {
		got := WithUser(u.user, u.template)
		if u.expected != got {
			t.Fatal("expected: " + u.expected + " got: " + got)
		}
	}
}

func TestLayoutPanic(t *testing.T) {
	assertPanic(t, testBadLayout)
}

func TestUserPanic(t *testing.T) {
	assertPanic(t, testBadUser)
}

// should panic
func testBadLayout() {
	layout := "{{ bad layout syntax"
	user := &userpb.User{}
	WithUser(user, layout)
}

// should panic
func testBadUser() {
	layout := "{{ .DoesNotExist }}"
	user := &userpb.User{}
	WithUser(user, layout)
}

func assertPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("the code did not panic")
		}
	}()
	f()
}

func TestGlob(t *testing.T) {
	for tpl, want := range map[string]string{
		"users/{{.User.Id.OpaqueId}}":                                           "users/*",
		"{{.SpaceType}}/{{.SpaceName}}/{{.SpaceId}}":                            "*/*/*",
		"{{.SpaceType}}/{{.User.Username}}-{{.SpaceId}}":                        "*/*",
		`shared/{{.SpaceName | replace " " "-" | lower}}`:                       "shared/*",
		"users/[old]/{{.User.Username}}?":                                       `users/\[old]/*`,
		"users/{{if .User.Mail}}mail-{{else}}user-{{end}}{{.User.Id.OpaqueId}}": "users/*",
		`{{.SpaceType}}/{{.SpaceName | replace "/" "-"}}`:                       "*/*",
		"users/[old]/x?": `users/\[old]/x\?`,
		"/storage/users": "/storage/users",
		"users/{{.User.Username}}/../{{.User.Id.OpaqueId}}": "users/*",
	} {
		if got := Glob(tpl); got != want {
			t.Errorf("Glob(%q) = %q, want %q", tpl, got, want)
		}
	}
}
