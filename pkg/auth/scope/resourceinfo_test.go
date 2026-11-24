// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package scope

import (
	"context"
	"testing"

	appprovider "github.com/cs3org/go-cs3apis/cs3/app/provider/v1beta1"
	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	registry "github.com/cs3org/go-cs3apis/cs3/storage/registry/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/opencloud-eu/reva/v3/pkg/utils"
)

var (
	scopedID = &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "file-1"}
	otherID  = &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "file-2"}
)

func scopedInfo() *provider.ResourceInfo {
	return &provider.ResourceInfo{
		Id:   scopedID,
		Path: "/Documents/report",
	}
}

func TestCheckResourceInfo(t *testing.T) {
	tests := []struct {
		name string
		ref  *provider.Reference
		want bool
	}{
		{"id only", &provider.Reference{ResourceId: scopedID}, true},
		{"id and dot", &provider.Reference{ResourceId: scopedID, Path: "."}, true},
		{"id and relative path below", &provider.Reference{ResourceId: scopedID, Path: "./ch1.md"}, true},
		{"id and relative path below without dot", &provider.Reference{ResourceId: scopedID, Path: "sub/ch1.md"}, true},
		{"id and relative path staying inside", &provider.Reference{ResourceId: scopedID, Path: "./a/../b"}, true},
		{"id and parent", &provider.Reference{ResourceId: scopedID, Path: ".."}, false},
		{"id and relative path leaving", &provider.Reference{ResourceId: scopedID, Path: "./a/../../report-salaries.xlsx"}, false},
		{"id and sibling path", &provider.Reference{ResourceId: scopedID, Path: "../report-salaries.xlsx"}, false},
		{"id and absolute path", &provider.Reference{ResourceId: scopedID, Path: "/Documents/report"}, false},
		{"id and absolute sibling path", &provider.Reference{ResourceId: scopedID, Path: "/Documents/report-salaries.xlsx"}, false},
		{"id without storage id", &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: "space-1", OpaqueId: "file-1"}}, true},
		{"other id", &provider.Reference{ResourceId: otherID}, false},
		{"other space", &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-2", OpaqueId: "file-1"}}, false},
		{"id without space id", &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage-1", OpaqueId: "file-1"}}, false},
		{"empty id", &provider.Reference{ResourceId: &provider.ResourceId{}}, false},
		{"path only, exact", &provider.Reference{Path: "/Documents/report"}, false},
		{"path only, below", &provider.Reference{Path: "/Documents/report/ch1.md"}, false},
		{"path only, sibling sharing the prefix", &provider.Reference{Path: "/Documents/report-salaries.xlsx"}, false},
		{"path only, relative", &provider.Reference{Path: "."}, false},
		{"empty reference", &provider.Reference{}, false},
		{"nil reference", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, checkResourceInfo(scopedInfo(), tt.ref))
		})
	}
}

func TestCheckResourceInfoWithIncompleteScope(t *testing.T) {
	ref := &provider.Reference{ResourceId: &provider.ResourceId{}, Path: "."}
	for name, inf := range map[string]*provider.ResourceInfo{
		"no id":        {Path: "/Documents/report"},
		"empty id":     {Id: &provider.ResourceId{}},
		"no opaque id": {Id: &provider.ResourceId{SpaceId: "space-1"}},
		"no space id":  {Id: &provider.ResourceId{OpaqueId: "file-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, checkResourceInfo(inf, ref))
			assert.False(t, checkResourceInfo(inf, &provider.Reference{Path: "/anything"}))
		})
	}
}

func TestResourceInfoScope(t *testing.T) {
	viewer, err := AddResourceInfoScope(scopedInfo(), authpb.Role_ROLE_VIEWER, nil)
	require.NoError(t, err)
	editor, err := AddResourceInfoScope(scopedInfo(), authpb.Role_ROLE_EDITOR, nil)
	require.NoError(t, err)

	idRef := &provider.Reference{ResourceId: scopedID, Path: "."}
	siblingPathRef := &provider.Reference{Path: "/Documents/report-salaries.xlsx"}

	tests := []struct {
		name       string
		req        interface{}
		wantViewer bool
		wantEditor bool
	}{
		{"stat by id", &provider.StatRequest{Ref: idRef}, true, true},
		{"stat by sibling path", &provider.StatRequest{Ref: siblingPathRef}, false, false},
		{"stat by own path", &provider.StatRequest{Ref: &provider.Reference{Path: "/Documents/report"}}, false, false},
		{"stat other id", &provider.StatRequest{Ref: &provider.Reference{ResourceId: otherID}}, false, false},
		{"list container by sibling path", &provider.ListContainerRequest{Ref: siblingPathRef}, false, false},
		{"download by id", &provider.InitiateFileDownloadRequest{Ref: idRef}, true, true},
		{"download by sibling path", &provider.InitiateFileDownloadRequest{Ref: siblingPathRef}, false, false},
		{"get storage providers by id", &registry.GetStorageProvidersRequest{Ref: idRef}, true, true},
		{"get storage providers by path", &registry.GetStorageProvidersRequest{Ref: siblingPathRef}, false, false},
		{"open in app", &appprovider.OpenInAppRequest{ResourceInfo: &provider.ResourceInfo{Id: scopedID}}, true, true},
		{"open other in app", &appprovider.OpenInAppRequest{ResourceInfo: &provider.ResourceInfo{Id: otherID}}, false, false},
		{"upload by id", &provider.InitiateFileUploadRequest{Ref: idRef}, false, true},
		{"upload by sibling path", &provider.InitiateFileUploadRequest{Ref: siblingPathRef}, false, false},
		{"delete by sibling path", &provider.DeleteRequest{Ref: siblingPathRef}, false, false},
		{"move within", &provider.MoveRequest{
			Source:      &provider.Reference{ResourceId: scopedID, Path: "./a"},
			Destination: &provider.Reference{ResourceId: scopedID, Path: "./b"},
		}, false, true},
		{"move out", &provider.MoveRequest{
			Source:      &provider.Reference{ResourceId: scopedID, Path: "./a"},
			Destination: siblingPathRef,
		}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyScope(context.Background(), viewer, tt.req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantViewer, got, "viewer")

			got, err = VerifyScope(context.Background(), editor, tt.req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEditor, got, "editor")
		})
	}
}

