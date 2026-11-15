// SPDX-License-Identifier: Apache-2.0

package s3utils_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/s3utils"
)

func TestNewSSEC(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	for _, tc := range []struct {
		name, key     string
		secure, valid bool
	}{
		{"unset HTTP", "", false, true},
		{"unset HTTPS", "", true, true},
		{"valid", key, true, true},
		{"HTTP", key, false, false},
		{"invalid base64", "secret!", true, false},
		{"short", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31)), true, false},
		{"long", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 33)), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := s3utils.NewSSEC(tc.key, tc.secure)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), tc.key) {
				t.Fatal("error exposes customer key")
			}
			if !tc.valid {
				return
			}
			if tc.key == "" {
				if enc != nil {
					t.Fatal("empty key enabled encryption")
				}
				return
			}
			h := http.Header{}
			enc.Marshal(h)
			if h.Get("X-Amz-Server-Side-Encryption-Customer-Key") != key {
				t.Fatal("encryption options contain the wrong customer key")
			}
		})
	}
}
