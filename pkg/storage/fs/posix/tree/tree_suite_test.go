package tree_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
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
	// The watcher might end up not picking up changes, e.g. because inotifywait silently skips directories whose
	// entries vanish while it is setting up its recursive watches, which can happen when it starts while the
	// environment is still being set up. Make sure we have a working watcher before running the specs.
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		var err error
		env, err = helpers.NewTestEnv(map[string]any{
			"watch_fs": true,
			"scan_fs":  true,
		})
		Expect(err).ToNot(HaveOccurred())

		err = waitForWatcher(env)
		if err == nil {
			break
		}
		if attempt == maxAttempts {
			Fail("the fs watcher does not pick up changes: " + err.Error())
		}
		GinkgoWriter.Printf("the fs watcher does not pick up changes (%s), setting up a new environment\n", err)
		env.Cleanup()
	}

	// Set up environment with FS watching disabled
	var err error
	non_watching_env, err = helpers.NewTestEnv(map[string]any{"watch_fs": false})
	Expect(err).ToNot(HaveOccurred())
}, func() {})

var _ = SynchronizedAfterSuite(func() {}, func() {
	if env != nil {
		env.Cleanup()
	}
})

// waitForWatcher waits until the watcher of the given environment picks up a directory created in the personal space
func waitForWatcher(env *helpers.TestEnv) error {
	deadline := time.Now().Add(10 * time.Second)
	// the watches might not have been established when creating the first directories, so keep creating new ones
	for i := 0; time.Now().Before(deadline); i++ {
		probe := fmt.Sprintf("/watcher-probe-%d", i)
		if err := os.Mkdir(env.Root+"/users/"+env.Owner.Username+probe, 0700); err != nil {
			return err
		}
		for range 10 {
			time.Sleep(100 * time.Millisecond)
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
				ResourceId: env.SpaceRootRes,
				Path:       probe,
			})
			if err == nil && n.Exists {
				return nil
			}
		}
	}
	return errors.New("no directory has been assimilated within 10s")
}

func TestTree(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tree Suite")
}
