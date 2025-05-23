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

package decomposeds3_test

import (
	"os"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposeds3"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("decomposeds3", func() {
	var (
		options map[string]interface{}
		tmpRoot string
	)

	BeforeEach(func() {
		var err error
		tmpRoot, err = helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		options = map[string]interface{}{
			"root":           tmpRoot,
			"share_folder":   "/Shares",
			"permissionssvc": "any",
			"s3.endpoint":    "http://1.2.3.4:5000",
			"s3.region":      "default",
			"s3.bucket":      "the-bucket",
			"s3.access_key":  "foo",
			"s3.secret_key":  "bar",
		}
	})

	AfterEach(func() {
		if tmpRoot != "" {
			os.RemoveAll(tmpRoot)
		}
	})

	Describe("New", func() {
		It("fails on missing s3 configuration", func() {
			_, err := decomposeds3.New(map[string]interface{}{}, nil, &zerolog.Logger{})
			Expect(err).To(MatchError("S3 configuration incomplete"))
		})

		It("works with complete configuration", func() {
			_, err := decomposeds3.New(options, nil, &zerolog.Logger{})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
