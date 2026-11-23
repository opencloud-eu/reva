// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package walker_test

import (
	"context"
	"errors"
	"path/filepath"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	"github.com/opencloud-eu/reva/v3/pkg/auth"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v3/pkg/storage/utils/walker"
	cs3mocks "github.com/opencloud-eu/reva/v3/tests/cs3mocks/mocks"
)

func container(id, path string) *provider.ResourceInfo {
	return &provider.ResourceInfo{
		Id:   &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: id},
		Path: path,
		Type: provider.ResourceType_RESOURCE_TYPE_CONTAINER,
	}
}

func file(id, path string) *provider.ResourceInfo {
	info := container(id, path)
	info.Type = provider.ResourceType_RESOURCE_TYPE_FILE
	return info
}

type visit struct {
	path   string
	failed bool
}

var _ = Describe("Walker", func() {
	var (
		gatewayClient *cs3mocks.GatewayAPIClient
		w             walker.Walker
		session       *auth.Session
		root          *provider.ResourceId
		listErr       error
		visits        []visit
	)

	BeforeEach(func() {
		pool.RemoveSelector("GatewaySelector" + "any")
		gatewayClient = cs3mocks.NewGatewayAPIClient(GinkgoT())
		gatewaySelector := pool.GetSelector[gateway.GatewayAPIClient](
			"GatewaySelector",
			"any",
			func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
				return gatewayClient
			},
		)
		w = walker.NewWalker(gatewaySelector)
		session = auth.NewStaticSession(context.Background())
		visits = nil
		listErr = nil

		// root
		// ├── broken/  (can't be listed)
		// ├── dir/
		// │   └── nested
		// └── top
		rootInfo := container("root", ".")
		root = rootInfo.Id
		children := map[string][]*provider.ResourceInfo{
			"root": {container("broken", "broken"), container("dir", "dir"), file("top", "top")},
			"dir":  {file("nested", "nested")},
		}

		gatewayClient.EXPECT().Stat(mock.Anything, mock.Anything).Return(&provider.StatResponse{
			Status: &rpc.Status{Code: rpc.Code_CODE_OK},
			Info:   rootInfo,
		}, nil)
		gatewayClient.EXPECT().ListContainer(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, req *provider.ListContainerRequest, _ ...grpc.CallOption) (*provider.ListContainerResponse, error) {
				id := req.GetRef().GetResourceId().GetOpaqueId()
				if id == "broken" {
					if listErr != nil {
						return nil, listErr
					}
					return &provider.ListContainerResponse{
						Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND, Message: "list container: error: not found: broken"},
					}, nil
				}
				return &provider.ListContainerResponse{
					Status: &rpc.Status{Code: rpc.Code_CODE_OK},
					Infos:  children[id],
				}, nil
			}).Maybe()
	})

	record := func(wd string, info *provider.ResourceInfo, err error) {
		visits = append(visits, visit{path: filepath.Join(wd, info.GetPath()), failed: err != nil})
	}

	It("walks the whole tree", func() {
		err := w.Walk(session, root, func(wd string, info *provider.ResourceInfo, err error) error {
			record(wd, info, err)
			if info.GetId().GetOpaqueId() == "broken" {
				return filepath.SkipDir
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(visits).To(Equal([]visit{
			{path: "."},
			{path: "broken"},
			{path: "dir"},
			{path: "dir/nested"},
			{path: "top"},
		}))
	})

	It("passes a listing error to the callback and continues when it returns nil", func() {
		var gotErr error
		err := w.Walk(session, root, func(wd string, info *provider.ResourceInfo, err error) error {
			record(wd, info, err)
			if err != nil {
				gotErr = err
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())

		var notFound errtypes.IsNotFound
		Expect(errors.As(gotErr, &notFound)).To(BeTrue())
		Expect(visits).To(Equal([]visit{
			{path: "."},
			{path: "broken"},
			{path: "broken", failed: true},
			{path: "dir"},
			{path: "dir/nested"},
			{path: "top"},
		}))
	})

	It("continues when the callback returns SkipDir for a listing error", func() {
		err := w.Walk(session, root, func(wd string, info *provider.ResourceInfo, err error) error {
			record(wd, info, err)
			if err != nil {
				return filepath.SkipDir
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(visits).To(ContainElements(visit{path: "dir/nested"}, visit{path: "top"}))
	})

	It("stops when the callback returns the listing error", func() {
		err := w.Walk(session, root, func(wd string, info *provider.ResourceInfo, err error) error {
			record(wd, info, err)
			return err
		})
		var notFound errtypes.IsNotFound
		Expect(errors.As(err, &notFound)).To(BeTrue())
		Expect(visits).To(Equal([]visit{
			{path: "."},
			{path: "broken"},
			{path: "broken", failed: true},
		}))
	})

	It("passes transport errors to the callback too", func() {
		listErr = errors.New("connection refused")
		var gotErr error
		err := w.Walk(session, root, func(wd string, info *provider.ResourceInfo, err error) error {
			if err != nil {
				gotErr = err
			}
			return err
		})
		Expect(err).To(MatchError("connection refused"))
		Expect(gotErr).To(MatchError("connection refused"))
	})
})
