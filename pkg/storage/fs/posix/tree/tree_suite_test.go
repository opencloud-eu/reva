package tree_test

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
)

var (
	env              *helpers.TestEnv
	non_watching_env *helpers.TestEnv

	root string
)

var _ = SynchronizedBeforeSuite(func() {
	var err error
	env, err = helpers.NewTestEnv(map[string]any{
		"watch_fs":                true,
		"scan_fs":                 true,
		"inotify_stats_frequency": 1 * time.Second,
	})
	Expect(err).ToNot(HaveOccurred())

	// Set up environment with FS watching disabled
	non_watching_env, err = helpers.NewTestEnv(map[string]any{"watch_fs": false})
	Expect(err).ToNot(HaveOccurred())
}, func() {})

var _ = SynchronizedAfterSuite(func() {}, func() {
	if env != nil {
		env.Cleanup()
	}
})

func TestTree(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tree Suite")
}
