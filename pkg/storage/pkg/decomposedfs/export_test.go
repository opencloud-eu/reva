// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs

import "github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"

// SetTreeForTest replaces the tree of fs, for tests only, e.g. to wrap it with a fake that
// records calls. It returns a function that restores the previous tree.
//
// This file is part of package decomposedfs but only compiled for tests, so the method is
// available to the external decomposedfs_test package without being part of the API.
func (fs *Decomposedfs) SetTreeForTest(tp node.Tree) func() {
	previous := fs.tp
	fs.tp = tp
	return func() { fs.tp = previous }
}
