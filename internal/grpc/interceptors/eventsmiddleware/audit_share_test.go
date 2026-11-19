package eventsmiddleware

import (
	"testing"

	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/opencloud-eu/reva/v3/pkg/conversions"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
)

func TestShareChanges(t *testing.T) {
	alan := &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: &userpb.UserId{OpaqueId: "alan"}}}
	devs := &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_GROUP, Id: &provider.Grantee_GroupId{GroupId: &grouppb.GroupId{OpaqueId: "devs"}}}
	viewer := conversions.NewViewerRole().CS3ResourcePermissions()
	editor := conversions.NewEditorRole().CS3ResourcePermissions()
	spaceEditor := conversions.NewSpaceEditorRole().CS3ResourcePermissions()
	custom := &provider.ResourcePermissions{Stat: true, InitiateFileDownload: true}
	exp := &types.Timestamp{Seconds: 1798761600} // 2027-01-01
	info := &provider.ResourceInfo{Id: rid, Name: "report.pdf"}

	removed := utils.AppendJSONToOpaque(nil, "granteeuserid", alan.GetUserId())
	removed = utils.AppendJSONToOpaque(removed, "permissions", editor)
	memberRemoved := utils.AppendJSONToOpaque(utils.SpaceGrantOpaque(), "granteegroupid", devs.GetGroupId())
	memberRemoved = utils.AppendJSONToOpaque(memberRemoved, "permissions", spaceEditor)

	for _, tc := range []struct {
		name       string
		req, res   any
		definition *audit.Definition
		change     *audit.Change
	}{
		{
			name:       "share created",
			req:        &collaboration.CreateShareRequest{ResourceInfo: info, Grant: &collaboration.ShareGrant{Grantee: alan, Permissions: &collaboration.SharePermissions{Permissions: viewer}, Expiration: exp}},
			res:        &collaboration.CreateShareResponse{Status: ok, Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}, Grantee: alan, Permissions: &collaboration.SharePermissions{Permissions: viewer}, Expiration: exp}},
			definition: audit.ShareCreate,
			change:     &audit.Change{After: map[string]any{"recipient_type": "user", "recipient": "alan", "role": "viewer", "expiration": "2027-01-01T00:00:00Z"}},
		},
		{
			name:       "failed share keeps the requested grant",
			req:        &collaboration.CreateShareRequest{ResourceInfo: info, Grant: &collaboration.ShareGrant{Grantee: devs, Permissions: &collaboration.SharePermissions{Permissions: custom}}},
			res:        &collaboration.CreateShareResponse{Status: denied},
			definition: audit.ShareCreate,
			change:     &audit.Change{After: map[string]any{"recipient_type": "group", "recipient": "devs", "role": "custom", "permissions": []string{"initiate_file_download", "stat"}}},
		},
		{
			name:       "space member added",
			req:        &collaboration.CreateShareRequest{ResourceInfo: &provider.ResourceInfo{Id: root}, Grant: &collaboration.ShareGrant{Grantee: devs, Permissions: &collaboration.SharePermissions{Permissions: spaceEditor}}},
			res:        &collaboration.CreateShareResponse{Status: ok, Opaque: utils.SpaceGrantOpaque(), Share: &collaboration.Share{Grantee: devs, Permissions: &collaboration.SharePermissions{Permissions: spaceEditor}}},
			definition: audit.SpaceMemberAdd,
			change:     &audit.Change{After: map[string]any{"recipient_type": "group", "recipient": "devs", "role": "spaceeditor"}},
		},
		{
			name:       "share role changed",
			req:        &collaboration.UpdateShareRequest{Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"permissions"}}},
			res:        &collaboration.UpdateShareResponse{Status: ok, Share: &collaboration.Share{Grantee: alan, ResourceId: rid, Permissions: &collaboration.SharePermissions{Permissions: editor}, Expiration: exp}},
			definition: audit.ShareUpdate,
			change:     &audit.Change{After: map[string]any{"recipient_type": "user", "recipient": "alan", "role": "editor"}},
		},
		{
			name:       "share expiration removed",
			req:        &collaboration.UpdateShareRequest{Share: &collaboration.Share{Id: &collaboration.ShareId{OpaqueId: "s-1"}}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"expiration"}}},
			res:        &collaboration.UpdateShareResponse{Status: ok, Share: &collaboration.Share{Grantee: alan, ResourceId: rid, Permissions: &collaboration.SharePermissions{Permissions: editor}}},
			definition: audit.ShareUpdate,
			change:     &audit.Change{After: map[string]any{"recipient_type": "user", "recipient": "alan", "expiration": ""}},
		},
		{
			name:       "share removed",
			req:        &collaboration.RemoveShareRequest{Ref: &collaboration.ShareReference{Spec: &collaboration.ShareReference_Id{Id: &collaboration.ShareId{OpaqueId: "s-1"}}}},
			res:        &collaboration.RemoveShareResponse{Status: ok, Opaque: removed},
			definition: audit.ShareDelete,
			change:     &audit.Change{Before: map[string]any{"recipient_type": "user", "recipient": "alan", "role": "editor"}},
		},
		{
			name:       "space member removed",
			req:        &collaboration.RemoveShareRequest{Ref: &collaboration.ShareReference{Spec: &collaboration.ShareReference_Id{Id: &collaboration.ShareId{OpaqueId: "s-2"}}}},
			res:        &collaboration.RemoveShareResponse{Status: ok, Opaque: memberRemoved},
			definition: audit.SpaceMemberRemove,
			change:     &audit.Change{Before: map[string]any{"recipient_type": "group", "recipient": "devs", "role": "spaceeditor"}},
		},
		{
			name:       "public link created",
			req:        &link.CreatePublicShareRequest{ResourceInfo: info, Grant: &link.Grant{Password: "secret"}},
			res:        &link.CreatePublicShareResponse{Status: ok, Share: &link.PublicShare{Id: &link.PublicShareId{OpaqueId: "l-1"}, Permissions: &link.PublicSharePermissions{Permissions: conversions.NewViewerRole().CS3ResourcePermissions()}, PasswordProtected: true, Expiration: exp}},
			definition: audit.PublicLinkCreate,
			change:     &audit.Change{After: map[string]any{"role": "viewer", "password": true, "expiration": "2027-01-01T00:00:00Z"}},
		},
		{
			name:       "internal link created",
			req:        &link.CreatePublicShareRequest{ResourceInfo: info},
			res:        &link.CreatePublicShareResponse{Status: ok, Share: &link.PublicShare{Id: &link.PublicShareId{OpaqueId: "l-2"}, Permissions: &link.PublicSharePermissions{Permissions: &provider.ResourcePermissions{}}}},
			definition: audit.PublicLinkCreate,
			change:     &audit.Change{After: map[string]any{"role": "internal", "password": false}},
		},
		{
			name:       "public link password removed",
			req:        &link.UpdatePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}, Update: &link.UpdatePublicShareRequest_Update{Type: link.UpdatePublicShareRequest_Update_TYPE_PASSWORD, Grant: &link.Grant{}}},
			res:        &link.UpdatePublicShareResponse{Status: ok, Share: &link.PublicShare{ResourceId: rid}},
			definition: audit.PublicLinkUpdate,
			change:     &audit.Change{After: map[string]any{"password": false}},
		},
		{
			name:       "public link role changed",
			req:        &link.UpdatePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}, Update: &link.UpdatePublicShareRequest_Update{Type: link.UpdatePublicShareRequest_Update_TYPE_PERMISSIONS}},
			res:        &link.UpdatePublicShareResponse{Status: ok, Share: &link.PublicShare{ResourceId: rid, Permissions: &link.PublicSharePermissions{Permissions: conversions.NewUploaderRole().CS3ResourcePermissions()}}},
			definition: audit.PublicLinkUpdate,
			change:     &audit.Change{After: map[string]any{"role": "uploader"}},
		},
		{
			name:       "public link removed",
			req:        &link.RemovePublicShareRequest{Ref: &link.PublicShareReference{Spec: &link.PublicShareReference_Id{Id: &link.PublicShareId{OpaqueId: "l-1"}}}},
			res:        &link.RemovePublicShareResponse{Status: ok, Opaque: utils.AppendJSONToOpaque(nil, "permissions", conversions.NewEditorRole().CS3ResourcePermissions())},
			definition: audit.PublicLinkDelete,
			change:     &audit.Change{Before: map[string]any{"role": "editor"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := auditEvent(tc.req, tc.res, nil)
			require.True(t, ok)
			assert.Equal(t, tc.definition, ev.Definition)
			assert.Equal(t, tc.change, ev.Change)
		})
	}
}
