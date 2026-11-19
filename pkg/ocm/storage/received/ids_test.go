// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocm_test

import (
	"context"
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	ocmpb "github.com/cs3org/go-cs3apis/cs3/sharing/ocm/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"
	"google.golang.org/grpc"

	ctxpkg "github.com/opencloud-eu/reva/v3/pkg/ctx"
	ocm "github.com/opencloud-eu/reva/v3/pkg/ocm/storage/received"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
	cs3mocks "github.com/opencloud-eu/reva/v3/tests/cs3mocks/mocks"
)

// newReceivedShareFS returns the received share driver for a share that is served by an
// in-memory webdav server holding /folder/file.txt.
func newReceivedShareFS(t *testing.T) (storage.FS, *ocmpb.ReceivedShare, context.Context) {
	t.Helper()
	const gatewaySVC = "ocmreceived-ids-test-gateway"
	ctx := context.Background()

	davFS := webdav.NewMemFS()
	require.NoError(t, davFS.Mkdir(ctx, "/folder", 0o755))
	f, err := davFS.OpenFile(ctx, "/folder/file.txt", os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	dav := &webdav.Handler{FileSystem: davFS, LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// gowebdav asks for all properties with an unprefixed <allprop/>, which OpenCloud
		// accepts but x/net/webdav rejects
		if r.Method == "PROPFIND" {
			body, _ := io.ReadAll(r.Body)
			body = bytes.ReplaceAll(body, []byte("<allprop/>"), []byte("<d:allprop/>"))
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		dav.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	share := &ocmpb.ReceivedShare{
		Id:      &ocmpb.ShareId{OpaqueId: "share-id"},
		Name:    "folder",
		Creator: &userpb.UserId{OpaqueId: "einstein@https://remote.example.org", Idp: "remote.example.org", Type: userpb.UserType_USER_TYPE_FEDERATED},
		Protocols: []*ocmpb.Protocol{{Term: &ocmpb.Protocol_WebdavOptions{WebdavOptions: &ocmpb.WebDAVProtocol{
			Uri:          srv.URL,
			SharedSecret: "secret",
			Permissions:  &ocmpb.SharePermissions{Permissions: &provider.ResourcePermissions{Stat: true}},
		}}}},
	}

	gw := &cs3mocks.GatewayAPIClient{}
	gw.On("Authenticate", mock.Anything, mock.Anything).
		Return(&gateway.AuthenticateResponse{Status: status.NewOK(ctx), Token: "service-token"}, nil)
	gw.On("GetReceivedOCMShare", mock.Anything, mock.Anything).
		Return(&ocmpb.GetReceivedOCMShareResponse{Status: status.NewOK(ctx), Share: share}, nil)
	pool.RemoveSelector("GatewaySelector" + gatewaySVC)
	pool.GetSelector("GatewaySelector", gatewaySVC, func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
		return gw
	})

	fs, err := ocm.New(map[string]any{"gatewaysvc": gatewaySVC}, nil, nil)
	require.NoError(t, err)

	ctx = ctxpkg.ContextSetUser(ctx, &userpb.User{Id: &userpb.UserId{OpaqueId: "marie"}})
	ctx = ctxpkg.ContextSetToken(ctx, "user-token")
	return fs, share, ctx
}

func rootRef(p string) *provider.Reference {
	return &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: "share-id", OpaqueId: "share-id"}, Path: p}
}

func opaqueID(p string) string {
	return base64.StdEncoding.EncodeToString([]byte(p))
}

func TestGetMDReturnsTheIDOfNestedResources(t *testing.T) {
	fs, _, ctx := newReceivedShareFS(t)

	for p, want := range map[string]string{
		".":                  "/",
		"./folder":           "/folder",
		"./folder/file.txt":  "/folder/file.txt",
		"folder/../folder/.": "/folder",
	} {
		ri, err := fs.GetMD(ctx, rootRef(p), nil, nil)
		require.NoError(t, err, p)
		require.Equal(t, utils.OCMStorageProviderID, ri.GetId().GetStorageId(), p)
		require.Equal(t, "share-id", ri.GetId().GetSpaceId(), p)
		require.Equal(t, opaqueID(want), ri.GetId().GetOpaqueId(), p)
	}

	// the id has to resolve to the same resource again
	ri, err := fs.GetMD(ctx, rootRef("./folder/file.txt"), nil, nil)
	require.NoError(t, err)
	again, err := fs.GetMD(ctx, &provider.Reference{ResourceId: ri.GetId()}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "file.txt", again.GetName())
	require.Equal(t, ri.GetId().GetOpaqueId(), again.GetId().GetOpaqueId())
}

func TestListFolderReturnsTheIDOfNestedResources(t *testing.T) {
	fs, _, ctx := newReceivedShareFS(t)

	list, err := fs.ListFolder(ctx, rootRef("./folder"), nil, nil)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, opaqueID("/folder/file.txt"), list[0].GetId().GetOpaqueId())
}

func TestTouchFileReportsTheNodeLikeAStat(t *testing.T) {
	fs, share, ctx := newReceivedShareFS(t)

	res, err := fs.TouchFile(ctx, rootRef("./folder/new.txt"), false, "")
	require.NoError(t, err)
	require.Equal(t, "share-id", res.SpaceID)
	require.Equal(t, share.GetCreator(), res.SpaceOwner)

	ri, err := fs.GetMD(ctx, rootRef("./folder/new.txt"), nil, nil)
	require.NoError(t, err)
	require.Equal(t, ri.GetId().GetStorageId(), res.ResourceID.GetStorageId())
	require.Equal(t, ri.GetId().GetSpaceId(), res.ResourceID.GetSpaceId())
	require.Equal(t, ri.GetId().GetOpaqueId(), res.ResourceID.GetOpaqueId())
	require.Equal(t, opaqueID("/folder/new.txt"), res.ResourceID.GetOpaqueId())
}
