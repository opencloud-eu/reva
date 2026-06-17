package conversions

import (
	"testing"

	providerv1beta1 "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/stretchr/testify/assert"
)

func TestSufficientPermissions(t *testing.T) {
	type testData struct {
		Existing   *providerv1beta1.ResourcePermissions
		Requested  *providerv1beta1.ResourcePermissions
		Sufficient bool
	}
	table := []testData{
		{
			Existing:   nil,
			Requested:  nil,
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("editor").CS3ResourcePermissions(),
			Requested:  nil,
			Sufficient: false,
		},
		{
			Existing:   nil,
			Requested:  RoleFromName("viewer").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("editor").CS3ResourcePermissions(),
			Requested:  RoleFromName("viewer").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("viewer").CS3ResourcePermissions(),
			Requested:  RoleFromName("editor").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("spaceviewer").CS3ResourcePermissions(),
			Requested:  RoleFromName("spaceeditor").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("manager").CS3ResourcePermissions(),
			Requested:  RoleFromName("spaceeditor").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("manager").CS3ResourcePermissions(),
			Requested:  RoleFromName("spaceviewer").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("manager").CS3ResourcePermissions(),
			Requested:  RoleFromName("manager").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("manager").CS3ResourcePermissions(),
			Requested:  RoleFromName("denied").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("spaceeditor").CS3ResourcePermissions(),
			Requested:  RoleFromName("denied").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("editor").CS3ResourcePermissions(),
			Requested:  RoleFromName("denied").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("secure-viewer").CS3ResourcePermissions(),
			Requested:  RoleFromName("secure-viewer").CS3ResourcePermissions(),
			Sufficient: true,
		},
		{
			Existing:   RoleFromName("secure-viewer").CS3ResourcePermissions(),
			Requested:  RoleFromName("viewer").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing:   RoleFromName("secure-viewer").CS3ResourcePermissions(),
			Requested:  RoleFromName("editor").CS3ResourcePermissions(),
			Sufficient: false,
		},
		{
			Existing: &providerv1beta1.ResourcePermissions{
				// all permissions, used for personal space owners
				AddGrant:             true,
				CreateContainer:      true,
				Delete:               true,
				GetPath:              true,
				GetQuota:             true,
				InitiateFileDownload: true,
				InitiateFileUpload:   true,
				ListContainer:        true,
				ListFileVersions:     true,
				ListGrants:           true,
				ListRecycle:          true,
				Move:                 true,
				PurgeRecycle:         true,
				RemoveGrant:          true,
				RestoreFileVersion:   true,
				RestoreRecycleItem:   true,
				Stat:                 true,
				UpdateGrant:          true,
				DenyGrant:            true,
			},
			Requested:  RoleFromName("denied").CS3ResourcePermissions(),
			Sufficient: true,
		},
	}
	for _, test := range table {
		assert.Equal(t, test.Sufficient, SufficientCS3Permissions(test.Existing, test.Requested))
	}
}

func TestRolesWithVersions(t *testing.T) {
	table := []struct {
		name               string
		role               *Role
		expectedName       string
		base               *Role
		listFileVersions   bool
		restoreFileVersion bool
	}{
		{
			name:               "viewer-with-versions extends viewer with list versions",
			role:               NewViewerWithVersionsRole(),
			expectedName:       RoleViewerWithVersions,
			base:               NewViewerRole(),
			listFileVersions:   true,
			restoreFileVersion: false,
		},
		{
			name:               "space-viewer-with-versions extends space-viewer with list versions",
			role:               NewSpaceViewerWithVersionsRole(),
			expectedName:       RoleSpaceViewerWithVersions,
			base:               NewSpaceViewerRole(),
			listFileVersions:   true,
			restoreFileVersion: false,
		},
		{
			name:               "editor-with-versions extends editor with list and restore versions",
			role:               NewEditorWithVersionsRole(),
			expectedName:       RoleEditorWithVersions,
			base:               NewEditorRole(),
			listFileVersions:   true,
			restoreFileVersion: true,
		},
		{
			name:               "file-editor-with-versions extends file-editor with list and restore versions",
			role:               NewFileEditorWithVersionsRole(),
			expectedName:       RoleFileEditorWithVersions,
			base:               NewFileEditorRole(),
			listFileVersions:   true,
			restoreFileVersion: true,
		},
	}

	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			perms := test.role.CS3ResourcePermissions()

			assert.Equal(t, test.expectedName, test.role.Name)

			fromName := RoleFromName(test.expectedName)
			assert.Equal(t, test.expectedName, fromName.Name)
			assert.Equal(t, perms, fromName.CS3ResourcePermissions())

			// the base role is a subset of the new role
			basePerms := test.base.CS3ResourcePermissions()
			assert.True(t, SufficientCS3Permissions(perms, basePerms),
				"with-versions role should grant at least the base role's permissions")

			// the base role has less permissions than the new role
			assert.False(t, SufficientCS3Permissions(basePerms, perms),
				"base role should not be sufficient for the with-versions role")
		})
	}
}
