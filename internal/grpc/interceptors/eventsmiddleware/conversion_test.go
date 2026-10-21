package eventsmiddleware

import (
	"testing"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/stretchr/testify/require"
)

func TestItemMoved(t *testing.T) {
	spaceOwner := &user.UserId{OpaqueId: "owner-1"}
	executant := &user.User{Id: &user.UserId{OpaqueId: "user-1"}}
	newRef := &provider.Reference{
		ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "node-1"},
		Path:       "./new-name.txt",
	}
	oldRef := &provider.Reference{
		ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "node-1"},
		Path:       "./old-name.txt",
	}

	result := &storage.MoveResult{
		SpaceOwner:   spaceOwner,
		NewReference: newRef,
		OldReference: oldRef,
	}
	res := &provider.MoveResponse{Status: &rpc.Status{Code: rpc.Code_CODE_OK}}
	req := &provider.MoveRequest{Source: oldRef, Destination: newRef}

	ev := ItemMoved(res, req, result, executant)

	require.Equal(t, spaceOwner, ev.SpaceOwner)
	require.Equal(t, executant.GetId(), ev.Executant)
	require.Equal(t, newRef, ev.Ref)
	require.Equal(t, oldRef, ev.OldReference)
	require.NotNil(t, ev.Timestamp)
}

func TestArbitraryMetadataSet(t *testing.T) {
	spaceOwner := &user.UserId{OpaqueId: "owner-1"}
	executant := &user.User{Id: &user.UserId{OpaqueId: "user-1"}}
	ref := &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "node-1"}}
	req := &provider.SetArbitraryMetadataRequest{
		Ref:               ref,
		ArbitraryMetadata: &provider.ArbitraryMetadata{Metadata: map[string]string{"foo/b": "1", "foo/a": "2"}},
	}

	t.Run("falls back to the sorted request keys without a storage result", func(t *testing.T) {
		ev, ok := ArbitraryMetadataSet(req, nil, spaceOwner, executant)
		require.True(t, ok)
		require.Equal(t, spaceOwner, ev.SpaceOwner)
		require.Equal(t, executant.GetId(), ev.Executant)
		require.Equal(t, ref, ev.Ref)
		require.Equal(t, []string{"foo/a", "foo/b"}, ev.ChangedKeys)
		require.Empty(t, ev.RemovedKeys)
		require.NotNil(t, ev.Timestamp)
	})

	t.Run("prefers the keys the storage reports", func(t *testing.T) {
		ev, ok := ArbitraryMetadataSet(req, &storage.MetadataResult{ChangedKeys: []string{"foo/b"}}, spaceOwner, executant)
		require.True(t, ok)
		require.Equal(t, []string{"foo/b"}, ev.ChangedKeys)
	})

	t.Run("emits nothing when the storage reports no change", func(t *testing.T) {
		_, ok := ArbitraryMetadataSet(req, &storage.MetadataResult{}, spaceOwner, executant)
		require.False(t, ok)
	})
}

func TestArbitraryMetadataUnset(t *testing.T) {
	spaceOwner := &user.UserId{OpaqueId: "owner-1"}
	executant := &user.User{Id: &user.UserId{OpaqueId: "user-1"}}
	req := &provider.UnsetArbitraryMetadataRequest{
		Ref:                   &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "node-1"}},
		ArbitraryMetadataKeys: []string{"foo/b", "foo/a"},
	}

	t.Run("falls back to the sorted request keys without a storage result", func(t *testing.T) {
		ev, ok := ArbitraryMetadataUnset(req, nil, spaceOwner, executant)
		require.True(t, ok)
		require.Equal(t, []string{"foo/a", "foo/b"}, ev.RemovedKeys)
		require.Empty(t, ev.ChangedKeys)
	})

	t.Run("prefers the keys the storage reports", func(t *testing.T) {
		ev, ok := ArbitraryMetadataUnset(req, &storage.MetadataResult{RemovedKeys: []string{"foo/a"}}, spaceOwner, executant)
		require.True(t, ok)
		require.Equal(t, []string{"foo/a"}, ev.RemovedKeys)
	})

	t.Run("emits nothing when the storage reports no change", func(t *testing.T) {
		_, ok := ArbitraryMetadataUnset(req, &storage.MetadataResult{}, spaceOwner, executant)
		require.False(t, ok)
	})
}
