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

package decomposedfs_test

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"hash/adler32"
	"io"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/owncloud/reva/v2/pkg/errtypes"
	"github.com/owncloud/reva/v2/pkg/storage"
	helpers "github.com/owncloud/reva/v2/pkg/storage/utils/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("CommitUpload", func() {
	var (
		env *helpers.TestEnv
		ref *provider.Reference
	)

	JustBeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		ref = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/new-file.txt",
		}

		// Blobstore.UploadFromReader is invoked for every successful commit.
		env.Blobstore.On("UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, mock.AnythingOfType("int64")).Return(nil)

		// TouchFile-first protocol: node must exist before CommitUpload is called.
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).
			Return(&provider.ResourcePermissions{InitiateFileUpload: true, Stat: true}, nil).Times(1)
		_, err = env.Fs.TouchFile(env.Ctx, ref, false, "")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	makeSource := func(content []byte, metadata map[string]string) storage.UploadSource {
		sha1h := sha1.New()
		md5h := md5.New()
		adler32h := adler32.New()
		r1 := io.TeeReader(bytes.NewReader(content), sha1h)
		r2 := io.TeeReader(r1, md5h)
		io.Copy(adler32h, r2) //nolint:errcheck
		return storage.UploadSource{
			Body:     io.NopCloser(bytes.NewReader(content)),
			Length:   int64(len(content)),
			Metadata: metadata,
			Checksums: storage.UploadChecksums{
				SHA1:    sha1h.Sum(nil),
				MD5:     md5h.Sum(nil),
				Adler32: adler32h.Sum(nil),
			},
		}
	}

	Context("when node does not exist", func() {
		It("fails with NotFound", func() {
			missingRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/never-created.txt",
			}
			_, err := env.Fs.CommitUpload(env.Ctx, missingRef, makeSource([]byte("x"), nil))
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsNotFound)
			Expect(ok).To(BeTrue(), "expected errtypes.NotFound, got %T: %v", err, err)
		})
	})

	Context("when checksums are missing", func() {
		It("fails with BadRequest", func() {
			_, err := env.Fs.CommitUpload(env.Ctx, ref, storage.UploadSource{
				Body:   io.NopCloser(bytes.NewReader([]byte("x"))),
				Length: 1,
			})
			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsBadRequest)
			Expect(ok).To(BeTrue(), "expected errtypes.BadRequest, got %T: %v", err, err)
		})
	})

	Context("on a new file", func() {
		It("commits the bytes and returns ResourceInfo", func() {
			content := []byte("hello reva")

			ri, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource(content, map[string]string{
				"providerID": "test-provider",
			}))

			Expect(err).ToNot(HaveOccurred())
			Expect(ri).ToNot(BeNil())
			Expect(ri.Id).ToNot(BeNil())
			Expect(ri.Id.OpaqueId).ToNot(BeEmpty())
			Expect(ri.Id.SpaceId).To(Equal(env.SpaceRootRes.SpaceId))
			Expect(ri.Id.StorageId).To(Equal("test-provider"))
			Expect(ri.Etag).ToNot(BeEmpty())
			Expect(ri.Mtime).ToNot(BeNil())

			// blobstore was called once
			env.Blobstore.AssertCalled(GinkgoT(), "UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, mock.AnythingOfType("int64"))
		})
	})

	Context("on overwrite", func() {
		It("commits the new bytes and preserves node identity", func() {
			// first commit: create the file
			ri1, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource([]byte("original content"), map[string]string{
				"mtime":      "1700000000",
				"providerID": "test-provider",
			}))
			Expect(err).ToNot(HaveOccurred())

			// second commit: overwrite with different bytes and mtime
			ri2, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource([]byte("brand new content"), map[string]string{
				"mtime":      "1750000000",
				"providerID": "test-provider",
			}))
			Expect(err).ToNot(HaveOccurred())

			// node identity is preserved across the overwrite
			Expect(ri2.Id.OpaqueId).To(Equal(ri1.Id.OpaqueId))
			// etag changed because mtime changed
			Expect(ri2.Etag).ToNot(Equal(ri1.Etag))
			// blobstore was called twice (once per commit)
			env.Blobstore.AssertNumberOfCalls(GinkgoT(), "UploadFromReader", 2)
		})
	})

	Context("checksum verification", func() {
		content := []byte("hello reva")

		It("commits when no client checksum is supplied", func() {
			ri, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource(content, nil))

			Expect(err).ToNot(HaveOccurred())
			Expect(ri).ToNot(BeNil())
		})
	})

	Context("mtime handling", func() {
		It("uses the supplied mtime", func() {
			ri, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource([]byte("hello"), map[string]string{
				"mtime": "1750000000",
			}))

			Expect(err).ToNot(HaveOccurred())
			Expect(ri).ToNot(BeNil())
			Expect(ri.Mtime).ToNot(BeNil())
			Expect(ri.Mtime.Seconds).To(Equal(uint64(1750000000)))
			Expect(ri.Mtime.Nanos).To(Equal(uint32(0)))
		})

		It("rejects malformed mtime", func() {
			_, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource([]byte("hello"), map[string]string{
				"mtime": "not-a-date",
			}))

			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsBadRequest)
			Expect(ok).To(BeTrue(), "expected errtypes.BadRequest, got %T: %v", err, err)
		})
	})

	Context("idempotency", func() {
		It("two clean runs with same ref+source produce equal final state", func() {
			content := []byte("idempotent payload")
			metadata := map[string]string{
				"mtime":      "1750000000",
				"providerID": "test-provider",
			}

			ri1, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource(content, metadata))
			Expect(err).ToNot(HaveOccurred())

			ri2, err := env.Fs.CommitUpload(env.Ctx, ref, makeSource(content, metadata))
			Expect(err).ToNot(HaveOccurred())

			Expect(ri2.Id.OpaqueId).To(Equal(ri1.Id.OpaqueId))
			Expect(ri2.Etag).To(Equal(ri1.Etag))
			Expect(ri2.Mtime.Seconds).To(Equal(ri1.Mtime.Seconds))
			Expect(ri2.Mtime.Nanos).To(Equal(ri1.Mtime.Nanos))
		})
	})

})
