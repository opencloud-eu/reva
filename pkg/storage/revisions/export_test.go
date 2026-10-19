// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type plainRevision struct {
	key   string
	mtime time.Time
	size  uint64
}

var _ Revision = &plainRevision{}

func (p plainRevision) Key() string {
	return p.key
}
func (p plainRevision) LastModificationTime() time.Time {
	return p.mtime
}
func (p plainRevision) Size() uint64 {
	return p.size
}

func newPlainRevision(key string, mtime time.Time, size int) Revision {
	return &plainRevision{
		key:   key,
		mtime: mtime,
		size:  uint64(size),
	}
}

func rfc3339(t *testing.T, value string) time.Time {
	if r, err := time.Parse(time.RFC3339, value); err != nil {
		require.FailNowf(t, "failed to parse as RFC3339", value, err)
		return time.Time{}
	} else {
		return r
	}
}

func checkRevs(t *testing.T, revs []Revision) {
	t.Helper()

	// ensure that we don't have revisions that share the same Key attribute,
	// which can easily happen when copy/pasting them
	memory := map[string]struct{}{}
	for _, rev := range revs {
		if _, ok := memory[rev.Key()]; ok {
			t.Fatalf("duplicate revision key: '%s'", rev.Key())
		}
	}

	// ensure they are in decreasingly sorted order
	isDecreasing := slices.IsSortedFunc(revs, func(a, b Revision) int {
		aTime := a.LastModificationTime()
		bTime := b.LastModificationTime()

		if aTime.After(bTime) {
			return -1
		}
		if aTime.Before(bTime) {
			return 1
		}
		return 0
	})
	if !isDecreasing {
		t.Fatalf("revisions are required to be sorted (newest first), but they are not")
	}
}

func keysOf(revs []Revision) []string {
	s := make([]string, len(revs))
	for i, rev := range revs {
		s[i] = rev.Key()
	}
	return s
}
