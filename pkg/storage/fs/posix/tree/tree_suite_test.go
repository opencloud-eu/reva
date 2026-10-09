package tree_test

import (
	"log"
	"os"
	"strings"
	"testing"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

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
				return true
			}
		}
		return false
	}).Should(BeTrue())

	// create a directory in the space root and wait for it to be assimilated
	Eventually(func(g Gomega) {
		probe, err := generateRandomString(10)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(os.Mkdir(env.Root+"/users/"+env.Owner.Username+"/"+probe, 0700)).To(Succeed())

		// give the event time to travel inotify -> debouncer -> scan queue
		time.Sleep(500 * time.Millisecond)

		n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
			ResourceId: env.SpaceRootRes,
			Path:       "/" + probe,
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(n.Exists).To(BeTrue())
	}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

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
