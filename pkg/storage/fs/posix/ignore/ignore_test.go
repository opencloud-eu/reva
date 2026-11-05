package ignore_test

import (
	"os"
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
		Entry("matches purged personal space child", filepath.Join("/storage", "users", lookup.PurgeDirPrefix+"user1-x", "a"), true),
		Entry("matches purged project space child", filepath.Join("/storage", "spaces", lookup.PurgeDirPrefix+"space1-x", "a", "b"), true),
		Entry("does not match purge prefix deeper in a space", filepath.Join("/storage", "users", "user1", lookup.PurgeDirPrefix+"a"), false),
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

		Entry("does not match metadata substring matches", filepath.Join("/storage", "users", "user1", "a"+lookup.MetadataDir), false),
		Entry("does not match temporary substring matches", filepath.Join("/storage", "users", "user1", "a"+blobstore.TMPDir), false),
		Entry("does not match trash substring matches", filepath.Join("/storage", "users", "user1", "a"+lookup.TrashDir), false),
		Entry("does not match metadata substring matches", filepath.Join("/storage", "users", "user1", lookup.MetadataDir+"a"), false),
		Entry("does not match temporary substring matches", filepath.Join("/storage", "users", "user1", blobstore.TMPDir+"a"), false),
		Entry("does not match trash substring matches", filepath.Join("/storage", "users", "user1", lookup.TrashDir+"a"), false),
	)

	Describe("with space path templates that start with an action", func() {
		var templated = ignore.NewIgnorer(&options.Options{
			Options: decomposedoptions.Options{
				Root:                      "/storage",
				PersonalSpacePathTemplate: "personal/{{.User.Username}}",
				GeneralSpacePathTemplate:  "{{.SpaceType}}/{{.SpaceName}}/{{.SpaceId}}",
				UploadDirectory:           "/storage/uploads",
			},
		}, &logger)

		DescribeTable("IsIgnored",
			func(path string, want bool) {
				Expect(templated.IsIgnored(path)).To(Equal(want))
			},
			Entry("matches the directory above the spaces", "/storage/project", true),
			Entry("matches a directory between the root and the spaces", "/storage/project/Team", true),
			Entry("matches a space root", "/storage/project/Team/id1", true),
			Entry("matches a personal space root", "/storage/personal/alice", true),
			Entry("matches trash in a space", "/storage/project/Team/id1/"+lookup.TrashDir+"/a", true),
			Entry("matches metadata in a space", "/storage/project/Team/id1/"+lookup.MetadataDir, true),
			Entry("matches temporary files in a space", "/storage/personal/alice/"+blobstore.TMPDir+"/a", true),
			Entry("does not match user content in a space", "/storage/project/Team/id1/a.txt", false),
			Entry("does not match a trash dir below a space root", "/storage/project/Team/id1/a/"+lookup.TrashDir, false),
		)

		DescribeTable("IsRootPath and IsSpaceRoot",
			func(path string, rootPath, spaceRoot bool) {
				Expect(templated.IsRootPath(path)).To(Equal(rootPath))
				Expect(templated.IsSpaceRoot(path)).To(Equal(spaceRoot))
			},
			Entry("storage root", "/storage", true, false),
			Entry("space type directory", "/storage/project", true, false),
			Entry("space name directory", "/storage/project/Team", true, false),
			Entry("project space root", "/storage/project/Team/id1", false, true),
			Entry("personal space root", "/storage/personal/alice", false, true),
			Entry("space content", "/storage/project/Team/id1/a", false, false),
			Entry("indexes", filepath.Join("/storage", lookup.IndexesDir), false, false),
			Entry("index content", filepath.Join("/storage", lookup.IndexesDir, "by-type", "a"), false, false),
			Entry("uploads", "/storage/uploads/a/b", false, false),
		)
	})

	Describe("with templates that overlap or have text around an action", func() {
		newIgnorer := func(root, personal, general string) *ignore.Ignorer {
			return ignore.NewIgnorer(&options.Options{
				Options: decomposedoptions.Options{
					Root:                      root,
					PersonalSpacePathTemplate: personal,
					GeneralSpacePathTemplate:  general,
					UploadDirectory:           filepath.Join(root, "uploads"),
				},
			}, &logger)
		}

		It("does not treat content of a space as a space root of another template", func() {
			i := newIgnorer("/storage", "users/{{.User.Id.OpaqueId}}", "{{.SpaceType}}/{{.SpaceName}}/{{.SpaceId}}")
			Expect(i.IsSpaceRoot("/storage/users/alice")).To(BeTrue())
			Expect(i.IsIgnored("/storage/users/alice/report.txt")).To(BeFalse())
			Expect(i.IsIgnored("/storage/users/alice/docs/report.txt")).To(BeFalse())
			Expect(i.IsSpaceRoot("/storage/project/Team/id1")).To(BeTrue())
		})

		It("prefers the template with more literal segments", func() {
			i := newIgnorer("/storage", "users/{{.User.Id.OpaqueId}}", "{{.SpaceId}}")
			Expect(i.IsRootPath("/storage/users")).To(BeTrue())
			Expect(i.IsSpaceRoot("/storage/users")).To(BeFalse())
			Expect(i.IsSpaceRoot("/storage/users/alice")).To(BeTrue())
			Expect(i.IsIgnored("/storage/users/alice/" + lookup.TrashDir)).To(BeTrue())
			Expect(i.IsSpaceRoot("/storage/id1")).To(BeTrue())
			Expect(i.IsIgnored("/storage/id1/a.txt")).To(BeFalse())
		})

		It("ignores the internal directories of a space with text after the action", func() {
			i := newIgnorer("/storage", "users/{{.User.Id.OpaqueId}}", "projects/{{.SpaceId}}-data")
			Expect(i.IsSpaceRoot("/storage/projects/id1-data")).To(BeTrue())
			Expect(i.IsIgnored("/storage/projects/id1-data/" + lookup.TrashDir)).To(BeTrue())
			Expect(i.IsIgnored("/storage/projects/id1-data/" + lookup.MetadataDir + "/a")).To(BeTrue())
		})

		It("works with a relative root", func() {
			i := newIgnorer(".", "users/{{.User.Id.OpaqueId}}", "projects/{{.SpaceId}}")
			Expect(i.IsRootPath("users")).To(BeTrue())
			Expect(i.IsSpaceRoot("users/alice")).To(BeTrue())
			Expect(i.IsIgnored("users/alice/" + lookup.TrashDir)).To(BeTrue())
			Expect(i.IsIgnored("users/alice/a.txt")).To(BeFalse())
		})

		It("matches a template with a condition", func() {
			i := newIgnorer("/storage", "users/{{if .User.Mail}}mail-{{else}}user-{{end}}{{.User.Id.OpaqueId}}", "projects/{{.SpaceId}}")
			Expect(i.IsSpaceRoot("/storage/users/mail-alice")).To(BeTrue())
			Expect(i.IsIgnored("/storage/users/user-bob/" + lookup.TrashDir)).To(BeTrue())
		})

		It("finds the space roots on disk", func() {
			root := GinkgoT().TempDir()
			for _, dir := range []string{"users/alice", "project/Team/id1", "project/Other/id2", lookup.IndexesDir + "/by-type/x"} {
				Expect(os.MkdirAll(filepath.Join(root, dir), 0o700)).To(Succeed())
			}
			Expect(os.WriteFile(filepath.Join(root, ".DS_Store"), nil, 0o600)).To(Succeed())
			i := newIgnorer(root, "users/{{.User.Id.OpaqueId}}", "{{.SpaceType}}/{{.SpaceName}}/{{.SpaceId}}")
			Expect(i.SpaceRoots()).To(ConsistOf(
				filepath.Join(root, "users/alice"),
				filepath.Join(root, "project/Team/id1"),
				filepath.Join(root, "project/Other/id2"),
			))

			if os.Geteuid() == 0 {
				return
			}
			// only a wildcard matches project, so it is skipped
			Expect(os.Chmod(filepath.Join(root, "project"), 0o000)).To(Succeed())
			DeferCleanup(os.Chmod, filepath.Join(root, "project"), os.FileMode(0o700))
			Expect(i.SpaceRoots()).To(ConsistOf(filepath.Join(root, "users/alice")))

			// the template names users, so it must be readable
			Expect(os.Chmod(filepath.Join(root, "users"), 0o000)).To(Succeed())
			DeferCleanup(os.Chmod, filepath.Join(root, "users"), os.FileMode(0o700))
			_, err := i.SpaceRoots()
			Expect(err).To(HaveOccurred())
		})
	})
})
