package eventsmiddleware

import (
	"time"

	groupv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/opencloud-eu/reva/v3/pkg/audit"
	"github.com/opencloud-eu/reva/v3/pkg/conversions"
	"github.com/opencloud-eu/reva/v3/pkg/storage/utils/grants"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
)

// The keys of share, space member and public link changes. A share or member
// change always names the recipient, a role change the new role.
const (
	keyRecipientType = "recipient_type"
	keyRecipient     = "recipient"
	keyRole          = "role"
	keyPermissions   = "permissions"
	keyExpiration    = "expiration"
	keyPassword      = "password"
)

// roleCustom is the role of permissions that match no known role.
const roleCustom = "custom"

// recipient adds the grantee to m.
func recipient(m map[string]any, g *provider.Grantee) {
	switch {
	case g.GetUserId() != nil:
		m[keyRecipientType], m[keyRecipient] = "user", g.GetUserId().GetOpaqueId()
	case g.GetGroupId() != nil:
		m[keyRecipientType], m[keyRecipient] = "group", g.GetGroupId().GetOpaqueId()
	}
}

// roles are the reva roles the unified roles are built from, in the order
// they are matched.
var roles = []func() *conversions.Role{
	conversions.NewViewerRole,
	conversions.NewViewerWithVersionsRole,
	conversions.NewViewerListGrantsRole,
	conversions.NewSpaceViewerRole,
	conversions.NewSpaceViewerWithVersionsRole,
	conversions.NewEditorRole,
	conversions.NewEditorWithVersionsRole,
	conversions.NewEditorListGrantsRole,
	conversions.NewEditorListGrantsWithVersionsRole,
	conversions.NewSpaceEditorRole,
	conversions.NewSpaceEditorWithoutVersionsRole,
	conversions.NewSpaceEditorWithoutTrashbinRole,
	conversions.NewSpaceEditorWithoutVersionsWithoutTrashbinRole,
	conversions.NewFileEditorRole,
	conversions.NewFileEditorWithVersionsRole,
	conversions.NewFileEditorListGrantsRole,
	conversions.NewFileEditorListGrantsWithVersionsRole,
	conversions.NewEditorLiteRole,
	conversions.NewUploaderRole,
	conversions.NewSecureViewerRole,
	conversions.NewManagerRole,
	conversions.NewCoownerRole,
}

// role adds the role of the permissions to m: the reva role with exactly
// these permissions, the one a unified role is built from, or "custom" with
// the permission names.
func role(m map[string]any, p *provider.ResourcePermissions, isLink bool) {
	if p == nil {
		return
	}
	if grants.PermissionsEqual(p, &provider.ResourcePermissions{}) {
		// empty permissions deny access, on a link they make an internal link
		m[keyRole] = conversions.RoleDenied
		if isLink {
			m[keyRole] = "internal"
		}
		return
	}
	for _, r := range roles {
		if role := r(); grants.PermissionsEqual(p, role.CS3ResourcePermissions()) {
			m[keyRole] = role.Name
			return
		}
	}
	m[keyRole] = roleCustom
	m[keyPermissions] = permissionNames(p)
}

// permissionNames lists the granted permissions, e.g. stat, list_container.
func permissionNames(p *provider.ResourcePermissions) []string {
	names := []string{}
	p.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.BoolKind && v.Bool() {
			names = append(names, string(fd.Name()))
		}
		return true
	})
	return names
}

// expiration adds the expiration date to m, an empty string when it was removed.
func expiration(m map[string]any, ts *typesv1beta1.Timestamp) {
	if ts == nil {
		m[keyExpiration] = ""
		return
	}
	m[keyExpiration] = time.Unix(int64(ts.GetSeconds()), int64(ts.GetNanos())).UTC().Format(time.RFC3339)
}

// shareCreated describes a new share or space member. The response carries the
// share; a failed request only has the grant that was asked for.
func shareCreated(r *collaboration.CreateShareRequest, res *collaboration.CreateShareResponse) *audit.Change {
	g, p, exp := r.GetGrant().GetGrantee(), r.GetGrant().GetPermissions().GetPermissions(), r.GetGrant().GetExpiration()
	if s := res.GetShare(); s != nil {
		g, p, exp = s.GetGrantee(), s.GetPermissions().GetPermissions(), s.GetExpiration()
	}
	after := map[string]any{}
	recipient(after, g)
	role(after, p, false)
	if exp != nil {
		expiration(after, exp)
	}
	return &audit.Change{After: after}
}

