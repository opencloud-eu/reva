// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package usermapper

import (
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestScopeUserByIdsRestoresTheScope(t *testing.T) {
	// the fsuid and fsgid belong to the thread: stay on the thread ScopeUserByIds runs on
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fsuid, _ := unix.SetfsuidRetUid(-1)
	fsgid, _ := unix.SetfsgidRetGid(-1)
	current := func() (int, int) {
		uid, _ := unix.SetfsuidRetUid(-1)
		gid, _ := unix.SetfsgidRetGid(-1)
		return uid, gid
	}

	t.Run("after switching", func(t *testing.T) {
		unscope, err := (&UnixMapper{}).ScopeUserByIds(os.Geteuid(), os.Getegid())
		require.NoError(t, err)
		require.NoError(t, unscope())

		uid, gid := current()
		assert.Equal(t, fsuid, uid)
		assert.Equal(t, fsgid, gid)
	})

	t.Run("when switching fails", func(t *testing.T) {
		// a group this process is not in, which it can't switch to without CAP_SETGID
		groups, err := os.Getgroups()
		require.NoError(t, err)
		foreign := os.Getegid() + 1
		for slices.Contains(groups, foreign) || foreign == os.Getgid() {
			foreign++
		}

		unscope, err := (&UnixMapper{}).ScopeUserByIds(os.Geteuid(), foreign)
		if err == nil {
			_ = unscope()
			t.Skip("this process may switch to any group, e.g. as root")
		}
		assert.Nil(t, unscope)

		// the fsuid it had switched already is restored as well
		uid, gid := current()
		assert.Equal(t, fsuid, uid)
		assert.Equal(t, fsgid, gid)
	})
}
