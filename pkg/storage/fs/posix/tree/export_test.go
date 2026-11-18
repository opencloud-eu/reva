// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tree

// AssimilateForTest assimilates the given path like the watcher would after a change, for tests only.
//
// Tests that need changes on disk to be picked up have two options, and neither works
// for a test with its own configuration:
//   - A second watching test env is not possible: the inotify watcher starts
//     inotifywait with KillOthers (see watcher_linux.go), which runs
//     'pkill inotifywait' and thereby kills the watcher of the suite's shared env,
//     making unrelated tests fail depending on the spec order.
//   - In a non-watching env, ListFolder only assimilates nodes it does not know yet,
//     modifications of known files are not picked up.
//
// This file is part of package tree but only compiled for tests, so the method is
// available to the external tree_test package without being part of the API.
func (t *Tree) AssimilateForTest(path string) error {
	return t.assimilate(scanItem{Path: path})
}
