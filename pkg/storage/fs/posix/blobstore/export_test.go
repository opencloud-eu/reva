// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package blobstore

// SetCanUseRenameForUpload is a test-only seam that forces the upload code path.
// When set to false, Upload uses the copy-with-periodic-sync fallback instead of
// renaming the source into place.
func (bs *Blobstore) SetCanUseRenameForUpload(v bool) {
	bs.canUseRenameForUpload = v
}
