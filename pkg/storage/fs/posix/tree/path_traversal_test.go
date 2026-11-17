// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// /app/new stats a parent container by id and then touches a file in it, and
// the revision handlers take a revision key next to the reference. In both
// cases the permissions are checked on the node before the revision delimiter,
// so an id like <own id>.REV.x/../../<somewhere> must never become a path.
var _ = Describe("ids that leave the space", func() {
	var (
		env       *helpers.TestEnv
		dir1      *node.Node
		file      *node.Node
		fileRef   *provider.Reference
		victimDir string
		victim    string
	)

	BeforeEach(func() {
		var err error
		env, err = helpers.NewTestEnv(map[string]any{"watch_fs": false})
		Expect(err).ToNot(HaveOccurred())

		dir1, err = env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/dir1",
		})
		Expect(err).ToNot(HaveOccurred())

		file, err = env.CreateTestFile("own-file", "own-blobid", dir1.ID, dir1.SpaceID, 0)
		Expect(err).ToNot(HaveOccurred())
		fileRef = &provider.Reference{ResourceId: env.SpaceRootRes, Path: "/dir1/own-file"}

		victimDir = GinkgoT().TempDir()
		victim = filepath.Join(victimDir, "victim.txt")
		Expect(os.WriteFile(victim, []byte("precious"), 0600)).To(Succeed())

		// grant everything, the id checks alone have to stop the requests
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Unset()
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:                 true,
			InitiateFileUpload:   true,
			InitiateFileDownload: true,
			ListFileVersions:     true,
			RestoreFileVersion:   true,
		}, nil)
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	climb := func() string {
		return strings.Repeat("../", 40) + strings.TrimPrefix(victimDir, "/")
	}

	forgedRef := func(nodeID, name string) *provider.Reference {
		return &provider.Reference{
			ResourceId: &provider.ResourceId{
				StorageId: env.SpaceRootRes.StorageId,
				SpaceId:   env.SpaceRootRes.SpaceId,
				OpaqueId:  nodeID,
			},
			Path: "./" + name,
		}
	}

	expectVictimUntouched := func() {
		content, err := os.ReadFile(victim)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("precious"))
	}

	DescribeTable("TouchFile does not truncate or create files outside the space",
		func(suffix func() string) {
			for _, name := range []string{"victim.txt", "new.txt"} {
				err := env.Fs.TouchFile(env.Ctx, forgedRef(dir1.ID+suffix(), name), false, "")
				Expect(err).To(HaveOccurred())
			}

			expectVictimUntouched()
			Expect(filepath.Join(victimDir, "new.txt")).ToNot(BeAnExistingFile())
		},
		Entry("revision suffix", func() string { return node.RevisionIDDelimiter + "x/" + climb() }),
		Entry("timestamp revision suffix", func() string {
			return node.RevisionIDDelimiter + "2024-01-01T00:00:00Z/" + climb()
		}),
	)

	It("does not stat a container outside the space", func() {
		ref := forgedRef(dir1.ID+node.RevisionIDDelimiter+"x/"+climb(), "")
		ref.Path = ""
		_, err := env.Fs.GetMD(env.Ctx, ref, nil, nil)
		Expect(err).To(HaveOccurred())
	})

	Describe("revision keys", func() {
		var key string

		BeforeEach(func() {
			key = file.ID + node.RevisionIDDelimiter + "x/" + climb() + "/victim.txt"
		})

		It("RestoreRevision refuses the key", func() {
			err := env.Fs.RestoreRevision(env.Ctx, fileRef, key)
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
			expectVictimUntouched()
		})

		It("DeleteRevision refuses the key", func() {
			err := env.Fs.DeleteRevision(env.Ctx, fileRef, key)
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
			expectVictimUntouched()
		})

		It("DownloadRevision refuses the key", func() {
			_, reader, err := env.Fs.DownloadRevision(env.Ctx, fileRef, key, func(*provider.ResourceInfo) bool { return true })
			if reader != nil {
				_, _ = io.Copy(io.Discard, reader)
				_ = reader.Close()
			}
			Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
		})
	})
})