// shareUpdated describes what an update changed: the fields of the update
// mask, or of the deprecated field.
func shareUpdated(r *collaboration.UpdateShareRequest, res *collaboration.UpdateShareResponse) *audit.Change {
	s := res.GetShare()
	if s == nil {
		s = r.GetShare()
	}
	after := map[string]any{}
	recipient(after, s.GetGrantee())
	for _, p := range r.GetUpdateMask().GetPaths() {
		switch p {
		case "permissions":
			role(after, s.GetPermissions().GetPermissions(), false)
		case "expiration":
			expiration(after, s.GetExpiration())
		}
	}
	if p := r.GetField().GetPermissions(); p != nil {
		role(after, p.GetPermissions(), false)
	}
	return &audit.Change{After: after}
}

// shareRemoved describes the removed share from what the share provider put
// into the response.
func shareRemoved(o *typesv1beta1.Opaque) *audit.Change {
	before := map[string]any{}
	var u *userpb.UserId
	var g *groupv1beta1.GroupId
	switch {
	case utils.ReadJSONFromOpaque(o, "granteeuserid", &u) == nil && u != nil:
		recipient(before, &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_USER, Id: &provider.Grantee_UserId{UserId: u}})
	case utils.ReadJSONFromOpaque(o, "granteegroupid", &g) == nil && g != nil:
		recipient(before, &provider.Grantee{Type: provider.GranteeType_GRANTEE_TYPE_GROUP, Id: &provider.Grantee_GroupId{GroupId: g}})
	}
	var p *provider.ResourcePermissions
	if utils.ReadJSONFromOpaque(o, "permissions", &p) == nil {
		role(before, p, false)
	}
	if len(before) == 0 {
		return nil
	}
	return &audit.Change{Before: before}
}

// linkCreated describes a new public link: role, expiration and whether it
// has a password, never the password itself.
func linkCreated(r *link.CreatePublicShareRequest, res *link.CreatePublicShareResponse) *audit.Change {
	after := map[string]any{}
	if s := res.GetShare(); s != nil {
		role(after, s.GetPermissions().GetPermissions(), true)
		after[keyPassword] = s.GetPasswordProtected()
		if s.GetExpiration() != nil {
			expiration(after, s.GetExpiration())
		}
		return &audit.Change{After: after}
	}
	role(after, r.GetGrant().GetPermissions().GetPermissions(), true)
	after[keyPassword] = r.GetGrant().GetPassword() != ""
	if r.GetGrant().GetExpiration() != nil {
		expiration(after, r.GetGrant().GetExpiration())
	}
	return &audit.Change{After: after}
}

// linkUpdated describes the one attribute a public link update changes.
func linkUpdated(r *link.UpdatePublicShareRequest, res *link.UpdatePublicShareResponse) *audit.Change {
	u := r.GetUpdate()
	s := res.GetShare()
	after := map[string]any{}
	switch u.GetType() {
	case link.UpdatePublicShareRequest_Update_TYPE_PERMISSIONS:
		p := u.GetGrant().GetPermissions().GetPermissions()
		if s != nil {
			p = s.GetPermissions().GetPermissions()
		}
		role(after, p, true)
	case link.UpdatePublicShareRequest_Update_TYPE_PASSWORD:
		// true: set or changed, false: removed
		after[keyPassword] = u.GetGrant().GetPassword() != ""
		if s != nil {
			after[keyPassword] = s.GetPasswordProtected()
		}
	case link.UpdatePublicShareRequest_Update_TYPE_EXPIRATION:
		expiration(after, u.GetGrant().GetExpiration())
	case link.UpdatePublicShareRequest_Update_TYPE_DISPLAYNAME:
		after["display_name"] = u.GetDisplayName()
	case link.UpdatePublicShareRequest_Update_TYPE_DESCRIPTION:
		after["description"] = u.GetDescription()
	case link.UpdatePublicShareRequest_Update_TYPE_NOTIFYUPLOADS:
		after["notify_uploads"] = u.GetNotifyUploads()
	case link.UpdatePublicShareRequest_Update_TYPE_NOTIFYUPLOADSEXTRARECIPIENTS:
		// email addresses, only whether there are any
		after["notify_uploads_extra_recipients"] = u.GetNotifyUploadsExtraRecipients() != ""
	default:
		return nil
	}
	return &audit.Change{After: after}
}

// linkRemoved describes the removed public link.
func linkRemoved(o *typesv1beta1.Opaque) *audit.Change {
	var p *provider.ResourcePermissions
	if utils.ReadJSONFromOpaque(o, "permissions", &p) != nil || p == nil {
		return nil
	}
	before := map[string]any{}
	role(before, p, true)
	return &audit.Change{Before: before}
}
