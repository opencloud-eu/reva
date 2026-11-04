package posix_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/idcache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/tree"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/usermapper"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
)

func newUploadLockEnv(t *testing.T) *posixhelpers.TestEnv {
	t.Helper()
	env, err := posixhelpers.NewTestEnv(map[string]interface{}{
		"metadata_backend":  "hybrid",
		"filemetadatacache": map[string]interface{}{"cache_store": "memory"},
		"watch_fs":          os.Getenv("REVA_UPLOAD_STRESS_WATCH") == "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Cleanup)
	// The hybrid test helper uses this cache config instead of FileMetadataCache.
	env.Options.FileMetadataCache = cache.Config{Database: env.Root}
	env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(
		&cs3permissions.CheckPermissionResponse{Status: &rpc.Status{Code: rpc.Code_CODE_OK}}, nil)
	env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(
		&provider.ResourcePermissions{Stat: true, ListContainer: true, CreateContainer: true,
			InitiateFileUpload: true, InitiateFileDownload: true, Delete: true, Move: true, GetPath: true, AddGrant: true}, nil)
	bs, err := blobstore.New(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	env.Blobstore.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(
		func(n *node.Node, source, target string) error { return bs.Upload(n, source, target) })
	env.Blobstore.On("Delete", mock.Anything).Return(func(n *node.Node) error { return bs.Delete(n) })
	return env
}

func uploadWithColdCache(env *posixhelpers.TestEnv, p string, content []byte, cold bool, preconditions ...map[string]string) error {
	ref := func(p string) *provider.Reference { return &provider.Reference{ResourceId: env.SpaceRootRes, Path: p} }
	var attrs map[string]string
	if len(preconditions) != 0 {
		attrs = preconditions[0]
	}
	ids, err := env.Fs.InitiateUpload(env.Ctx, ref(p), int64(len(content)), attrs)
	if err != nil {
		return err
	}
	if cold {
		n, err := env.Lookup.NodeFromResource(env.Ctx, ref(p))
		if err != nil {
			return err
		}
		// Model expiry or eviction between initiate and finish, without relying on its timing.
		if err := cache.GetFileMetadataCache(env.Options.FileMetadataCache).RemoveMetadata(n.SpaceID + "/" + n.ID); err != nil {
			return err
		}
	}
	_, err = env.Fs.Upload(env.Ctx, storage.UploadRequest{Ref: &provider.Reference{Path: "/" + ids["simple"]},
		Body: io.NopCloser(bytes.NewReader(content)), Length: int64(len(content))}, nil)
	return err
}

func TestUploadWithColdMetadataCache(t *testing.T) {
	env := newUploadLockEnv(t)
	if err := uploadWithColdCache(env, "./probe.txt", []byte("before"), false); err != nil {
		t.Fatal(err)
	}
	content := []byte("after the metadata cache entry expires")
	done := make(chan error, 1)
	go func() {
		// A rejected overwrite must release the same lock as a successful one.
		err := uploadWithColdCache(env, "./probe.txt", content, true, map[string]string{"if-match": "wrong-etag"})
		if _, ok := err.(errtypes.IsAborted); !ok {
			done <- fmt.Errorf("wrong etag returned %v, want an aborted upload", err)
			return
		}
		done <- uploadWithColdCache(env, "./probe.txt", content, true)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		stacks := make([]byte, 1<<20)
		t.Fatalf("overwrite blocked with a cold metadata cache:\n%s", stacks[:runtime.Stack(stacks, true)])
	}
	got, err := os.ReadFile(filepath.Join(env.Root, "users", env.Owner.Username, "probe.txt"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content = %q, error = %v", got, err)
	}
	n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes})
	if err != nil {
		t.Fatal(err)
	}
	// The fixture contains a 1234-byte file before this test adds probe.txt.
	if size, err := n.GetTreeSize(env.Ctx); err != nil || size != uint64(1234+len(content)) {
		t.Fatalf("tree size = %d, error = %v", size, err)
	}
}

func TestUploadScanStress(t *testing.T) {
	if os.Getenv("REVA_UPLOAD_STRESS") != "1" {
		t.Skip("set REVA_UPLOAD_STRESS=1 for the mixed upload/move/delete/scan diagnostic")
	}
	env := newUploadLockEnv(t)
	ref := func(p string) *provider.Reference { return &provider.Reference{ResourceId: env.SpaceRootRes, Path: p} }
	for i := range 3 {
		if err := uploadWithColdCache(env, fmt.Sprintf("./probe-%d.txt", i), []byte("initial"), false); err != nil {
			t.Fatal(err)
		}
	}
	// A scan process has its own metadata cache but shares node IDs and file locks.
	scanOptions := *env.Options
	scanOptions.WatchFS = false
	scanOptions.ScanFS = false
	scanOptions.FileMetadataCache = cache.Config{Store: "memory", Database: env.Root + "-scan"}
	log := zerolog.Nop()
	scanLookup, err := lookup.New(metadata.NewHybridBackend(1024, func(n metadata.MetadataNode) string {
		return filepath.Join(env.Lookup.InternalSpaceRoot(n.GetSpaceID()), lookup.MetadataDir)
	}, scanOptions.FileMetadataCache), &usermapper.NullMapper{}, &scanOptions, &timemanager.Manager{},
		env.Lookup.IDCache.(*idcache.IDCache), env.Lookup.IDHistoryCache.(*idcache.IDCache), &log)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := blobstore.New(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := tree.New(scanLookup, bs, &usermapper.NullMapper{}, nil, permissions.Permissions{}, &scanOptions, nil, env.Lookup.IDCache.(*idcache.IDCache), &log)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var completed [4]atomic.Int64
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			<-start
			for i := range 100 {
				p := fmt.Sprintf("./probe-%d.txt", (worker+i)%3)
				var err error
				switch worker {
				case 0, 1:
					err = uploadWithColdCache(env, p, []byte(fmt.Sprintf("worker %d round %d", worker, i)), false)
					if err == nil {
						completed[0].Add(1)
					}
				case 2:
					p = fmt.Sprintf("./move-%d.txt", i)
					if err = uploadWithColdCache(env, p, []byte("move me"), false); err == nil {
						_, err = env.Fs.Move(env.Ctx, ref(p), ref(p+".moved"))
						if err == nil {
							completed[1].Add(1)
						}
					}
				case 3:
					p = fmt.Sprintf("./delete-%d.txt", i)
					if err = uploadWithColdCache(env, p, []byte("delete me"), false); err == nil {
						err = env.Fs.Delete(env.Ctx, ref(p))
						if err == nil {
							completed[2].Add(1)
						}
					}
				}
				t.Logf("worker=%d round=%d err=%v", worker, i, err)
				time.Sleep(time.Millisecond)
			}
		})
	}
	wg.Go(func() {
		<-start
		for range 100 {
			if err := scanner.WarmupIDCache(filepath.Join(env.Root, "users"), true, false); err != nil {
				t.Logf("scan: %v", err)
			} else {
				completed[3].Add(1)
			}
			time.Sleep(time.Millisecond)
		}
	})
	close(start)
	wg.Wait()
	for i, name := range []string{"upload", "move", "delete", "scan"} {
		count := completed[i].Load()
		t.Logf("%s completed: %d", name, count)
		if count == 0 {
			t.Errorf("no successful %s operations", name)
		}
	}
}
