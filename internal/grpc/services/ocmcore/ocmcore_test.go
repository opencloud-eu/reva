// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocmcore_test

import (
	"context"
	"path/filepath"
	"testing"

	appprovider "github.com/cs3org/go-cs3apis/cs3/app/provider/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	ocmcorepb "github.com/cs3org/go-cs3apis/cs3/ocm/core/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	ocm "github.com/cs3org/go-cs3apis/cs3/sharing/ocm/v1beta1"
	providerpb "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typespb "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	microevents "go-micro.dev/v4/events"

	"github.com/opencloud-eu/reva/v3/internal/grpc/services/ocmcore"
	"github.com/opencloud-eu/reva/v3/pkg/events"
	ocmshare "github.com/opencloud-eu/reva/v3/pkg/ocm/share"
	jsonrepo "github.com/opencloud-eu/reva/v3/pkg/ocm/share/repository/json"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
)

func TestUpdateOCMCoreShareTakesOnlyWebDAVPermissions(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "shares.json")
	grantee := &userpb.UserId{Idp: "cloud.example.com", OpaqueId: "alice", Type: userpb.UserType_USER_TYPE_PRIMARY}

	read := &ocm.SharePermissions{Permissions: &providerpb.ResourcePermissions{InitiateFileDownload: true}}
	write := &ocm.SharePermissions{Permissions: &providerpb.ResourcePermissions{InitiateFileDownload: true, InitiateFileUpload: true}}

	log := zerolog.Nop()
	svc, err := ocmcore.New(map[string]any{
		"driver":  "json",
		"drivers": map[string]any{"json": map[string]any{"file": file}},
	}, nil, &log)
	require.NoError(t, err)
	core := svc.(ocmcorepb.OcmCoreAPIServer)

	created, err := core.CreateOCMCoreShare(ctx, &ocmcorepb.CreateOCMCoreShareRequest{
		ResourceId:   "remote-resource",
		Name:         "project",
		ShareWith:    grantee,
		ResourceType: providerpb.ResourceType_RESOURCE_TYPE_CONTAINER,
		ShareType:    ocm.ShareType_SHARE_TYPE_USER,
		Owner:        &userpb.UserId{Idp: "remote.example.org", OpaqueId: "bob"},
		Sender:       &userpb.UserId{Idp: "remote.example.org", OpaqueId: "bob"},
		Protocols: []*ocm.Protocol{
			ocmshare.NewWebDAVProtocol("https://remote.example.org/dav/ocm/token", "secret", read),
			ocmshare.NewWebappProtocol("https://remote.example.org/app/{token}", appprovider.ViewMode_VIEW_MODE_READ_ONLY),
		},
	})
	require.NoError(t, err)
	require.Equal(t, rpc.Code_CODE_OK, created.GetStatus().GetCode())

	// the notification carries a full protocol set, only its webdav permissions may land
	o := &typespb.Opaque{}
	utils.AppendPlainToOpaque(o, "grantee", "alice@cloud.example.com")
	updated, err := core.UpdateOCMCoreShare(ctx, &ocmcorepb.UpdateOCMCoreShareRequest{
		OcmShareId: created.GetId(),
		Protocols: []*ocm.Protocol{
			ocmshare.NewWebDAVProtocol("https://attacker.example.net/dav", "other-secret", write),
		},
		Opaque: o,
	})
	require.NoError(t, err)
	require.Equal(t, rpc.Code_CODE_OK, updated.GetStatus().GetCode())

	repo, err := jsonrepo.New(map[string]any{"file": file})
	require.NoError(t, err)
	rs, err := repo.GetReceivedShare(ctx, &userpb.User{Id: grantee}, &ocm.ShareReference{
		Spec: &ocm.ShareReference_Id{Id: &ocm.ShareId{OpaqueId: created.GetId()}},
	})
	require.NoError(t, err)
	require.Len(t, rs.GetProtocols(), 2)

	webdav := rs.GetProtocols()[0].GetWebdavOptions()
	require.Equal(t, "https://remote.example.org/dav/ocm/token", webdav.GetUri())
	require.Equal(t, "secret", webdav.GetSharedSecret())
	require.True(t, webdav.GetPermissions().GetPermissions().GetInitiateFileUpload())
	require.Equal(t, "https://remote.example.org/app/{token}", rs.GetProtocols()[1].GetWebappOptions().GetUri())
}

type recordingStream struct {
	published []any
}

func (r *recordingStream) Publish(_ string, ev any, _ ...microevents.PublishOption) error {
	r.published = append(r.published, ev)
	return nil
}

func (r *recordingStream) Consume(string, ...microevents.ConsumeOption) (<-chan microevents.Event, error) {
	return nil, nil
}

func TestDeleteOCMCoreShareEventNamesTheCreatorAsExecutant(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "shares.json")
	grantee := &userpb.UserId{Idp: "cloud.example.com", OpaqueId: "alice", Type: userpb.UserType_USER_TYPE_PRIMARY}
	owner := &userpb.UserId{Idp: "remote.example.org", OpaqueId: "space-owner"}
	sender := &userpb.UserId{Idp: "remote.example.org", OpaqueId: "bob"}

	log := zerolog.Nop()
	svc, err := ocmcore.New(map[string]any{
		"driver":  "json",
		"drivers": map[string]any{"json": map[string]any{"file": file}},
	}, nil, &log)
	require.NoError(t, err)
	stream := &recordingStream{}
	ocmcore.SetEventStream(svc, stream)
	core := svc.(ocmcorepb.OcmCoreAPIServer)

	created, err := core.CreateOCMCoreShare(ctx, &ocmcorepb.CreateOCMCoreShareRequest{
		ResourceId:   "remote-resource",
		Name:         "project",
		ShareWith:    grantee,
		ResourceType: providerpb.ResourceType_RESOURCE_TYPE_CONTAINER,
		ShareType:    ocm.ShareType_SHARE_TYPE_USER,
		Owner:        owner,
		Sender:       sender,
		Protocols: []*ocm.Protocol{
			ocmshare.NewWebDAVProtocol("https://remote.example.org/dav/ocm/token", "secret", &ocm.SharePermissions{Permissions: &providerpb.ResourcePermissions{InitiateFileDownload: true}}),
		},
	})
	require.NoError(t, err)
	require.Equal(t, rpc.Code_CODE_OK, created.GetStatus().GetCode())

	o := &typespb.Opaque{}
	utils.AppendPlainToOpaque(o, "grantee", "alice@cloud.example.com")
	deleted, err := core.DeleteOCMCoreShare(ctx, &ocmcorepb.DeleteOCMCoreShareRequest{Id: created.GetId(), Opaque: o})
	require.NoError(t, err)
	require.Equal(t, rpc.Code_CODE_OK, deleted.GetStatus().GetCode())

	require.Len(t, stream.published, 2)
	ev, ok := stream.published[1].(events.OCMCoreShareDelete)
	require.True(t, ok)
	require.True(t, utils.UserEqual(sender, ev.Executant), "executant should be the share creator, got %v", ev.Executant)
	require.True(t, utils.UserEqual(owner, ev.Sharer), "sharer should stay the share owner, got %v", ev.Sharer)
}
