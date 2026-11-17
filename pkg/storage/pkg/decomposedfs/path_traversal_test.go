// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"os"
	"path/filepath"
	"strings"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	helpers "github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/testhelpers"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// decomposedfs turns node ids into paths with Pathify, so a plain id with ".."
// in it points at another node, and a revision suffix can point anywhere.
var _ = Describe("ids that leave the node", func() {
	var (
		env       *helpers.DecomposedTestEnv
		dir1      *node.Node
		dir2      *node.Node
		victimDir string
	)

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		dir1, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1",
		})
		Expect(err).ToNot(HaveOccurred())

		dir2, err = env.CreateTestDir("dir2", &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/"})
		Expect(err).ToNot(HaveOccurred())

		victimDir = GinkgoT().TempDir()

		// grant everything, the id check alone has to stop the requests
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			InitiateFileUpload: true,
		}, nil)
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	forgedRef := func(nodeID string) *provider.Reference {
		return &provider.Reference{
			ResourceId: &provider.ResourceId{
				StorageId: env.SpaceRootRes.StorageId,
				SpaceId:   env.SpaceRootRes.SpaceId,
				OpaqueId:  nodeID,
			},
		}
	}

	It("does not resolve a plain id with '..' to another node", func() {
		// nodes/xx/xx/xx/xx/<rest of dir1>/.. five times is the nodes directory
		forged := dir1.ID + "/" + strings.Repeat("../", 5) + lookup.Pathify(dir2.ID, 4, 2)
		_, err := env.Fs.GetMD(env.Ctx, forgedRef(forged), nil, nil)
		Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
	})

	It("does not create a child link outside the space", func() {
		forged := dir1.ID + node.RevisionIDDelimiter + "x/" + strings.Repeat("../", 40) + strings.TrimPrefix(victimDir, "/")
		ref := forgedRef(forged)
		ref.Path = "./new.txt"
		_, err := env.Fs.TouchFile(env.Ctx, ref, false, "")
		Expect(err).To(HaveOccurred())

		entries, err := os.ReadDir(victimDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
		Expect(filepath.Join(victimDir, "new.txt")).ToNot(BeAnExistingFile())
	})
})
