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

package net_test

import (
	"context"
	"encoding/json"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typespb "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebDAVPermissions", func() {
	var (
		owner = &userpb.User{Id: &userpb.UserId{OpaqueId: "owner"}, Username: "owner"}
		other = &userpb.User{Id: &userpb.UserId{OpaqueId: "other"}, Username: "other"}

		// a resource the user may edit, owned by "owner"
		resource = func(t provider.ResourceType, opaque *typespb.Opaque) *provider.ResourceInfo {
			return &provider.ResourceInfo{
				Type:   t,
				Owner:  owner.Id,
				Opaque: opaque,
				PermissionSet: &provider.ResourcePermissions{
					Stat: true, ListContainer: true, InitiateFileDownload: true,
					InitiateFileUpload: true, Move: true, Delete: true,
				},
			}
		}

		ownerCtx = ctxpkg.ContextSetUser(context.Background(), owner)
		otherCtx = ctxpkg.ContextSetUser(context.Background(), other)
	)

	It("omits the shared marker when the current user owns the resource", func() {
		Expect(net.WebDAVPermissions(ownerCtx, resource(provider.ResourceType_RESOURCE_TYPE_FILE, nil))).ToNot(HavePrefix("S"))
	})

	It("leads with the shared marker when the resource is not owned by the current user", func() {
		Expect(net.WebDAVPermissions(otherCtx, resource(provider.ResourceType_RESOURCE_TYPE_FILE, nil))).To(HavePrefix("S"))
	})

	It("returns a permission string for a container", func() {
		perms := net.WebDAVPermissions(otherCtx, resource(provider.ResourceType_RESOURCE_TYPE_CONTAINER, nil))
		Expect(perms).ToNot(BeEmpty())
		Expect(perms).To(HavePrefix("S"))
	})

	It("handles a public-link resource via the link-share opaque", func() {
		b, err := json.Marshal(&link.PublicShare{Id: &link.PublicShareId{OpaqueId: "share-1"}})
		Expect(err).ToNot(HaveOccurred())
		opaque := &typespb.Opaque{Map: map[string]*typespb.OpaqueEntry{
			"link-share": {Decoder: "json", Value: b},
		}}
		Expect(net.WebDAVPermissions(ownerCtx, resource(provider.ResourceType_RESOURCE_TYPE_FILE, opaque))).ToNot(BeEmpty())
	})
})
