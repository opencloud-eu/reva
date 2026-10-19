// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestRevisionComparator(t *testing.T) {
	require := require.New(t)

	a := newPlainRevision("a", rfc3339(t, "2026-09-22T16:49:00.000Z"), 300)
	b := newPlainRevision("b", rfc3339(t, "2026-09-22T16:50:00.000Z"), 200)
	c := newPlainRevision("c", rfc3339(t, "2026-09-22T16:51:00.000Z"), 100)

	require.Positive(CompareNewestFirst(a, b))
	require.Positive(CompareNewestFirst(b, c))
	require.Positive(CompareNewestFirst(a, c))

	require.Zero(CompareNewestFirst(a, a))
	require.Zero(CompareNewestFirst(b, b))
	require.Zero(CompareNewestFirst(c, c))

	require.Negative(CompareNewestFirst(b, a))
	require.Negative(CompareNewestFirst(c, b))
	require.Negative(CompareNewestFirst(c, a))
}

func TestSort(t *testing.T) {
	require := require.New(t)

	a := newPlainRevision("a", rfc3339(t, "2026-09-22T16:51:00.000Z"), 100)
	b := newPlainRevision("b", rfc3339(t, "2026-09-22T16:50:00.000Z"), 200)
	c := newPlainRevision("c", rfc3339(t, "2026-09-22T16:49:00.000Z"), 300)

	perm3 := func(a, b, c Revision) [][]Revision {
		return [][]Revision{
			{a, b, c},
			{a, c, b},
			{b, a, c},
			{b, c, a},
			{c, a, b},
			{c, b, a},
		}
	}

	for i, perm := range perm3(a, b, c) {
		names := make([]string, len(perm))
		for i, value := range perm {
			names[i] = value.Key()
		}
		name := "[" + strings.Join(names, "|") + "]"
		t.Run(fmt.Sprintf("Permutation_%d:%s", i, name), func(t *testing.T) {
			s := slices.Clone(perm)
			SortRevisionsCanonically(s)
			require.Equal([]Revision{a, b, c}, s)
		})
	}

	{
		s := []Revision{a, a, a}
		SortRevisionsCanonically(s)
		require.Equal([]Revision{a, a, a}, s)
	}
	{
		s := []Revision{a, a}
		SortRevisionsCanonically(s)
		require.Equal([]Revision{a, a}, s)
	}
	{
		s := []Revision{a}
		SortRevisionsCanonically(s)
		require.Equal([]Revision{a}, s)
	}
	{
		s := []Revision{}
		SortRevisionsCanonically(s)
		require.Empty(s)
	}
}

func TestExecuteCleanupStrategy(t *testing.T) {
	require := require.New(t)

	logger := zerolog.New(os.Stdout)
	ctx := context.TODO()
	now := rfc3339(t, "2026-09-22T17:00:00.000Z")
	a := newPlainRevision("a", rfc3339(t, "2026-09-22T16:49:00.000Z"), 200)
	b := newPlainRevision("b", rfc3339(t, "2026-09-22T16:50:00.000Z"), 300)
	c := newPlainRevision("c", rfc3339(t, "2026-09-22T16:51:00.000Z"), 100)
	revs := []Revision{a, b, c}

	var strategy CleanupStrategyFunc = func(now time.Time, revs []Revision, ctx context.Context, logger *zerolog.Logger) (Plan, error) {
		plan := NewSimplePlan()
		for _, rev := range revs {
			if rev.Size() > 150 {
				plan.Delete(rev)
			} else {
				plan.Retain(rev)
			}
		}
		return plan, nil
	}

	deleted := []Revision{}
	err := ExecuteCleanupStrategy(now, strategy, revs, ctx, &logger, func(rev Revision) error {
		deleted = append(deleted, rev)
		return nil
	})
	require.NoError(err)
	require.Equal([]Revision{b, a}, deleted)
}

