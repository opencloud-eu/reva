// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package scope

import (
	"context"
	"testing"

	authpb "github.com/cs3org/go-cs3apis/cs3/auth/provider/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	registry "github.com/cs3org/go-cs3apis/cs3/storage/registry/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var scopedID = &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "file-1"}

func scopedInfo() *provider.ResourceInfo {
	return &provider.ResourceInfo{
		Id:   scopedID,
		Path: "/Documents/report",
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
		{"by id without space id", map[string]string{"storage_id": "storage-1", "opaque_id": "file-1", "path": ""}, false},
		{"by other id", map[string]string{"storage_id": "storage-1", "space_id": "space-1", "opaque_id": "file-2", "path": ""}, false},
		{"by id in other space", map[string]string{"storage_id": "storage-1", "space_id": "space-2", "opaque_id": "file-1", "path": ""}, false},
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
