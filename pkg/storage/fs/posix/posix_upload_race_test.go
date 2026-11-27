// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package posix_test

import (
	"fmt"
	"path/filepath"
	"sync"

	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
	posixhelpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/pkg/xattr"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Uploads new files while other requests list the parent. A listing that saw the new file
// before its id was written took it for an unknown item and gave it another id, which then
// replaced the upload's id on disk and in the id cache. That happened in about one of five
// rounds. Now whoever sets the id first owns the file. When it is the listing, the upload
// fails because the name is taken, and the file keeps the listing's id.
var _ = Describe("Uploading a new file while its parent is listed", func() {
	const (
		rounds  = 300
		listers = 4
	)

	var env *posixhelpers.TestEnv

	BeforeEach(func() {
		var err error
		env, err = posixhelpers.NewTestEnv(nil)
		Expect(err).ToNot(HaveOccurred())

		env.PermissionsClient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(
			&cs3permissions.CheckPermissionResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}}, nil)
		env.Permissions.On("AssemblePermissions", mock.Anything, mock.Anything, mock.Anything).Return(
			&provider.ResourcePermissions{
				Stat: true, ListContainer: true, CreateContainer: true, InitiateFileUpload: true,
			}, nil)
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("gives a new file a single id", func() {
		resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "race", Type: "project"})
		Expect(err).ToNot(HaveOccurred())
		root := resp.StorageSpace.GetRoot()
		ref := func(path string) *provider.Reference {
			return &provider.Reference{ResourceId: root, Path: path}
		}

		lost, listedOtherID, otherIDOnDisk, otherIDCached := 0, 0, 0, 0
		for i := range rounds {
			parentRef := ref(fmt.Sprintf("./p%d", i))
			Expect(env.Fs.CreateDir(env.Ctx, parentRef)).Error().To(Succeed())
			parent, err := env.Lookup.NodeFromResource(env.Ctx, parentRef)
			Expect(err).ToNot(HaveOccurred())
			id := uuid.NewString()

			var (
				mu     sync.Mutex
				listed = map[string]bool{}
				wg     sync.WaitGroup
			)
			stop := make(chan struct{})
			for range listers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-stop:
							return
						default:
							infos, _ := env.Fs.ListFolder(env.Ctx, parentRef, nil, nil)
							mu.Lock()
							for _, info := range infos {
								listed[info.GetId().GetOpaqueId()] = true
							}
							mu.Unlock()
						}
					}
				}()
			}

			newFile := &provider.Reference{ResourceId: &provider.ResourceId{
				StorageId: root.GetStorageId(), SpaceId: root.GetSpaceId(), OpaqueId: id,
			}}
			_, err = env.Fs.PrepareUpload(env.Ctx, newFile, "session-"+id, storage.UploadInfo{ParentID: parent.ID, Name: "file"})
			close(stop)
			wg.Wait()

			if _, ok := err.(errtypes.IsAlreadyExists); ok {
				// a listing gave the file its id first: the file must carry that one only
				lost++
				_, id, err = env.Lookup.IDsForPath(env.Ctx, filepath.Join(parent.InternalPath(), "file"))
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}

			delete(listed, id)
			if len(listed) > 0 {
				listedOtherID++
			}
			path := filepath.Join(parent.InternalPath(), "file")
			if idOnDisk, _ := xattr.Get(path, prefixes.IDAttr); string(idOnDisk) != id {
				otherIDOnDisk++
			}
			if _, cached, _ := env.Lookup.IDsForPath(env.Ctx, path); cached != id {
				otherIDCached++
			}
		}

		GinkgoWriter.Printf("rounds=%d lost to a listing=%d listed with another id=%d another id on disk=%d another id cached=%d\n",
			rounds, lost, listedOtherID, otherIDOnDisk, otherIDCached)
		Expect(listedOtherID).To(BeZero())
		Expect(otherIDOnDisk).To(BeZero())
		Expect(otherIDCached).To(BeZero())
	})
})
