package eventsmiddleware

import (
	"context"
	"testing"
	"time"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/audit"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
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
		itemType   provider.ResourceType
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
			req:  &collaboration.UpdateReceivedShareRequest{},
			res: &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{
				State: collaboration.ShareState_SHARE_STATE_REJECTED,
				Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}, ResourceId: rid},
			}},
			definition: audit.ShareDecline, outcome: audit.OutcomeSuccess, targetID: "s-1",
		},
		{
			name:       "public link removed",
			req:        &link.RemovePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}},
			res:        &link.RemovePublicShareResponse{Status: ok},
			definition: audit.PublicLinkDelete, outcome: audit.OutcomeSuccess, targetID: "l-1",
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, mapped := auditEvent(tc.req, tc.res, tc.itemType)
			assert.True(t, mapped)
			assert.Equal(t, tc.definition, ev.Definition)
			assert.Equal(t, tc.outcome, ev.Outcome)
			assert.Equal(t, tc.reason, ev.Reason)
			assert.Equal(t, tc.targetID, ev.Target.ID)
		})
	}
}

func TestAuditEventNotMapped(t *testing.T) {
	for name, tc := range map[string]struct{ req, res interface{} }{
		"move, type unknown":   {&provider.MoveRequest{}, &provider.MoveResponse{Status: ok}},
		"delete, type unknown": {&provider.DeleteRequest{}, &provider.DeleteResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}}},
		"purge, type unknown":  {&provider.PurgeRecycleRequest{Ref: &provider.Reference{ResourceId: root}, Key: "k"}, &provider.PurgeRecycleResponse{Status: ok}},
		"share hidden":         {&collaboration.UpdateReceivedShareRequest{}, &collaboration.UpdateReceivedShareResponse{Status: ok, Share: &collaboration.ReceivedShare{State: collaboration.ShareState_SHARE_STATE_PENDING}}},
		"share with noevent":   {&collaboration.CreateShareRequest{}, &collaboration.CreateShareResponse{Status: ok, Opaque: opaque("noevent")}},
		"password probe":       {&link.GetPublicShareByTokenRequest{Token: "t"}, &link.GetPublicShareByTokenResponse{Status: denied}},
	} {
		_, mapped := auditEvent(tc.req, tc.res, provider.ResourceType_RESOURCE_TYPE_INVALID)
		assert.False(t, mapped, name)
	}
}

func TestPublicLinkAccess(t *testing.T) {
	share := &link.PublicShare{Id: &link.PublicShareId{OpaqueId: "l-1"}, ResourceId: rid}
	withPassword := &link.PublicShareAuthentication{Spec: &link.PublicShareAuthentication_Password{Password: "secret"}}
	withSignature := &link.PublicShareAuthentication{Spec: &link.PublicShareAuthentication_Signature{Signature: &link.ShareSignature{Signature: "sig"}}}

	for _, tc := range []struct {
		name     string
		req      *link.GetPublicShareByTokenRequest
		res      *link.GetPublicShareByTokenResponse
		outcome  audit.Outcome
		reason   string
		targetID string
	}{
		{
			name: "access", outcome: audit.OutcomeSuccess, targetID: "l-1",
			req: &link.GetPublicShareByTokenRequest{Token: "t"},
			res: &link.GetPublicShareByTokenResponse{Status: ok, Share: share},
		},
		{
			name: "wrong password", outcome: audit.OutcomeDenied, reason: "invalid_password", targetID: "token:" + tokenHash("t"),
			req: &link.GetPublicShareByTokenRequest{Token: "t", Authentication: withPassword},
			res: &link.GetPublicShareByTokenResponse{Status: denied},
		},
		{
			name: "wrong signature", outcome: audit.OutcomeDenied, reason: "invalid_signature", targetID: "token:" + tokenHash("t"),
			req: &link.GetPublicShareByTokenRequest{Token: "t", Authentication: withSignature},
			res: &link.GetPublicShareByTokenResponse{Status: denied},
		},
		{
			name: "unknown token", outcome: audit.OutcomeFailure, reason: "not_found", targetID: "token:" + tokenHash("guessed"),
			req: &link.GetPublicShareByTokenRequest{Token: "guessed"},
			res: &link.GetPublicShareByTokenResponse{Status: &rpc.Status{Code: rpc.Code_CODE_NOT_FOUND}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, mapped := auditEvent(tc.req, tc.res, provider.ResourceType_RESOURCE_TYPE_INVALID)
			assert.True(t, mapped)
			assert.Equal(t, audit.PublicLinkAccess, ev.Definition)
			assert.Equal(t, tc.outcome, ev.Outcome)
			assert.Equal(t, tc.reason, ev.Reason)
			assert.Equal(t, tc.targetID, ev.Target.ID)
			assert.Equal(t, audit.ActorPublicLink, ev.Actor.Type)
			assert.NotEqual(t, "token:"+tc.req.GetToken(), ev.Target.ID, "the token itself is never in the record")
		})
	}
	assert.NotEqual(t, "t", tokenHash("t"))
	assert.Len(t, tokenHash("t"), 16)
}

func TestOnceWithin(t *testing.T) {
	o := newOnceWithin(30 * time.Minute)
	now := time.Now()

	assert.True(t, o.first("l-1|203.0.113.7", now))
	assert.False(t, o.first("l-1|203.0.113.7", now.Add(10*time.Minute)), "the same access")
	assert.True(t, o.first("l-1|198.51.100.1", now.Add(10*time.Minute)), "another client")
	assert.True(t, o.first("l-1|203.0.113.7", now.Add(41*time.Minute)), "a new access after the window")
}

func TestFirstLinkAccessOnlyDropsRepeatedSuccesses(t *testing.T) {
	o := newOnceWithin(time.Hour)
	ok := audit.Event{Definition: audit.PublicLinkAccess, Outcome: audit.OutcomeSuccess, Target: audit.Target{ID: "l-1"}}
	failed := audit.Event{Definition: audit.PublicLinkAccess, Outcome: audit.OutcomeDenied, Target: audit.Target{ID: "l-1"}}
	other := audit.Event{Definition: audit.FileLock, Outcome: audit.OutcomeSuccess, Target: audit.Target{ID: "f"}}

	assert.True(t, firstLinkAccess(context.Background(), o, ok))
	assert.False(t, firstLinkAccess(context.Background(), o, ok))
	assert.True(t, firstLinkAccess(context.Background(), o, failed), "every failure counts")
	assert.True(t, firstLinkAccess(context.Background(), o, failed))
	assert.True(t, firstLinkAccess(context.Background(), o, other))
	assert.True(t, firstLinkAccess(context.Background(), o, other))
}
