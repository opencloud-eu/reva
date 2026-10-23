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

package options_test

import (
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Options", func() {
	var (
		o      *options.Options
		config map[string]interface{}
	)

	BeforeEach(func() {
		config = map[string]interface{}{}
	})

	Describe("New", func() {
		JustBeforeEach(func() {
			var err error
			o, err = options.New(config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("sets defaults", func() {
			Expect(o.MetadataBackend).ToNot(BeEmpty())
			Expect(o.RevisionsCleanupAsync).To(BeTrue())
			Expect(o.RevisionsCleanupAsyncWorkers).To(Equal(0))
			Expect(o.RevisionsCleanupAsyncQueueSize).To(Equal(0))
			Expect(o.RevisionsCleanupAsyncQueueFull).To(BeEmpty())
			Expect(o.RevisionsCleanupAsyncQueueFullTimeout).To(Equal(5 * time.Second))
		})

		Context("with the revision cleanup configured", func() {
			BeforeEach(func() {
				config["revisions_cleanup_async"] = false
				config["revisions_cleanup_async_workers"] = 3
				config["revisions_cleanup_async_queue_size"] = 50
				config["revisions_cleanup_async_queue_full"] = "block"
				config["revisions_cleanup_async_queue_full_timeout"] = time.Minute
			})

			It("keeps the configured values, including an explicit false", func() {
				Expect(o.RevisionsCleanupAsync).To(BeFalse())
				Expect(o.RevisionsCleanupAsyncWorkers).To(Equal(3))
				Expect(o.RevisionsCleanupAsyncQueueSize).To(Equal(50))
				Expect(o.RevisionsCleanupAsyncQueueFull).To(Equal("block"))
				Expect(o.RevisionsCleanupAsyncQueueFullTimeout).To(Equal(time.Minute))
			})
		})

		Context("with unclean root path configuration", func() {
			BeforeEach(func() {
				config["root"] = "foo/"
			})

			It("sanitizes the root path", func() {
				Expect(o.Root).To(Equal("foo"))
			})
		})
	})
})
