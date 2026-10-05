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

package json

import (
	"github.com/opencloud-eu/reva/v2/pkg/publicshare"
)

// SetStatConcurrency overrides the bounded worker pool size used by
// ListPublicShares' stat fan-out. maxConcurrency is deliberately not
// operator-configurable (see defaultStatConcurrency), but tests still need to
// exercise the bound directly rather than waiting out the real default.
//
// Export for testing only.
func SetStatConcurrency(m publicshare.Manager, n int) {
	m.(*manager).maxConcurrency = n
}
