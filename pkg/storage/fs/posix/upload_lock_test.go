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
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/blobstore"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/idcache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/lookup"
	posixhelpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/tree"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/usermapper"
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
	env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
		&provider.ResourcePermissions{Stat: true, ListContainer: true, CreateContainer: true,
			InitiateFileUpload: true, InitiateFileDownload: true, Delete: true, Move: true, GetPath: true, AddGrant: true}, nil)
	bs, err := blobstore.New(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	env.Blobstore.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(bs.Upload).Maybe()
	env.Blobstore.On("UploadFromReader", mock.Anything, mock.Anything, mock.Anything).Return(bs.UploadFromReader).Maybe()
	env.Blobstore.EXPECT().Delete(mock.Anything).RunAndReturn(bs.Delete).Maybe()
	return env
}

func uploadWithColdCache(env *posixhelpers.TestEnv, p string, content []byte, cold bool, ifMatch ...string) error {
	ref := &provider.Reference{ResourceId: env.SpaceRootRes, Path: p}
	info := storage.UploadInfo{Size: int64(len(content))}
	if len(ifMatch) != 0 {
		info.IfMatch = ifMatch[0]
	}
	n, err := env.Lookup.NodeFromResource(env.Ctx, ref)
	if err != nil {
		return err
	}
	if n.Exists {
		info.NodeExisted = true
	} else {
		// PrepareUpload creates a new file under a placeholder id
		ref = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: env.SpaceRootRes.SpaceId, OpaqueId: uuid.NewString()}}
		info.ParentID, info.Name = env.SpaceRootRes.OpaqueId, filepath.Base(p)
	}
	sessionID := uuid.NewString()
	if _, err := env.Fs.PrepareUpload(env.Ctx, ref, sessionID, info); err != nil {
		return err
	}
	if cold {
		// Model expiry or eviction between prepare and commit, without relying on its timing.
		if err := cache.GetFileMetadataCache(env.Options.FileMetadataCache).RemoveMetadata(n.SpaceID + "/" + n.ID); err != nil {
			return err
		}
	}
	if err := env.Fs.CommitUpload(env.Ctx, ref, sessionID, storage.UploadSource{
		Body: io.NopCloser(bytes.NewReader(content)), Length: int64(len(content))}); err != nil {
		return err
	}
	// The upload coordinator clears the marker after a commit.
	return env.Fs.MarkProcessing(env.Ctx, ref, false, sessionID)
}

func TestUploadWithColdMetadataCache(t *testing.T) {
	env := newUploadLockEnv(t)
	if err := uploadWithColdCache(env, "./probe.txt", []byte("before"), false); err != nil {
		t.Fatal(err)
	}
	probe, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./probe.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// Expire the entry over and over, so it also expires while an upload holds the node lock.
	stop := make(chan struct{})
	evicted := make(chan struct{})
	go func() {
		defer close(evicted)
		c := cache.GetFileMetadataCache(env.Options.FileMetadataCache)
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.RemoveMetadata(probe.SpaceID + "/" + probe.ID)
				runtime.Gosched()
			}
		}
	}()
	content := []byte("after the metadata cache entry expires")
	done := make(chan error, 1)
	go func() {
		// A rejected overwrite must release the same lock as a successful one.
		err := uploadWithColdCache(env, "./probe.txt", content, true, "wrong-etag")
		if _, ok := err.(errtypes.IsAborted); !ok {
			done <- fmt.Errorf("wrong etag returned %v, want an aborted upload", err)
			return
		}
		for range 50 {
			if err := uploadWithColdCache(env, "./probe.txt", content, true); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		close(stop)
		<-evicted
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
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
						_, err = env.Fs.Delete(env.Ctx, ref(p))
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
