package eventsmiddleware

import (
	"testing"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/opencloud-eu/reva/v3/pkg/storagespace"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var (
	ok     = &rpc.Status{Code: rpc.Code_CODE_OK}
	denied = &rpc.Status{Code: rpc.Code_CODE_PERMISSION_DENIED}
	rid    = &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "item"}
	root   = &provider.ResourceId{StorageId: "storage", SpaceId: "space", OpaqueId: "space"}
)

func opaque(key string) *types.Opaque {
	return utils.AppendPlainToOpaque(nil, key, "true")
}

func TestAuditEvent(t *testing.T) {
	ref := &provider.Reference{ResourceId: rid, Path: "./docs"}

	for _, tc := range []struct {
		name       string
		req, res   interface{}
		definition *audit.Definition
		outcome    audit.Outcome
		reason     string
		targetID   string
		targetName string
		itemType   provider.ResourceType
		// the item the driver reported, without the storage ID it doesn't know
		itemID   *provider.ResourceId
		itemName string
	}{
		{
			name:       "folder created",
			req:        &provider.CreateContainerRequest{Ref: ref},
			res:        &provider.CreateContainerResponse{Status: ok},
			definition: audit.FolderCreate, outcome: audit.OutcomeSuccess, targetID: "storage$space!item/docs",
		},
		{
			name:       "lock denied",
			req:        &provider.SetLockRequest{Ref: ref},
			res:        &provider.SetLockResponse{Status: denied},
			definition: audit.FileLock, outcome: audit.OutcomeDenied, reason: "permission_denied", targetID: "storage$space!item/docs",
		},
		{
			name:       "unlock not found",
			req:        &provider.UnlockRequest{Ref: ref},
			res:        &provider.UnlockResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}},
			definition: audit.FileUnlock, outcome: audit.OutcomeFailure, reason: "not_found", targetID: "storage$space!item/docs",
		},
		{
			name:       "trash bin purged",
			req:        &provider.PurgeRecycleRequest{Ref: &provider.Reference{ResourceId: root}},
			res:        &provider.PurgeRecycleResponse{Status: ok},
			definition: audit.TrashPurgeAll, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name: "space member added",
			req:  &collaboration.CreateShareRequest{ResourceInfo: &provider.ResourceInfo{Id: root}},
			res: &collaboration.CreateShareResponse{
				Status: ok, Opaque: opaque("spacegrant"),
				Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}},
			},
			definition: audit.SpaceMemberAdd, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name: "share created",
			req:  &collaboration.CreateShareRequest{ResourceInfo: &provider.ResourceInfo{Id: rid, Name: "report.pdf"}},
			res: &collaboration.CreateShareResponse{
				Status: ok,
				Share:  &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}},
			},
			definition: audit.ShareCreate, outcome: audit.OutcomeSuccess, targetID: "s-1",
		},
		{
			name: "share declined",
			req:  &collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state"}}},
			res: &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{
				State: collaboration.ShareState_SHARE_STATE_REJECTED,
				Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}, ResourceId: rid},
			}},
			definition: audit.ShareDecline, outcome: audit.OutcomeSuccess, targetID: "s-1",
		},
		{
			name: "share accepted",
			req:  &collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state", "mount_point"}}},
			res: &collaboration.UpdateReceivedShareResponse{Status: ok, Opaque: utils.AppendPlainToOpaque(nil, "previousstate", "SHARE_STATE_PENDING"), Share: &collaboration.ReceivedShare{
				State: collaboration.ShareState_SHARE_STATE_ACCEPTED,
				Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}, ResourceId: rid},
			}},
			definition: audit.ShareAccept, outcome: audit.OutcomeSuccess, targetID: "s-1",
		},
		{
			name:       "public link removed",
			req:        &link.RemovePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}},
			res:        &link.RemovePublicShareResponse{Status: ok},
			definition: audit.PublicLinkDelete, outcome: audit.OutcomeSuccess, targetID: "l-1",
		},
		{
			name:       "home created",
			req:        &provider.CreateStorageSpaceRequest{Type: "personal", Name: "Alan"},
			res:        &provider.CreateStorageSpaceResponse{Status: ok, StorageSpace: &provider.StorageSpace{Root: root}},
			definition: audit.SpaceCreate, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "project space exists",
			req:        &provider.CreateStorageSpaceRequest{Type: "project", Name: "Marketing"},
			res:        &provider.CreateStorageSpaceResponse{Status: &rpc.Status{Code: rpc.Code_CODE_ALREADY_EXISTS}},
			definition: audit.SpaceCreate, outcome: audit.OutcomeFailure, reason: "already_exists", targetID: "Marketing",
		},
		{
			name:       "space restored",
			req:        &provider.UpdateStorageSpaceRequest{Opaque: opaque("restore"), StorageSpace: &provider.StorageSpace{Root: root}},
			res:        &provider.UpdateStorageSpaceResponse{Status: ok},
			definition: audit.SpaceEnable, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "space quota changed",
			req:        &provider.UpdateStorageSpaceRequest{StorageSpace: &provider.StorageSpace{Root: root, Quota: &provider.Quota{QuotaMaxBytes: 10}}},
			res:        &provider.UpdateStorageSpaceResponse{Status: ok},
			definition: audit.SpaceQuotaChange, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "space renamed",
			req:        &provider.UpdateStorageSpaceRequest{StorageSpace: &provider.StorageSpace{Root: root, Name: "Marketing"}},
			res:        &provider.UpdateStorageSpaceResponse{Status: ok},
			definition: audit.SpaceUpdate, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "space purged",
			req:        &provider.DeleteStorageSpaceRequest{Opaque: opaque("purge"), Id: &provider.StorageSpaceId{OpaqueId: "storage$space"}},
			res:        &provider.DeleteStorageSpaceResponse{Status: ok},
			definition: audit.SpaceDelete, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "space disabled",
			req:        &provider.DeleteStorageSpaceRequest{Id: &provider.StorageSpaceId{OpaqueId: "storage$space"}},
			res:        &provider.DeleteStorageSpaceResponse{Status: ok},
			definition: audit.SpaceDisable, outcome: audit.OutcomeSuccess, targetID: "storage$space",
		},
		{
			name:       "file renamed",
			req:        &provider.MoveRequest{Source: &provider.Reference{ResourceId: root, Path: "./docs/a.txt"}, Destination: &provider.Reference{ResourceId: root, Path: "./docs/b.txt"}},
			res:        &provider.MoveResponse{Status: ok},
			itemType:   provider.ResourceType_RESOURCE_TYPE_FILE,
			definition: audit.FileRename, outcome: audit.OutcomeSuccess, targetID: "storage$space!space/docs/a.txt",
		},
		{
			name:       "folder moved",
			req:        &provider.MoveRequest{Source: &provider.Reference{ResourceId: root, Path: "./docs"}, Destination: &provider.Reference{ResourceId: root, Path: "./archive/docs"}},
			res:        &provider.MoveResponse{Status: ok},
			itemType:   provider.ResourceType_RESOURCE_TYPE_CONTAINER,
			definition: audit.FolderMove, outcome: audit.OutcomeSuccess, targetID: "storage$space!space/docs",
		},
		{
			name:       "file delete denied",
			req:        &provider.DeleteRequest{Ref: ref},
			res:        &provider.DeleteResponse{Status: denied},
			itemType:   provider.ResourceType_RESOURCE_TYPE_FILE,
			definition: audit.FileDelete, outcome: audit.OutcomeDenied, reason: "permission_denied", targetID: "storage$space!item/docs",
		},
		{
			name:       "folder restored",
			req:        &provider.RestoreRecycleItemRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k-1"},
			res:        &provider.RestoreRecycleItemResponse{Status: ok},
			itemType:   provider.ResourceType_RESOURCE_TYPE_CONTAINER,
			definition: audit.FolderRestore, outcome: audit.OutcomeSuccess, targetID: "k-1",
		},
		{
			name:       "file purged",
			req:        &provider.PurgeRecycleRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k-1"},
			res:        &provider.PurgeRecycleResponse{Status: ok},
			itemType:   provider.ResourceType_RESOURCE_TYPE_FILE,
			definition: audit.FilePurge, outcome: audit.OutcomeSuccess, targetID: "k-1",
		},
		{
			name:   "folder created, driver reported the node",
			req:    &provider.CreateContainerRequest{Ref: ref},
			res:    &provider.CreateContainerResponse{Status: ok},
			itemID: &provider.ResourceId{SpaceId: "space", OpaqueId: "node-1"}, itemName: "docs",
			definition: audit.FolderCreate, outcome: audit.OutcomeSuccess, targetID: "storage$space!node-1", targetName: "docs",
		},
		{
			name:     "file renamed, driver reported the node",
			req:      &provider.MoveRequest{Source: &provider.Reference{ResourceId: root, Path: "./docs/a.txt"}, Destination: &provider.Reference{ResourceId: root, Path: "./docs/b.txt"}},
			res:      &provider.MoveResponse{Status: ok},
			itemType: provider.ResourceType_RESOURCE_TYPE_FILE, itemID: &provider.ResourceId{SpaceId: "space", OpaqueId: "node-1"}, itemName: "a.txt",
			definition: audit.FileRename, outcome: audit.OutcomeSuccess, targetID: "storage$space!node-1", targetName: "a.txt",
		},
		{
			name:     "file restored, driver reported the node",
			req:      &provider.RestoreRecycleItemRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k-1"},
			res:      &provider.RestoreRecycleItemResponse{Status: ok},
			itemType: provider.ResourceType_RESOURCE_TYPE_FILE, itemID: &provider.ResourceId{SpaceId: "space", OpaqueId: "node-1"}, itemName: "a.txt",
			definition: audit.FileRestore, outcome: audit.OutcomeSuccess, targetID: "storage$space!node-1", targetName: "a.txt",
		},
		{
			name:     "folder purged, driver reported the node",
			req:      &provider.PurgeRecycleRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k-1"},
			res:      &provider.PurgeRecycleResponse{Status: ok},
			itemType: provider.ResourceType_RESOURCE_TYPE_CONTAINER, itemID: &provider.ResourceId{SpaceId: "space", OpaqueId: "node-1"}, itemName: "docs",
			definition: audit.FolderPurge, outcome: audit.OutcomeSuccess, targetID: "storage$space!node-1", targetName: "docs",
		},
		{
			name:       "share updated, with the name of the resource",
			req:        &collaboration.UpdateShareRequest{Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}}},
			res:        &collaboration.UpdateShareResponse{Status: ok, Opaque: utils.AppendPlainToOpaque(nil, "resourcename", "report.pdf"), Share: &collaboration.Share{ResourceId: rid}},
			definition: audit.ShareUpdate, outcome: audit.OutcomeSuccess, targetID: "s-1", targetName: "report.pdf",
		},
		{
			name:       "public link removed, with the name of the resource",
			req:        &link.RemovePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}},
			res:        &link.RemovePublicShareResponse{Status: ok, Opaque: utils.AppendPlainToOpaque(nil, "resourcename", "report.pdf")},
			definition: audit.PublicLinkDelete, outcome: audit.OutcomeSuccess, targetID: "l-1", targetName: "report.pdf",
		},
		{
			name:       "space renamed, with the name before",
			req:        &provider.UpdateStorageSpaceRequest{StorageSpace: &provider.StorageSpace{Root: root, Name: "Sales"}},
			res:        &provider.UpdateStorageSpaceResponse{Status: ok},
			itemName:   "Marketing",
			definition: audit.SpaceUpdate, outcome: audit.OutcomeSuccess, targetID: "storage$space", targetName: "Marketing",
		},
		{
			name:       "space disabled, with its name",
			req:        &provider.DeleteStorageSpaceRequest{Id: &provider.StorageSpaceId{OpaqueId: "storage$space"}},
			res:        &provider.DeleteStorageSpaceResponse{Status: ok},
			itemName:   "Marketing",
			definition: audit.SpaceDisable, outcome: audit.OutcomeSuccess, targetID: "storage$space", targetName: "Marketing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, mapped := auditEvent(tc.req, tc.res, &storagespace.ItemSlot{Type: tc.itemType, ID: tc.itemID, Name: tc.itemName})
			assert.True(t, mapped)
			assert.Equal(t, tc.definition, ev.Definition)
			assert.Equal(t, tc.outcome, ev.Outcome)
			assert.Equal(t, tc.reason, ev.Reason)
			assert.Equal(t, tc.targetID, ev.Target.ID)
			if tc.targetName != "" {
				assert.Equal(t, tc.targetName, ev.Target.Name)
			}
		})
	}
}

