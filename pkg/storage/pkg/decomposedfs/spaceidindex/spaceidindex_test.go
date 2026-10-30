// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package spaceidindex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexes(t *testing.T) {
	i := New(t.TempDir(), "by-type")
	require.NoError(t, i.Init())

	names, err := i.Indexes()
	require.NoError(t, err)
	require.Empty(t, names)

	require.NoError(t, i.Add("project", "p1", "../../spaces/p1"))
	require.NoError(t, i.Add("custom", "c1", "../../spaces/c1"))
	require.NoError(t, i.Add("personal", "u1", "../../spaces/u1"))
	// only the indexes count, not lock files or anything else
	require.NoError(t, os.WriteFile(filepath.Join(i.root, i.name, "custom.mpk.mlock"), nil, 0600))
	require.NoError(t, os.Mkdir(filepath.Join(i.root, i.name, "folder.mpk"), 0700))

	names, err = i.Indexes()
	require.NoError(t, err)
	require.Equal(t, []string{"custom", "personal", "project"}, names)
}
