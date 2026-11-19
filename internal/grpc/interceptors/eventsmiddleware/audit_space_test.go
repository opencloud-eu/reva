package eventsmiddleware

import (
	"testing"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/opencloud-eu/reva/v3/pkg/storagespace"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
)

func TestSpaceUpdateChanges(t *testing.T) {
	update := func(s *provider.StorageSpace) *audit.Change {
		s.Root = root
		ev, found := auditEvent(&provider.UpdateStorageSpaceRequest{StorageSpace: s}, &provider.UpdateStorageSpaceResponse{Status: ok}, &storagespace.ItemTypeSlot{Name: "Audit Test"})
		require.True(t, found)
		assert.Equal(t, audit.SpaceUpdate, ev.Definition)
		assert.Equal(t, "Audit Test", ev.Target.Name, "the name before the update")
		return ev.Change
	}

	assert.Equal(t, &audit.Change{After: map[string]any{"name": "Renamed"}}, update(&provider.StorageSpace{Name: "Renamed"}))

	o := utils.AppendPlainToOpaque(nil, "description", "Team files")
	o = utils.AppendPlainToOpaque(o, "readme", "storage$space!readme-id")
	assert.Equal(t, &audit.Change{After: map[string]any{"description": "Team files", "readme": "storage$space!readme-id"}}, update(&provider.StorageSpace{Opaque: o}))

	assert.Equal(t, &audit.Change{After: map[string]any{"description": ""}}, update(&provider.StorageSpace{Opaque: utils.AppendPlainToOpaque(nil, "description", "")}), "a removed description")
	assert.Nil(t, update(&provider.StorageSpace{}))
}

func TestTrashAndVersionPaths(t *testing.T) {
	item := &storagespace.ItemTypeSlot{Type: provider.ResourceType_RESOURCE_TYPE_FILE, ID: &provider.ResourceId{SpaceId: "space", OpaqueId: "item"}, Name: "report.pdf", Path: "/docs/report.pdf"}
	space := &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "space"}}

	ev, found := auditEvent(&provider.RestoreRecycleItemRequest{Ref: space, Key: "item"}, &provider.RestoreRecycleItemResponse{Status: ok}, item)
	require.True(t, found)
	assert.Equal(t, "./docs/report.pdf", ev.Target.Path, "the path before the deletion")

	ev, found = auditEvent(&provider.PurgeRecycleRequest{Ref: space, Key: "item"}, &provider.PurgeRecycleResponse{Status: ok}, item)
	require.True(t, found)
	assert.Equal(t, "./docs/report.pdf", ev.Target.Path)

	byID := &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "item"}}
	ev, found = auditEvent(&provider.RestoreFileVersionRequest{Ref: byID, Key: "item.REV.1"}, &provider.RestoreFileVersionResponse{Status: ok}, item)
	require.True(t, found)
	assert.Equal(t, "./docs/report.pdf", ev.Target.Path, "a request by ID gets the path from the driver")

	byPath := &provider.Reference{ResourceId: space.ResourceId, Path: "./docs/report.pdf"}
	ev, found = auditEvent(&provider.RestoreFileVersionRequest{Ref: byPath, Key: "item.REV.1"}, &provider.RestoreFileVersionResponse{Status: ok}, &storagespace.ItemTypeSlot{Type: item.Type, ID: item.ID})
	require.True(t, found)
	assert.Equal(t, "./docs/report.pdf", ev.Target.Path, "a request from the space root has the path, the driver doesn't look it up")
}

func TestPathsThroughALink(t *testing.T) {
	// a link on /linkact: requests start at the shared folder
	linkRoot := &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "linkact"}
	src := &provider.Reference{ResourceId: linkRoot, Path: "./sub/a.txt"}
	item := &storagespace.ItemTypeSlot{Type: provider.ResourceType_RESOURCE_TYPE_FILE, ID: &provider.ResourceId{SpaceId: "space", OpaqueId: "a"}, Name: "a.txt", Path: "/linkact/sub/a.txt"}

	ev, found := auditEvent(&provider.DeleteRequest{Ref: src}, &provider.DeleteResponse{Status: ok}, item)
	require.True(t, found)
	assert.Equal(t, "./linkact/sub/a.txt", ev.Target.Path, "the path in the space, not in the link")

	ev, found = auditEvent(&provider.DeleteRequest{Ref: src}, &provider.DeleteResponse{Status: ok}, &storagespace.ItemTypeSlot{Type: item.Type})
	require.True(t, found)
	assert.Empty(t, ev.Target.Path, "without the driver the path in the space is unknown")

	for _, tc := range []struct {
		name          string
		dst           *provider.Reference
		before, after string
	}{
		{"rename", &provider.Reference{ResourceId: linkRoot, Path: "./sub/b.txt"}, "./linkact/sub/a.txt", "./linkact/sub/b.txt"},
		{"move within the link", &provider.Reference{ResourceId: linkRoot, Path: "./other/a.txt"}, "./linkact/sub/a.txt", "./linkact/other/a.txt"},
		{"move to a path from the space root", &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "space"}, Path: "./x/a.txt"}, "./linkact/sub/a.txt", "./x/a.txt"},
		{"move relative to another node", &provider.Reference{ResourceId: &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "x"}, Path: "./a.txt"}, "./linkact/sub/a.txt", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, found := auditEvent(&provider.MoveRequest{Source: src, Destination: tc.dst}, &provider.MoveResponse{Status: ok}, item)
			require.True(t, found)
			assert.Equal(t, "./linkact/sub/a.txt", ev.Target.Path)
			assert.Equal(t, tc.before, ev.Change.Before["path"])
			if tc.after == "" {
				assert.Nil(t, ev.Change.After, "an unknown path is left out")
				return
			}
			assert.Equal(t, tc.after, ev.Change.After["path"])
		})
	}
}

func TestSpaceDisableHasTheSpaceID(t *testing.T) {
	for _, purge := range []bool{false, true} {
		req := &provider.DeleteStorageSpaceRequest{Id: &provider.StorageSpaceId{OpaqueId: "storage$space!space"}}
		if purge {
			req.Opaque = utils.AppendPlainToOpaque(nil, "purge", "")
		}
		ev, found := auditEvent(req, &provider.DeleteStorageSpaceResponse{Status: ok}, &storagespace.ItemTypeSlot{Name: "Team"})
		require.True(t, found)
		assert.Equal(t, "storage$space", ev.Target.SpaceID)
		assert.Equal(t, "Team", ev.Target.Name)
	}
}
