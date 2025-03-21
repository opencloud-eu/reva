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

package metadata_test

import (
	"context"
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
)

type testNode struct {
	spaceID string
	id      string
	path    string
}

func (t testNode) GetSpaceID() string {
	return t.spaceID
}

func (t testNode) GetID() string {
	return t.id
}

func (t testNode) InternalPath() string {
	return t.path
}

var _ = Describe("Backend", func() {
	var (
		tmpdir string
		n      testNode

		backend metadata.Backend
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp(os.TempDir(), "XattrsBackendTest-")
		Expect(err).ToNot(HaveOccurred())
	})

	JustBeforeEach(func() {
		n = testNode{
			spaceID: "123",
			id:      "456",
			path:    path.Join(tmpdir, "file"),
		}
	})

	AfterEach(func() {
		if tmpdir != "" {
			os.RemoveAll(tmpdir)
		}
	})

	Describe("MessagePackBackend", func() {
		BeforeEach(func() {
			backend = metadata.NewMessagePackBackend(cache.Config{
				Database: tmpdir,
			})
		})

		Describe("Set", func() {
			It("sets an attribute", func() {
				data := []byte(`bar\`)
				err := backend.Set(context.Background(), n, "foo", data)
				Expect(err).ToNot(HaveOccurred())

				readData, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(readData).To(Equal(data))
			})

			It("handles funny strings", func() {
				data := []byte(`bar\`)
				err := backend.Set(context.Background(), n, "foo", data)
				Expect(err).ToNot(HaveOccurred())

				readData, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(readData).To(Equal(data))
			})

			It("updates an attribute", func() {
				err := backend.Set(context.Background(), n, "foo", []byte("bar"))
				Expect(err).ToNot(HaveOccurred())
				err = backend.Set(context.Background(), n, "foo", []byte("baz"))
				Expect(err).ToNot(HaveOccurred())

				readData, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(readData).To(Equal([]byte("baz")))
			})

			It("sets an empty attribute", func() {
				_, err := backend.Get(context.Background(), n, "foo")
				Expect(err).To(HaveOccurred())

				err = backend.Set(context.Background(), n, "foo", []byte{})
				Expect(err).ToNot(HaveOccurred())

				v, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal([]byte{}))
			})
		})

		Describe("SetMultiple", func() {
			It("sets attributes", func() {
				data := map[string][]byte{"foo": []byte("bar"), "baz": []byte("qux")}
				err := backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				readData, err := backend.All(context.Background(), n)
				Expect(err).ToNot(HaveOccurred())
				Expect(readData).To(Equal(data))
			})

			It("updates an attribute", func() {
				err := backend.Set(context.Background(), n, "foo", []byte("something"))

				data := map[string][]byte{"foo": []byte("bar"), "baz": []byte("qux")}
				Expect(err).ToNot(HaveOccurred())
				err = backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				readData, err := backend.All(context.Background(), n)
				Expect(err).ToNot(HaveOccurred())
				Expect(readData).To(Equal(data))
			})
		})

		Describe("All", func() {
			It("returns the entries", func() {
				data := map[string][]byte{"foo": []byte("123"), "bar": []byte("baz")}
				err := backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				v, err := backend.All(context.Background(), n)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(v)).To(Equal(2))
				Expect(v["foo"]).To(Equal([]byte("123")))
				Expect(v["bar"]).To(Equal([]byte("baz")))
			})

			It("fails when the metafile does not exist", func() {
				_, err := backend.All(context.Background(), n)
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Get", func() {
			It("returns the attribute", func() {
				data := map[string][]byte{"foo": []byte("bar")}
				err := backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				v, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal([]byte("bar")))
			})

			It("returns an error on unknown attributes", func() {
				_, err := backend.Get(context.Background(), n, "foo")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("GetInt64", func() {
			It("returns the attribute", func() {
				data := map[string][]byte{"foo": []byte("123")}
				err := backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				v, err := backend.GetInt64(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal(int64(123)))
			})

			It("returns an error on unknown attributes", func() {
				_, err := backend.GetInt64(context.Background(), n, "foo")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Remove", func() {
			It("deletes an attribute", func() {
				data := map[string][]byte{"foo": []byte("bar")}
				err := backend.SetMultiple(context.Background(), n, data, true)
				Expect(err).ToNot(HaveOccurred())

				v, err := backend.Get(context.Background(), n, "foo")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal([]byte("bar")))

				err = backend.Remove(context.Background(), n, "foo", true)
				Expect(err).ToNot(HaveOccurred())

				_, err = backend.Get(context.Background(), n, "foo")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("IsMetaFile", func() {
			It("returns true", func() {
				Expect(backend.IsMetaFile("foo.mpk")).To(BeTrue())
			})

			It("returns false", func() {
				Expect(backend.IsMetaFile("foo.txt")).To(BeFalse())
			})
		})
	})
})
