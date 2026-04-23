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

package posix_test

import (
	"context"
	"os"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/idcache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Posix", func() {
	var (
		o            *options.Options
		tmpRoot      string
		idCache      *idcache.IDCache
		historyCache *idcache.IDCache
	)

	BeforeEach(func() {
		var err error
		tmpRoot, err = helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]interface{}{
			"root":           tmpRoot,
			"share_folder":   "/Shares",
			"permissionssvc": "any",
			"idcache": map[string]interface{}{
				"cache_store": "nats-js-kv",
			},
		})
		Expect(err).ToNot(HaveOccurred())

		// wire in-process nats server for testing
		_, js, _, err := posixhelpers.NewInProcessNATSServer()
		Expect(err).ToNot(HaveOccurred())

		kv, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: "posix-id-cache",
		})
		Expect(err).ToNot(HaveOccurred())
		idCache, err = idcache.NewStoreIDCache(kv)
		Expect(err).ToNot(HaveOccurred())

		o.IDCache.Database += "_history" // Use a versioned bucket name to avoid conflicts with previous implementations

		historyKv, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: "posix-id-cache-history",
		})
		Expect(err).ToNot(HaveOccurred())
		historyCache, err = idcache.NewStoreIDCache(historyKv)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if tmpRoot != "" {
			os.RemoveAll(tmpRoot)
		}
	})

	Describe("New", func() {
		It("returns a new instance", func() {
			_, err := posix.New(o, nil, idCache, historyCache, &zerolog.Logger{})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
