package user_test

import (
	"testing"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/ocm/user"
)

func TestTrimOCMScheme(t *testing.T) {
	cases := map[string]string{
		"host.docker":         "host.docker",
		"https://host.docker": "host.docker",
		"http://host.docker":  "host.docker",
		"":                    "",
	}
	for in, want := range cases {
		if got := user.TrimOCMScheme(in); got != want {
			t.Errorf("TrimOCMScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRemoteUserID(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		provider string
		want     string
	}{
		{
			name:     "bare id is left untouched (spec-conformant)",
			userID:   "f7fbf8c8-1234",
			provider: "ocis2.docker",
			want:     "f7fbf8c8-1234",
		},
		{
			name:     "id qualified with matching host is stripped (oCIS)",
			userID:   "f7fbf8c8-1234@ocis2.docker",
			provider: "ocis2.docker",
			want:     "f7fbf8c8-1234",
		},
		{
			name:     "id qualified with scheme+host is stripped (OpenCloud)",
			userID:   "60708dda-5678@https://opencloud2.docker",
			provider: "opencloud2.docker",
			want:     "60708dda-5678",
		},
		{
			name:     "scheme in provider domain is tolerated",
			userID:   "60708dda-5678@opencloud2.docker",
			provider: "https://opencloud2.docker",
			want:     "60708dda-5678",
		},
		{
			name:     "double-qualified id collapses fully",
			userID:   "f7fbf8c8-1234@ocis2.docker@ocis2.docker",
			provider: "ocis2.docker",
			want:     "f7fbf8c8-1234",
		},
		{
			name:     "trailing suffix from a different host is preserved",
			userID:   "user@other.docker",
			provider: "ocis2.docker",
			want:     "user@other.docker",
		},
		{
			name:     "empty provider returns userID unchanged",
			userID:   "user@ocis2.docker",
			provider: "",
			want:     "user@ocis2.docker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := user.NormalizeRemoteUserID(tt.userID, tt.provider); got != tt.want {
				t.Errorf("NormalizeRemoteUserID(%q, %q) = %q, want %q", tt.userID, tt.provider, got, tt.want)
			}
		})
	}
}

func TestParseOCMAddress(t *testing.T) {
	uid, err := user.ParseOCMAddress("id@http://host.docker")
	if err != nil {
		t.Fatalf("ParseOCMAddress returned error: %v", err)
	}
	if uid.Idp != "host.docker" {
		t.Errorf("Idp = %q, want %q", uid.Idp, "host.docker")
	}
	if uid.OpaqueId != "id" {
		t.Errorf("OpaqueId = %q, want %q", uid.OpaqueId, "id")
	}
}

func TestCanonicalizeRemoteUserID(t *testing.T) {
	t.Run("nil input does not panic", func(t *testing.T) {
		user.CanonicalizeRemoteUserID(nil)
	})

	t.Run("strips scheme from Idp and matching qualified opaque id", func(t *testing.T) {
		id := &userpb.UserId{
			OpaqueId: "id@host.docker",
			Idp:      "https://host.docker",
		}
		user.CanonicalizeRemoteUserID(id)
		if id.Idp != "host.docker" {
			t.Errorf("Idp = %q, want %q", id.Idp, "host.docker")
		}
		if id.OpaqueId != "id" {
			t.Errorf("OpaqueId = %q, want %q", id.OpaqueId, "id")
		}
	})
}

func TestFormatOCMUser(t *testing.T) {
	tests := []struct {
		name string
		user *userpb.UserId
		want string
	}{
		{
			name: "bare opaque id and clean host",
			user: &userpb.UserId{OpaqueId: "f7fbf8c8-1234", Idp: "cernbox2.docker"},
			want: "f7fbf8c8-1234@cernbox2.docker",
		},
		{
			name: "opaque id already qualified with matching host does not double up",
			user: &userpb.UserId{OpaqueId: "f7fbf8c8-1234@ocis2.docker", Idp: "ocis2.docker"},
			want: "f7fbf8c8-1234@ocis2.docker",
		},
		{
			name: "scheme in host is stripped",
			user: &userpb.UserId{OpaqueId: "60708dda-5678", Idp: "https://opencloud2.docker"},
			want: "60708dda-5678@opencloud2.docker",
		},
		{
			name: "scheme-qualified opaque id collapses to single host",
			user: &userpb.UserId{OpaqueId: "60708dda-5678@https://opencloud2.docker", Idp: "opencloud2.docker"},
			want: "60708dda-5678@opencloud2.docker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := user.FormatOCMUser(tt.user); got != tt.want {
				t.Errorf("FormatOCMUser(%+v) = %q, want %q", tt.user, got, tt.want)
			}
		})
	}
}

func TestRemoteUserIDsMatch(t *testing.T) {
	const (
		uuid = "056fc874-dd7f-11ef-ba84-af6fca4b7289"
		idp  = "cloud2.opencloud.test:10200"
	)
	legacyOpaque := uuid + "@https://" + idp

	tests := []struct {
		name   string
		stored *userpb.UserId
		query  *userpb.UserId
		want   bool
	}{
		{
			name:   "canonical exact match",
			stored: &userpb.UserId{OpaqueId: uuid, Idp: idp},
			query:  &userpb.UserId{OpaqueId: uuid, Idp: idp},
			want:   true,
		},
		{
			name:   "legacy stored found by canonical query",
			stored: &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			query:  &userpb.UserId{OpaqueId: uuid, Idp: idp},
			want:   true,
		},
		{
			name:   "legacy stored found by legacy graph query opaque",
			stored: &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			query:  &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			want:   true,
		},
		{
			name:   "different users",
			stored: &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			query:  &userpb.UserId{OpaqueId: "other-uuid", Idp: idp},
			want:   false,
		},
		{
			name:   "different idp",
			stored: &userpb.UserId{OpaqueId: uuid, Idp: idp},
			query:  &userpb.UserId{OpaqueId: uuid, Idp: "other.example.com"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := user.RemoteUserIDsMatch(tt.stored, tt.query); got != tt.want {
				t.Errorf("RemoteUserIDsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}
