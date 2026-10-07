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

package publicshare_test

import (
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/publicshare"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MatchesFilters", func() {
	// A persisted public share can have a nil ResourceId (see OCISDEV-877: ocis shipped
	// a repair CLI for exactly this class of corrupt row). MatchesFilters is called
	// before ListPublicShares' own nil-ResourceId guard, so it must not panic.
	It("does not panic and returns false for a StorageIDFilter against a share with a nil ResourceId", func() {
		share := &link.PublicShare{ResourceId: nil}
		filters := []*link.ListPublicSharesRequest_Filter{
			publicshare.StorageIDFilter("s"),
		}

		var result bool
		Expect(func() { result = publicshare.MatchesFilters(share, filters) }).ToNot(Panic())
		Expect(result).To(BeFalse())
	})

	It("does not panic and returns false for a TYPE_RESOURCE_ID filter against a share with a nil ResourceId", func() {
		share := &link.PublicShare{ResourceId: nil}
		rid := &provider.ResourceId{StorageId: "s", SpaceId: "sp", OpaqueId: "o"}
		filters := []*link.ListPublicSharesRequest_Filter{
			publicshare.ResourceIDFilter(rid),
		}

		var result bool
		Expect(func() { result = publicshare.MatchesFilters(share, filters) }).ToNot(Panic())
		Expect(result).To(BeFalse())
	})
})
