package grants

import (
	"testing"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

func TestGetACLPerm_ContainerFlags(t *testing.T) {
	tests := []struct {
		name string
		perm *provider.ResourcePermissions
		want string // substring that must be present
		not  string // substring that must NOT be present
	}{
		{
			name: "delete container allowed",
			perm: &provider.ResourcePermissions{Delete: true, DeleteContainer: true},
			want: "+dc",
		},
		{
			name: "delete container denied",
			perm: &provider.ResourcePermissions{Delete: true, DeleteContainer: false},
			want: "!dc",
		},
		{
			name: "move container allowed",
			perm: &provider.ResourcePermissions{Move: true, MoveContainer: true},
			want: "+mc",
		},
		{
			name: "move container denied",
			perm: &provider.ResourcePermissions{Move: true, MoveContainer: false},
			want: "!mc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetACLPerm(tt.perm)
			if err != nil {
				t.Fatalf("GetACLPerm() error = %v", err)
			}
			if tt.want != "" && !contains(got, tt.want) {
				t.Errorf("GetACLPerm() = %q, want substring %q", got, tt.want)
			}
			if tt.not != "" && contains(got, tt.not) {
				t.Errorf("GetACLPerm() = %q, should NOT contain %q", got, tt.not)
			}
		})
	}
}

func TestGetGrantPermissionSet_ContainerFlags(t *testing.T) {
	tests := []struct {
		name            string
		perm            string
		deleteContainer bool
		moveContainer   bool
		delete          bool
	}{
		{
			name:            "dc allowed",
			perm:            "rw+d+dc+mc",
			deleteContainer: true,
			moveContainer:   true,
			delete:          true,
		},
		{
			name:            "dc denied, file delete allowed",
			perm:            "rw+d!dc!mc",
			deleteContainer: false,
			moveContainer:   false,
			delete:          true, // +d is not affected by !dc
		},
		{
			name:            "both denied",
			perm:            "rw!d!dc!mc",
			deleteContainer: false,
			moveContainer:   false,
			delete:          false,
		},
		{
			name:            "file delete denied, container delete allowed",
			perm:            "rw!d+dc!mc",
			deleteContainer: true,
			moveContainer:   false,
			delete:          false, // !d still works even with +dc
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := GetGrantPermissionSet(tt.perm)
			if rp.DeleteContainer != tt.deleteContainer {
				t.Errorf("DeleteContainer = %v, want %v (perm=%q)", rp.DeleteContainer, tt.deleteContainer, tt.perm)
			}
			if rp.MoveContainer != tt.moveContainer {
				t.Errorf("MoveContainer = %v, want %v (perm=%q)", rp.MoveContainer, tt.moveContainer, tt.perm)
			}
			if rp.Delete != tt.delete {
				t.Errorf("Delete = %v, want %v (perm=%q)", rp.Delete, tt.delete, tt.perm)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
