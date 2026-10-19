// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type mockRevisionCleanupStrategy struct {
	name         string
	keysToDelete []string
}

var _ RevisionCleanupStrategy = &mockRevisionCleanupStrategy{}

func (m mockRevisionCleanupStrategy) Cleanup(now time.Time, revs []Revision, ctx context.Context, logger *zerolog.Logger) (Plan, error) {
	plan := NewSimplePlan()
	for _, rev := range revs {
		if slices.Contains(m.keysToDelete, rev.Key()) {
			plan.Delete(rev)
		} else {
			plan.Retain(rev)
		}
	}
	return plan, nil
}

func TestNewChainRevisionCleanupStrategy(t *testing.T) {
	require := require.New(t)

	now := rfc3339(t, "2026-09-22T11:00:00.000Z")
	a := newPlainRevision("a", rfc3339(t, "2026-09-22T10:59:30.000Z"), 1) // within a minute
	b := newPlainRevision("b", rfc3339(t, "2026-09-22T10:05:30.000Z"), 1) // within an hour
	c := newPlainRevision("c", rfc3339(t, "2026-09-21T11:05:30.000Z"), 1) // within 24 hours
	d := newPlainRevision("d", rfc3339(t, "2026-08-23T11:05:30.000Z"), 1) // within 30 days
	e := newPlainRevision("e", rfc3339(t, "2026-07-22T11:00:30.000Z"), 1) // older than 30 days
	revs := []Revision{a, b, c, d, e}
	SortRevisionsCanonically(revs)

	checkRevs(t, revs)
	chain, err := newChainRevisionCleanupStrategy([]RevisionCleanupStrategy{
		&mockRevisionCleanupStrategy{name: "x", keysToDelete: []string{"d"}},
		&mockRevisionCleanupStrategy{name: "y", keysToDelete: []string{"a"}},
		&mockRevisionCleanupStrategy{name: "z", keysToDelete: []string{"e"}},
	})
	require.NoError(err)

	logger := zerolog.New(os.Stdout)

	plan, err := chain.Cleanup(now, revs, context.TODO(), &logger)
	require.NoError(err)

	require.EqualValues([]string{"d", "a", "e"}, keysOf(plan.Deleted()))
}
