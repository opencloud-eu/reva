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

package lookup_test

import (
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/testhelpers"
)

var _ = Describe("Lookup", func() {
	var (
		env *helpers.DecomposedTestEnv
	)

	JustBeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	// expectSpaceRoot checks that n.SpaceRoot is the root of the test space. A space
	// root is the top of its space: it has no parent, and its id is the space id. The
	// name of the space is kept in an attribute of its own.
	expectSpaceRoot := func(n *node.Node) {
		Expect(n.SpaceRoot).ToNot(BeNil())
		Expect(n.SpaceRoot.ID).To(Equal(env.SpaceRootRes.GetOpaqueId()))
		Expect(n.SpaceRoot.SpaceID).To(Equal(env.SpaceRootRes.GetSpaceId()))
		Expect(n.SpaceRoot.ParentID).To(BeEmpty())
		spaceName, err := n.SpaceRoot.XattrString(env.Ctx, prefixes.SpaceNameAttr)
		Expect(err).ToNot(HaveOccurred())
		Expect(spaceName).To(Equal("username"))
	}

	Describe("Node from path", func() {
		It("returns the path including a leading slash", func() {
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			})
			Expect(err).ToNot(HaveOccurred())

			path, err := env.Lookup.Path(env.Ctx, n, func(n *node.Node) bool { return true })
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dir1/file1"))
		})
	})

	Describe("Node From Resource only by path", func() {
		It("returns the path including a leading slash and the space root is set", func() {
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/subdir1/file2",
			})
			Expect(err).ToNot(HaveOccurred())

			path, err := env.Lookup.Path(env.Ctx, n, func(n *node.Node) bool { return true })
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dir1/subdir1/file2"))
			expectSpaceRoot(n)
		})
	})

	Describe("Node From Resource only by id", func() {
		It("returns the path including a leading slash and the space root is set", func() {
			// do a node lookup by path
			nRef, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1/file1",
			})
			Expect(err).ToNot(HaveOccurred())

			// try to find the same node by id, which needs the id of its space
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: nRef.SpaceID, OpaqueId: nRef.ID}})
			Expect(err).ToNot(HaveOccurred())
			Expect(n.SpaceRoot).ToNot(BeNil())

			// Check if we got the right node and spaceRoot
			path, err := env.Lookup.Path(env.Ctx, n, func(n *node.Node) bool { return true })
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dir1/file1"))
			expectSpaceRoot(n)
		})
	})

	Describe("Node From Resource by id and relative path", func() {
		It("returns the path including a leading slash and the space root is set", func() {
			// do a node lookup by path for the parent
			nRef, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       "/dir1",
			})
			Expect(err).ToNot(HaveOccurred())

			// try to find the child node by parent id and relative path
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: nRef.SpaceID, OpaqueId: nRef.ID}, Path: "./file1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(n.SpaceRoot).ToNot(BeNil())

			// Check if we got the right node and spaceRoot
			path, err := env.Lookup.Path(env.Ctx, n, func(n *node.Node) bool { return true })
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dir1/file1"))
			expectSpaceRoot(n)
		})
	})
})
