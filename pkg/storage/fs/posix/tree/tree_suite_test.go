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
	helpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/shirou/gopsutil/process"
)

// watchTimeoutEnv names the environment variable that sets watchTimeout, as a Go
// duration such as "1m". Raise it when the specs time out on a busy machine.
const watchTimeoutEnv = "REVA_POSIX_WATCH_TEST_TIMEOUT"

var (
	env              *helpers.TestEnv
	non_watching_env *helpers.TestEnv

	// watchTimeout is how long the specs wait for the watcher to pick up a change on
	// disk.
	watchTimeout = 15 * time.Second

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
	}).WithTimeout(watchTimeout).Should(BeTrue())

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
	}).WithTimeout(watchTimeout).WithPolling(200 * time.Millisecond).Should(Succeed())

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
	if v, ok := os.LookupEnv(watchTimeoutEnv); ok {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			t.Fatalf("%s=%q is not a positive duration such as \"1m\"", watchTimeoutEnv, v)
		}
		watchTimeout = d
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Tree Suite")
}
