// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package lookup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/lookup"
)

// newSpace creates the root node file of a space with the given extension and
// returns the storage root
func newSpace(t *testing.T, ext string) string {
	t.Helper()
	root := t.TempDir()
	spaceID := "f3a4b5c6-0000-4000-8000-000000000001"
	spaceDir := filepath.Join(root, "spaces", lookup.Pathify(spaceID, 1, 2))
	nodeFile := filepath.Join(spaceDir, "nodes", lookup.Pathify(spaceID, 4, 2)+ext)
	if err := os.MkdirAll(filepath.Dir(nodeFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodeFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDetectBackendOnDisk(t *testing.T) {
	tests := []struct {
		name string
		root string
		want string
	}{
		{name: "a space with messagepack metadata", root: newSpace(t, ".mpk"), want: "mpk"},
		{name: "a space with metadata of a removed backend", root: newSpace(t, ".ini"), want: "unknown"},
		{name: "a space without metadata files", root: newSpace(t, ""), want: "unknown"},
		{name: "a storage without spaces", root: t.TempDir(), want: "unknown"},
		{name: "a root that does not exist", root: filepath.Join(t.TempDir(), "missing"), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lookup.DetectBackendOnDisk(tt.root); got != tt.want {
				t.Errorf("DetectBackendOnDisk() = %q, want %q", got, tt.want)
			}
		})
	}
}