func TestAuditEventNotMapped(t *testing.T) {
	for name, tc := range map[string]struct{ req, res interface{} }{
		"move, type unknown":    {&provider.MoveRequest{}, &provider.MoveResponse{Status: ok}},
		"delete, type unknown":  {&provider.DeleteRequest{}, &provider.DeleteResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}}},
		"purge, type unknown":   {&provider.PurgeRecycleRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k"}, &provider.PurgeRecycleResponse{Status: ok}},
		"share pending":         {&collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state"}}}, &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{State: collaboration.ShareState_SHARE_STATE_PENDING}}},
		"accepted share hidden": {&collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"hidden"}}}, &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{State: collaboration.ShareState_SHARE_STATE_ACCEPTED, Hidden: true}}},
		"hidden, state sent":    {&collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state", "hidden"}}}, &collaboration.UpdateReceivedShareResponse{Status: ok, Opaque: utils.AppendPlainToOpaque(nil, "previousstate", "SHARE_STATE_ACCEPTED"), Share: &collaboration.ReceivedShare{State: collaboration.ShareState_SHARE_STATE_ACCEPTED, Hidden: true}}},
		"mount point renamed":   {&collaboration.UpdateReceivedShareRequest{UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"mount_point"}}}, &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{State: collaboration.ShareState_SHARE_STATE_ACCEPTED}}},
		"share with noevent":    {&collaboration.CreateShareRequest{}, &collaboration.CreateShareResponse{Status: ok, Opaque: opaque("noevent")}},
		"password probe":        {&link.GetPublicShareByTokenRequest{Token: "t"}, &link.GetPublicShareByTokenResponse{Status: denied}},
		"empty signature probe": {&link.GetPublicShareByTokenRequest{Token: "t", Authentication: &link.PublicShareAuthentication{Spec: &link.PublicShareAuthentication_Signature{Signature: &link.ShareSignature{}}}}, &link.GetPublicShareByTokenResponse{Status: denied}},
		"existing home":         {&provider.CreateStorageSpaceRequest{Type: "personal"}, &provider.CreateStorageSpaceResponse{Status: &rpc.Status{Code: rpc.Code_CODE_ALREADY_EXISTS}}},
	} {
		_, mapped := auditEvent(tc.req, tc.res, nil)
		assert.False(t, mapped, name)
	}
}