// Revisions with the same time are ordered by the collision suffix of their keys, newest first,
// whatever order they come in: posix renames the existing revision to the next ".<N>" when a
// new one gets the same key.
func TestSortRevisionsWithTheSameTime(t *testing.T) {
	ts := "2026-10-14T10:00:00.123456789Z"
	at := rfc3339(t, ts)
	newestFirst := []string{"n.REV." + ts, "n.REV." + ts + ".10", "n.REV." + ts + ".2", "n.REV." + ts + ".1"}
	for range 20 {
		revs := make([]Revision, len(newestFirst))
		for i, key := range newestFirst {
			revs[i] = newPlainRevision(key, at, 1)
		}
		rand.Shuffle(len(revs), func(i, j int) { revs[i], revs[j] = revs[j], revs[i] })

		SortRevisionsCanonically(revs)

		keys := make([]string, len(revs))
		for i, rev := range revs {
			keys[i] = rev.Key()
		}
		require.Equal(t, newestFirst, keys)
	}

	// keys without a collision suffix are ordered by key, so the order is still deterministic
	a, b := newPlainRevision("a.REV."+ts, at, 1), newPlainRevision("b.REV."+ts, at, 1)
	require.Negative(t, CompareNewestFirst(a, b))
	require.Positive(t, CompareNewestFirst(b, a))
}

func TestParseDuration(t *testing.T) {
	for _, tt := range []struct {
		input    string
		expected time.Duration
	}{
		{"6w", time.Duration(6) * 24 * 7 * time.Hour},
		{"5d", time.Duration(5) * 24 * time.Hour},
		{"4h", time.Duration(4) * time.Hour},
		{"3m", time.Duration(3) * time.Minute},
		{"2s", time.Duration(2) * time.Second},
	} {
		t.Run(fmt.Sprintf("%s: %s", t.Name(), tt.input), func(t *testing.T) {
			require := require.New(t)
			result, err := parseDuration(tt.input)
			require.NoError(err)
			require.Equal(tt.expected, result)
		})
	}

	{
		_, err := parseDuration("")
		require.Error(t, err)
	}

	{
		_, err := parseDuration("123")
		require.Error(t, err)
	}

	// the largest durations a time.Duration can hold, and one more
	for unit, limit := range map[string]int64{"w": 15250, "d": 106751, "h": 2562047, "m": 153722867, "s": 9223372036} {
		d, err := parseDuration(fmt.Sprintf("%d%s", limit, unit))
		require.NoError(t, err, unit)
		require.Positive(t, d, unit)
		_, err = parseDuration(fmt.Sprintf("%d%s", limit+1, unit))
		require.ErrorContains(t, err, "too large", unit)
	}
}

func TestComputeCutoffFromRetentionAlignsToMidnightAcrossDST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("time zone data not available: %v", err)
	}
	// daylight saving time ends on 2026-10-25 in Europe/Berlin, that day has 25 hours
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, berlin)

	require.Equal(t, time.Date(2026, 10, 25, 0, 0, 0, 0, berlin), computeCutoffFromRetention(now, -24*time.Hour, true))
	require.Equal(t, time.Date(2026, 10, 19, 0, 0, 0, 0, berlin), computeCutoffFromRetention(now, -7*24*time.Hour, true))
	require.Equal(t, time.Date(2026, 10, 26, 0, 0, 0, 0, berlin), computeCutoffFromRetention(now, 0, true))
	// without alignment, the duration is a fixed time span
	require.Equal(t, now.Add(-24*time.Hour), computeCutoffFromRetention(now, -24*time.Hour, false))
}

// In "auto, N", the number of versions applies before "auto", not after (see
// NewRevisionCleanupStrategy): "auto, 10" can keep fewer versions than "auto" alone.
func TestAutoThenCountAppliesTheCountFirst(t *testing.T) {
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)
	yesterday := now.Add(-21 * time.Hour)
	revs := []Revision{}
	// the 10 newest versions, all saved within the same minute yesterday
	for i := range 10 {
		revs = append(revs, newPlainRevision(fmt.Sprintf("new%d", i), yesterday.Add(time.Duration(i)*time.Second), 1))
	}
	// and 5 older ones, a day apart
	for i := range 5 {
		revs = append(revs, newPlainRevision(fmt.Sprintf("old%d", i), now.Add(-time.Duration(i+3)*24*time.Hour), 1))
	}
	SortRevisionsCanonically(revs)
	kept := func(config string) int {
		s, err := NewRevisionCleanupStrategy(config)
		require.NoError(t, err)
		plan, err := s.Cleanup(now, revs, context.Background(), &zerolog.Logger{})
		require.NoError(t, err)
		return len(revs) - len(plan.Deleted())
	}

	// one per hour within the last 24 hours, one per day before that
	require.Equal(t, 6, kept("auto"))
	// the 10 newest only, and of those one per hour
	require.Equal(t, 1, kept("auto, 10"))
}
