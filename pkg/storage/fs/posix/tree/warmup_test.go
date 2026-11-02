package tree_test

import (
	"os"
	"path/filepath"
	"testing"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
)

// TestWarmupIDCacheBelowStorageRoot guards the startup warmup: called with the
// storage root, the walk must not skip the whole tree because the root itself
// is an ignored path. Files written directly to the user storage have no id
// yet; after the warmup they must have been assimilated.
func TestWarmupIDCacheBelowStorageRoot(t *testing.T) {
	env, err := helpers.NewTestEnv(map[string]interface{}{
		"watch_fs": false,
		"scan_fs":  false,
	})
	if err != nil {
		t.Fatalf("could not create test environment: %v", err)
	}
	defer env.Cleanup()

	userRoot := filepath.Join(env.Root, "users", "username")
	dir := filepath.Join(userRoot, "warmupdir")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("could not create directory on disk: %v", err)
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("hello world"), 0600); err != nil {
		t.Fatalf("could not write file on disk: %v", err)
	}

	if err := env.Tree.WarmupIDCache(env.Root, true, false); err != nil {
		t.Fatalf("warmup failed: %v", err)
	}

	n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{
		ResourceId: env.SpaceRootRes,
		Path:       "warmupdir/file.txt",
	})
	if err != nil {
		t.Fatalf("could not look up the file after the warmup: %v", err)
	}
	if n.ID == "" {
		t.Fatalf("the warmup did not assimilate %s below the storage root", file)
	}
}
