package ignore_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/ignore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	decomposedoptions "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree/propagator"
	"github.com/rs/zerolog"
)

var _ = Describe("Ignore", func() {
	var (
		logger  = zerolog.Nop()
		ignorer = ignore.NewIgnorer(&options.Options{
			Options: decomposedoptions.Options{
				Root:                      "/storage",
				PersonalSpacePathTemplate: "users/{{.User.Id.OpaqueId}}",
				GeneralSpacePathTemplate:  "spaces/{{.Space.Id.OpaqueId}}",
				UploadDirectory:           "/storage/uploads",
			},
		}, &logger)
	)
	DescribeTable("IsIgnored",
		func(path string, want bool) {
			Expect(ignorer.IsIgnored(path)).To(Equal(want))
		},
		Entry("matches index paths", filepath.Join("/storage", lookup.IndexesDir, "something"), true),
		Entry("matches changes paths", filepath.Join("/storage", propagator.ChangesDir, "node1"), true),
		Entry("matches upload directory", filepath.Join("/storage", "uploads", "upload.bin"), true),
		Entry("matches metadata dir", filepath.Join("/storage", "users", "user1", lookup.MetadataDir), true),
		Entry("matches metadata dir child", filepath.Join("/storage", "users", "user1", lookup.MetadataDir, "a"), true),
		Entry("matches metadata dir deep child", filepath.Join("/storage", "users", "user1", lookup.MetadataDir, "a", "b"), true),
		Entry("matches temporary dir", filepath.Join("/storage", "users", "user1", blobstore.TMPDir), true),
		Entry("matches temporary child", filepath.Join("/storage", "users", "user1", blobstore.TMPDir, "a"), true),
		Entry("matches temporary deep child", filepath.Join("/storage", "users", "user1", blobstore.TMPDir, "a", "b"), true),
		Entry("matches trash paths", filepath.Join("/storage", "users", "user1", lookup.TrashDir), true),
		Entry("matches trash child", filepath.Join("/storage", "users", "user1", lookup.TrashDir, "a"), true),
		Entry("matches trash deep child", filepath.Join("/storage", "users", "user1", lookup.TrashDir, "a", "b"), true),

		Entry("does not match internal paths in other locations", filepath.Join("/storage", "users", "user1", "a", lookup.MetadataDir), false),
		Entry("does not match regular user content in space root", filepath.Join("/storage", "users", "user1", "a.txt"), false),
		Entry("does not match regular user content", filepath.Join("/storage", "users", "user1", "documents", "a.txt"), false),
		Entry("does not match temporary dir in other locations", filepath.Join("/storage", "users", "user1", "a", blobstore.TMPDir), false),
		Entry("does not match trash dir in other locations", filepath.Join("/storage", "users", "user1", "a", lookup.TrashDir), false),
		Entry("does not match trash dir in other locations", filepath.Join("/storage", "users", "user1", "a", "foo.trashitem"), false),
		Entry("does not match trash dir in other locations", filepath.Join("/storage", "users", "user1", "a", "foo.trashinfo"), false),
	)

})