func TestListStorageProvidersScope(t *testing.T) {
	viewer, err := AddResourceInfoScope(scopedInfo(), authpb.Role_ROLE_VIEWER, nil)
	require.NoError(t, err)

	tests := []struct {
		name    string
		filters map[string]string
		want    bool
	}{
		{"by id", map[string]string{"storage_id": "storage-1", "space_id": "space-1", "opaque_id": "file-1", "path": ""}, true},
		{"by id and dot", map[string]string{"storage_id": "storage-1", "space_id": "space-1", "opaque_id": "file-1", "path": "."}, true},
		{"by id and parent", map[string]string{"storage_id": "storage-1", "space_id": "space-1", "opaque_id": "file-1", "path": ".."}, false},
		{"by id without space id", map[string]string{"storage_id": "storage-1", "opaque_id": "file-1", "path": ""}, false},
		{"by other id", map[string]string{"storage_id": "storage-1", "space_id": "space-1", "opaque_id": "file-2", "path": ""}, false},
		{"by id in other space", map[string]string{"storage_id": "storage-1", "space_id": "space-2", "opaque_id": "file-1", "path": ""}, false},
		{"by own path", map[string]string{"path": "/Documents/report"}, false},
		{"by sibling path", map[string]string{"path": "/Documents/report-salaries.xlsx"}, false},
		{"without filters", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyScope(context.Background(), viewer, listStorageProvidersRequest(tt.filters))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func listStorageProvidersRequest(filters map[string]string) *registry.ListStorageProvidersRequest {
	m := map[string]*types.OpaqueEntry{}
	for k, v := range filters {
		m[k] = &types.OpaqueEntry{Decoder: "plain", Value: []byte(v)}
	}
	return &registry.ListStorageProvidersRequest{Opaque: &types.Opaque{Map: m}}
}

func TestAddResourceInfoScope(t *testing.T) {
	t.Run("stores only the id", func(t *testing.T) {
		scopes, err := AddResourceInfoScope(scopedInfo(), authpb.Role_ROLE_VIEWER, nil)
		require.NoError(t, err)
		require.Len(t, scopes, 1)

		s, ok := scopes["resourceinfo:"+scopedID.String()]
		require.True(t, ok)
		assert.Equal(t, authpb.Role_ROLE_VIEWER, s.GetRole())

		var inf provider.ResourceInfo
		require.NoError(t, utils.UnmarshalJSONToProtoV1(s.GetResource().GetValue(), &inf))
		assert.True(t, proto.Equal(scopedID, inf.GetId()))
		assert.Empty(t, inf.GetPath())
	})

	t.Run("adds to the given scopes", func(t *testing.T) {
		scopes := map[string]*authpb.Scope{"user": {}}
		scopes, err := AddResourceInfoScope(scopedInfo(), authpb.Role_ROLE_VIEWER, scopes)
		require.NoError(t, err)
		assert.Len(t, scopes, 2)
	})

	for name, r := range map[string]*provider.ResourceInfo{
		"nil resource info": nil,
		"no id":             {Path: "/Documents/report"},
		"empty id":          {Id: &provider.ResourceId{}, Path: "/Documents/report"},
		"no space id":       {Id: &provider.ResourceId{StorageId: "storage-1", OpaqueId: "file-1"}},
		"no opaque id":      {Id: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1"}},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			scopes := map[string]*authpb.Scope{"user": {}}
			got, err := AddResourceInfoScope(r, authpb.Role_ROLE_VIEWER, scopes)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Len(t, scopes, 1)
		})
	}
}

// TestResourceInfoScopeWithPath checks scopes created by older versions, which
// also stored the path of the resource.
func TestResourceInfoScopeWithPath(t *testing.T) {
	val, err := utils.MarshalProtoV1ToJSON(scopedInfo())
	require.NoError(t, err)
	scopes := map[string]*authpb.Scope{
		"resourceinfo:" + scopedID.String(): {
			Resource: &types.OpaqueEntry{Decoder: "json", Value: val},
			Role:     authpb.Role_ROLE_VIEWER,
		},
	}

	tests := []struct {
		name string
		req  interface{}
		want bool
	}{
		{"stat by id", &provider.StatRequest{Ref: &provider.Reference{ResourceId: scopedID, Path: "."}}, true},
		{"stat by own path", &provider.StatRequest{Ref: &provider.Reference{Path: "/Documents/report"}}, false},
		{"stat by sibling path", &provider.StatRequest{Ref: &provider.Reference{Path: "/Documents/report-salaries.xlsx"}}, false},
		{"http data path", "/data/sometoken", true},
		{"http webdav path", "/remote.php/dav/files/alice", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyScope(context.Background(), scopes, tt.req)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
