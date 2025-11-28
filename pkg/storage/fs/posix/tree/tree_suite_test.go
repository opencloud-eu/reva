package tree_test

import (
	"log"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/shirou/gopsutil/process"
)

var (
	env              *helpers.TestEnv
	non_watching_env *helpers.TestEnv

	root string
)

var _ = SynchronizedBeforeSuite(func() {
	var err error
	env, err = helpers.NewTestEnv(map[string]any{
		"watch_fs": true,
		"scan_fs":  true,
	})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() bool {
		// Get all running processes
		processes, err := process.Processes()
		if err != nil {
			panic("could not get processes: " + err.Error())
		}

		// Search for the process named "inotifywait"
		for _, p := range processes {
			name, err := p.Name()
			if err != nil {
				log.Println(err)
				continue
			}

			if strings.Contains(name, "inotifywait") {
				// Give it some time to setup the watches
				time.Sleep(2 * time.Second)
				return true
			}
		}
		return false
	}).Should(BeTrue())

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
