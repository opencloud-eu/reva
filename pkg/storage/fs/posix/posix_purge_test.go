package posix_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Purging a space", func() {
	const (
		rounds  = 10
		dirs    = 10
		files   = 20
		workers = 8
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

	It("succeeds while other requests keep locking nodes of the space", func() {
		for round := range rounds {
			resp, err := env.Fs.CreateStorageSpace(env.Ctx, &provider.CreateStorageSpaceRequest{Name: fmt.Sprintf("purge-%d", round), Type: "project"})
			Expect(err).ToNot(HaveOccurred())
			spaceID := resp.StorageSpace.GetId()
			root := resp.StorageSpace.GetRoot()
			spaceRoot := env.Lookup.InternalSpaceRoot(root.GetSpaceId())
			Expect(spaceRoot).To(BeADirectory())

			ref := func(path string) *provider.Reference {
				return &provider.Reference{ResourceId: root, Path: path}
			}
			for d := range dirs {
				Expect(env.Fs.CreateDir(env.Ctx, ref(fmt.Sprintf("./d%d", d)))).To(Succeed())
				for f := range files {
					Expect(env.Fs.TouchFile(env.Ctx, ref(fmt.Sprintf("./d%d/f%d", d, f)), false, "")).To(Succeed())
				}
			}

			// requests that resolved their nodes before the purge and lock them while it runs
			var nodes []*node.Node
			for d := range dirs {
				for f := range files {
					n, err := env.Lookup.NodeFromResource(env.Ctx, ref(fmt.Sprintf("./d%d/f%d", d, f)))
					Expect(err).ToNot(HaveOccurred())
					nodes = append(nodes, n)
				}
			}
			ctx, stop := context.WithCancel(env.Ctx)
			var wg sync.WaitGroup
			for w := range workers {
				wg.Go(func() {
					for i := w; ctx.Err() == nil; i += workers {
						if unlock, err := env.Lookup.MetadataBackend().Lock(nodes[i%len(nodes)]); err == nil {
							_ = unlock()
						}
					}
				})
			}

			Expect(env.Fs.DeleteStorageSpace(env.Ctx, &provider.DeleteStorageSpaceRequest{Id: spaceID})).To(Succeed())
			err = env.Fs.DeleteStorageSpace(env.Ctx, &provider.DeleteStorageSpaceRequest{
				Id: spaceID,
				Opaque: &typesv1beta1.Opaque{Map: map[string]*typesv1beta1.OpaqueEntry{
					"purge": {Decoder: "plain", Value: []byte("true")},
				}},
			})
			stop()
			wg.Wait()

			Expect(err).ToNot(HaveOccurred(), "round %d", round)
			var left []string
			_ = filepath.Walk(spaceRoot, func(p string, _ os.FileInfo, _ error) error { left = append(left, p); return nil })
			Expect(spaceRoot).ToNot(BeAnExistingFile(), "round %d: %v", round, left)
			// the purge also removes the projects dir once it is empty
			entries, err := os.ReadDir(filepath.Dir(spaceRoot))
			if !os.IsNotExist(err) {
				Expect(err).ToNot(HaveOccurred())
			}
			for _, e := range entries {
				Expect(strings.HasPrefix(e.Name(), lookup.PurgeDirPrefix)).To(BeFalse(), "tombstone %s left behind", e.Name())
			}
		}
	})
})
