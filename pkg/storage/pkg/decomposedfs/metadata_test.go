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
	"fmt"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("SetArbitraryMetadata", func() {
	var (
		env *helpers.DecomposedTestEnv
		ref *provider.Reference
	)

	JustBeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		ref = &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1/file1",
		}
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	Context("with sufficient permissions", func() {
		It("sets arbitrary metadata attributes", func() {
			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar", "baz": "qux"},
			})
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())

			foo, err := n.XattrString(env.Ctx, prefixes.MetadataPrefix+"foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(foo).To(Equal("bar"))

			baz, err := n.XattrString(env.Ctx, prefixes.MetadataPrefix+"baz")
			Expect(err).ToNot(HaveOccurred())
			Expect(baz).To(Equal("qux"))
		})

		It("sets the mtime", func() {
			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"mtime": "1600000000.0"},
			})
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())

			mtime, err := n.XattrString(env.Ctx, prefixes.MTimeAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(mtime).To(Equal(time.Unix(1600000000, 0).UTC().Format(time.RFC3339Nano)))

			// mtime must not be persisted as a plain metadata attribute
			_, err = n.XattrString(env.Ctx, prefixes.MetadataPrefix+"mtime")
			Expect(err).To(HaveOccurred())
		})

		It("sets the etag", func() {
			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"etag": "abcdef"},
			})
			Expect(err).ToNot(HaveOccurred())

			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())

			etag, err := n.XattrString(env.Ctx, prefixes.TmpEtagAttr)
			Expect(err).ToNot(HaveOccurred())
			Expect(etag).To(Equal("\"abcdef\""))

			// etag must not be persisted as a plain metadata attribute
			_, err = n.XattrString(env.Ctx, prefixes.MetadataPrefix+"etag")
			Expect(err).To(HaveOccurred())
		})

		It("only propagates when the metadata actually changes", func() {
			parentRef := &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1",
			}
			// tmtimeOf reads the tree mtime the propagator bumps whenever it runs.
			tmtimeOf := func() time.Time {
				p, err := env.Lookup.NodeFromResource(env.Ctx, parentRef)
				Expect(err).ToNot(HaveOccurred())
				t, err := p.GetTMTime(env.Ctx)
				Expect(err).ToNot(HaveOccurred())
				return t
			}

			// initial write changes the metadata and therefore propagates
			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).ToNot(HaveOccurred())
			afterChange := tmtimeOf()

			// writing the same value must not propagate -> tmtime stays byte-identical,
			// independent of the clock, because propagation is skipped entirely
			err = env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(tmtimeOf()).To(BeTemporally("==", afterChange))

			// sanity check: an actual change does propagate. Each iteration writes a
			// distinct value to force a change, so the tmtime advances as soon as the
			// clock has moved past afterChange - no fixed sleep required.
			i := 0
			Eventually(func() time.Time {
				i++
				err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
					Metadata: map[string]string{"foo": fmt.Sprintf("baz%d", i)},
				})
				Expect(err).ToNot(HaveOccurred())
				return tmtimeOf()
			}).Should(BeTemporally(">", afterChange))
		})
	})

	Context("with insufficient permissions", func() {
		It("denies setting metadata but reveals the resource when the user may stat", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
				Stat: true,
			}, nil)

			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).To(MatchError(ContainSubstring("permission denied")))
		})

		It("hides the resource when the user may not even stat", func() {
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
			env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{}, nil)

			err := env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Context("with a non existing resource", func() {
		It("returns not found", func() {
			err := env.Fs.SetArbitraryMetadata(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/does-not-exist",
			}, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Context("with a locked resource", func() {
		It("refuses to set metadata without the matching lock", func() {
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
			Expect(err).ToNot(HaveOccurred())

			err = n.SetLock(env.Ctx, &provider.Lock{
				Type:   provider.LockType_LOCK_TYPE_EXCL,
				User:   env.Owner.Id,
				LockId: uuid.New().String(),
			})
			Expect(err).ToNot(HaveOccurred())

			err = env.Fs.SetArbitraryMetadata(env.Ctx, ref, &provider.ArbitraryMetadata{
				Metadata: map[string]string{"foo": "bar"},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})
