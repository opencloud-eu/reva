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
	posixhelpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"
	"github.com/pkg/xattr"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Creates directories while other requests list the parent. A listing that saw the new
// directory before its id was cached used to assimilate it and give it a second id, which
// happened about once in 300 rounds. Children then got a parent id that stopped resolving
// once the id cache was rebuilt.
var _ = Describe("Creating a directory while its parent is listed", func() {
	const (
		rounds  = 500
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
				InitiateFileDownload: true, Delete: true, Move: true, GetPath: true, AddGrant: true,
			}, nil)
	})

	AfterEach(func() {
		if env != nil {
			env.Cleanup()
		}
	})

	It("keeps a single id per directory", func() {
		resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: "race", Type: "project"})
		Expect(err).ToNot(HaveOccurred())
		root := resp.StorageSpace.GetRoot()
		spaceRoot := env.Lookup.InternalSpaceRoot(root.GetSpaceId())

		ref := func(path string) *provider.Reference {
			return &provider.Reference{ResourceId: root, Path: path}
		}
		diskAttr := func(path, attr string) string {
			v, err := xattr.Get(path, attr)
			Expect(err).ToNot(HaveOccurred())
			return string(v)
		}

		twoIDs := 0
		brokenParentIDs := 0
		for i := range rounds {
			parent := fmt.Sprintf("./p%d", i)
			Expect(env.Fs.CreateDir(env.Ctx, ref(parent))).Error().To(Succeed())

			stop := make(chan struct{})
			var wg sync.WaitGroup
			for range listers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-stop:
							return
						default:
							_, _ = env.Fs.ListFolder(env.Ctx, ref(parent), nil, nil)
						}
					}
				}()
			}

			Expect(env.Fs.CreateDir(env.Ctx, ref(parent+"/child"))).Error().To(Succeed())
			close(stop)
			wg.Wait()

			childPath := filepath.Join(spaceRoot, parent, "child")
			idOnDisk := diskAttr(childPath, "user.oc.id")
			n, err := env.Lookup.NodeFromResource(env.Ctx, ref(parent+"/child"))
			Expect(err).ToNot(HaveOccurred())
			if n.ID != idOnDisk {
				twoIDs++
			}

			// what a client does next: create something inside the new directory, by path
			Expect(env.Fs.CreateDir(env.Ctx, ref(parent+"/child/grandchild"))).Error().To(Succeed())
			parentID := diskAttr(filepath.Join(childPath, "grandchild"), "user.oc.parentid")
			if parentID != diskAttr(childPath, "user.oc.id") {
				brokenParentIDs++
			}
		}

		GinkgoWriter.Printf("rounds=%d directories with two ids=%d grandchildren with a parentid that is not the id on disk=%d\n",
			rounds, twoIDs, brokenParentIDs)
		Expect(twoIDs).To(BeZero())
		Expect(brokenParentIDs).To(BeZero())
	})
})
