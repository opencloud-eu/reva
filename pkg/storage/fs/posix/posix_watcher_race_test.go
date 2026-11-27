// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package posix_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/pkg/xattr"
	"github.com/stretchr/testify/mock"

	"github.com/opencloud-eu/reva/v3/pkg/storage"
	posixblobstore "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/blobstore"
	posixhelpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// https://github.com/opencloud-eu/reva/issues/1062
var _ = Describe("new uploads with the fs watcher", Label("watcher-race"), func() {
	var env *posixhelpers.TestEnv

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(map[string]any{
			"watch_fs":            true,
			"scan_debounce_delay": time.Duration(0),
		})
		Expect(err).ToNot(HaveOccurred())

		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything).Return(
			&provider.ResourcePermissions{Stat: true, InitiateFileUpload: true}, nil)

		bs, err := posixblobstore.New(env.Root)
		Expect(err).ToNot(HaveOccurred())
		env.Blobstore.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(bs.Upload).Maybe()
		env.Blobstore.On("UploadFromReader", mock.Anything, mock.Anything, mock.Anything).Return(bs.UploadFromReader).Maybe()

		// wait until the watcher is up: inotifywait only sees files created after it set up its
		// watches, so keep creating probes until one of them is assimilated
		var probes []string
		Eventually(func() bool {
			for _, p := range probes {
				if _, err := xattr.Get(p, prefixes.IDAttr); err == nil {
					return true
				}
			}
			p := filepath.Join(env.Root, "users", env.Owner.Username, fmt.Sprintf("probe-%d.txt", len(probes)))
			Expect(os.WriteFile(p, []byte("probe"), 0600)).To(Succeed())
			probes = append(probes, p)
			return false
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).Should(BeTrue())
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("keeps the content, the id and the caches of every new file", func() {
		const (
			files   = 2000
			workers = 8
		)
		content := []byte("some data")

		type upload struct {
			id, name string
			ref      *provider.Reference
		}
		uploads := make([]upload, files)
		for i := range uploads {
			id := uuid.New().String()
			uploads[i] = upload{
				id:   id,
				name: fmt.Sprintf("race-%d.txt", i),
				ref:  &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: env.SpaceRootRes.SpaceId, OpaqueId: id}},
			}
		}

		var wg sync.WaitGroup
		work := make(chan upload)
		for range workers {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				for u := range work {
					session := "session-" + u.id
					_, err := env.Fs.PrepareUpload(env.Ctx, u.ref, session, storage.UploadInfo{
						NodeExisted: false,
						Size:        int64(len(content)),
						ParentID:    env.SpaceRootRes.OpaqueId,
						Name:        u.name,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(env.Fs.CommitUpload(env.Ctx, u.ref, session, storage.UploadSource{
						Body:   io.NopCloser(bytes.NewReader(content)),
						Length: int64(len(content)),
					})).To(Succeed())
					// what the coordinator does once postprocessing is done
					Expect(env.Fs.MarkProcessing(env.Ctx, u.ref, false, session)).To(Succeed())
				}
			}()
		}
		for _, u := range uploads {
			work <- u
		}
		close(work)
		wg.Wait()

		// let the watcher settle
		time.Sleep(2 * time.Second)

		var (
			emptyOnDisk, wrongBlobsize, wrongBlobID, wrongIDOnDisk, wrongReverseCache, staleByPath int
			examples                                                                               []string
		)
		for _, u := range uploads {
			path := filepath.Join(env.Root, "users", env.Owner.Username, u.name)
			var problems []string

			if b, _ := os.ReadFile(path); !bytes.Equal(b, content) {
				emptyOnDisk++
				problems = append(problems, fmt.Sprintf("content on disk %q", b))
			}
			if v, _ := xattr.Get(path, prefixes.BlobsizeAttr); string(v) != fmt.Sprint(len(content)) {
				wrongBlobsize++
				problems = append(problems, fmt.Sprintf("blobsize on disk %q", v))
			}
			if v, _ := xattr.Get(path, prefixes.BlobIDAttr); string(v) != "session-"+u.id {
				wrongBlobID++
				problems = append(problems, fmt.Sprintf("blobid on disk %q", v))
			}
			if v, _ := xattr.Get(path, prefixes.IDAttr); string(v) != u.id {
				wrongIDOnDisk++
				problems = append(problems, fmt.Sprintf("id on disk %q", v))
			}
			if _, id, err := env.Lookup.IDCache.GetByPath(env.Ctx, path); err != nil || id != u.id {
				wrongReverseCache++
				problems = append(problems, fmt.Sprintf("path resolves to id %q (%v)", id, err))
			}

			// what a GET by path sees
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./" + u.name})
			if err != nil || n.Blobsize != int64(len(content)) || n.IsProcessing(env.Ctx) {
				staleByPath++
				if n != nil {
					problems = append(problems, fmt.Sprintf("by path: id %q blobsize %d processing %t (%v)", n.ID, n.Blobsize, n.IsProcessing(env.Ctx), err))
				} else {
					problems = append(problems, fmt.Sprintf("by path: %v", err))
				}
			}

			if len(problems) > 0 && len(examples) < 10 {
				examples = append(examples, fmt.Sprintf("%s (id %s): %v", u.name, u.id, problems))
			}
		}

		GinkgoWriter.Printf("of %d uploads: content wrong on disk %d, blobsize wrong on disk %d, blobid wrong on disk %d, id wrong on disk %d, path resolves to another id %d, wrong by path %d\n",
			files, emptyOnDisk, wrongBlobsize, wrongBlobID, wrongIDOnDisk, wrongReverseCache, staleByPath)
		for _, e := range examples {
			GinkgoWriter.Println(e)
		}

		Expect(emptyOnDisk + wrongBlobsize + wrongBlobID + wrongIDOnDisk + wrongReverseCache + staleByPath).To(BeZero())
	})

	// TouchFile creates empty files for an empty PUT, the app provider and locks on unmapped
	// urls. It used to create the file before writing its id, the same window as above.
	It("keeps the id TouchFile reports for every new file", func() {
		const (
			files   = 2000
			workers = 8
		)
		ids := make([]string, files)

		var wg sync.WaitGroup
		work := make(chan int)
		for range workers {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				for i := range work {
					res, err := env.Fs.TouchFile(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: fmt.Sprintf("./touch-%d.txt", i)}, false, "")
					Expect(err).ToNot(HaveOccurred())
					ids[i] = res.ResourceID.GetOpaqueId()
				}
			}()
		}
		for i := range files {
			work <- i
		}
		close(work)
		wg.Wait()

		// let the watcher settle
		time.Sleep(2 * time.Second)

		var (
			wrongIDOnDisk, wrongReverseCache, wrongByPath int
			examples                                      []string
		)
		for i, id := range ids {
			name := fmt.Sprintf("touch-%d.txt", i)
			path := filepath.Join(env.Root, "users", env.Owner.Username, name)
			var problems []string

			if v, _ := xattr.Get(path, prefixes.IDAttr); string(v) != id {
				wrongIDOnDisk++
				problems = append(problems, fmt.Sprintf("id on disk %q", v))
			}
			if _, cached, err := env.Lookup.IDCache.GetByPath(env.Ctx, path); err != nil || cached != id {
				wrongReverseCache++
				problems = append(problems, fmt.Sprintf("path resolves to id %q (%v)", cached, err))
			}
			n, err := env.Lookup.NodeFromResource(env.Ctx, &provider.Reference{ResourceId: env.SpaceRootRes, Path: "./" + name})
			if err != nil || n.ID != id {
				wrongByPath++
				problems = append(problems, fmt.Sprintf("by path: %v (%v)", n, err))
			}

			if len(problems) > 0 && len(examples) < 10 {
				examples = append(examples, fmt.Sprintf("%s (id %s): %v", name, id, problems))
			}
		}

		GinkgoWriter.Printf("of %d touched files: id wrong on disk %d, path resolves to another id %d, wrong by path %d\n",
			files, wrongIDOnDisk, wrongReverseCache, wrongByPath)
		for _, e := range examples {
			GinkgoWriter.Println(e)
		}

		Expect(wrongIDOnDisk + wrongReverseCache + wrongByPath).To(BeZero())
	})
})
