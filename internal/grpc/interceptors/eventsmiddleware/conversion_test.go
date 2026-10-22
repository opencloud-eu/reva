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

func TestArbitraryMetadataChanged(t *testing.T) {
	spaceOwner := &user.UserId{OpaqueId: "owner-1"}
	executant := &user.User{Id: &user.UserId{OpaqueId: "user-1"}}
	ref := &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage-1", SpaceId: "space-1", OpaqueId: "node-1"}}

	t.Run("emits the reported keys sorted", func(t *testing.T) {
		ev, ok := ArbitraryMetadataChanged(ref, &storage.MetadataResult{ChangedKeys: []string{"foo/b", "foo/a"}, RemovedKeys: []string{"bar/b", "bar/a"}}, spaceOwner, executant)
		require.True(t, ok)
		require.Equal(t, spaceOwner, ev.SpaceOwner)
		require.Equal(t, executant.GetId(), ev.Executant)
		require.Equal(t, ref, ev.Ref)
		require.Equal(t, []string{"foo/a", "foo/b"}, ev.ChangedKeys)
		require.Equal(t, []string{"bar/a", "bar/b"}, ev.RemovedKeys)
		require.NotNil(t, ev.Timestamp)
	})

	t.Run("emits nothing when no key changed", func(t *testing.T) {
		_, ok := ArbitraryMetadataChanged(ref, &storage.MetadataResult{}, spaceOwner, executant)
		require.False(t, ok)
	})
}
