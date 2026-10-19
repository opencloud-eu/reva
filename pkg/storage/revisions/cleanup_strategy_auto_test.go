// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func explain(t *testing.T, plan Plan, explanation map[string]bucketExplanation) {
	var buf strings.Builder
	fmt.Fprintf(&buf, "plan: retain=[%s] delete=[%s]\n", strings.Join(keysOf(plan.Retained()), ", "), strings.Join(keysOf(plan.Deleted()), ", "))
	for _, policy := range defaultPolicy {
		bucket := explanation[policy.name]
		if len(bucket.retain)+len(bucket.delete) < 1 {
			continue
		}
		fmt.Fprintf(&buf, "* %s:\n", policy.name)
		if len(bucket.retain) > 0 {
			fmt.Fprintf(&buf, "  - retain: %s\n", strings.Join(keysOf(bucket.retain), ", "))
		}
		if len(bucket.delete) > 0 {
			fmt.Fprintf(&buf, "  - delete: %s\n", strings.Join(keysOf(bucket.delete), ", "))
		}
	}
	t.Log("\n" + buf.String())
}

func TestEvaluationRetentionOneRevInEachSlotKeepsAll(t *testing.T) {
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
	plan, explanation := evaluateRetention(revs, defaultPolicy, now, true)
	explain(t, plan, explanation)

	require.ElementsMatch([]Revision{a, b, c, d, e}, plan.Retained())
	require.Empty(plan.Deleted())
}

func TestEvaluationRetentionMultipleRevsInFirstBucketKeepsOnlyTheFirst(t *testing.T) {
	require := require.New(t)

	now := rfc3339(t, "2026-09-22T11:00:00.000Z")
	a := newPlainRevision("a", rfc3339(t, "2026-09-22T10:59:30.000Z"), 1) // within a minute
	b := newPlainRevision("b", rfc3339(t, "2026-09-22T10:59:31.000Z"), 1) // within a minute
	c := newPlainRevision("c", rfc3339(t, "2026-09-22T10:59:32.000Z"), 1) // within a minute
	revs := []Revision{a, b, c}
	SortRevisionsCanonically(revs)

	checkRevs(t, revs)
	plan, explanation := evaluateRetention(revs, defaultPolicy, now, true)
	explain(t, plan, explanation)

	require.ElementsMatch([]Revision{c}, plan.Retained())
	require.ElementsMatch([]Revision{a, b}, plan.Deleted())
}

func ensureAllWithinRange(t *testing.T, instant time.Time, startOffset time.Duration, endOffset time.Duration, revs ...Revision) {
	t.Helper()
	if startOffset > endOffset {
		t.Fatalf("invalid relative window: startOffset %v is greater than endOffset %v", startOffset, endOffset)
	}
	windowStart := instant.Add(startOffset)
	windowEnd := instant.Add(endOffset)

	for i, rev := range revs {
		modTime := rev.LastModificationTime()
		if modTime.Before(windowStart) || modTime.After(windowEnd) {
			t.Fatalf(
				"revision at index %d with LastModificationTime %s falls outside relative window [%s, %s] (instant: %s, offsets: [%v, %v])",
				i,
				modTime.Format(time.RFC3339),
				windowStart.Format(time.RFC3339),
				windowEnd.Format(time.RFC3339),
				instant.Format(time.RFC3339),
				startOffset,
				endOffset,
			)
		}
	}
}

func ensureWithin(t *testing.T, d time.Duration, revs ...Revision) {
	t.Helper()

	if len(revs) == 0 {
		return
	}

	minTime := revs[0].LastModificationTime()
	maxTime := revs[0].LastModificationTime()

	for _, rev := range revs[1:] {
		tm := rev.LastModificationTime()
		if tm.Before(minTime) {
			minTime = tm
		}
		if tm.After(maxTime) {
			maxTime = tm
		}
	}

	span := maxTime.Sub(minTime)
	if span > d {
		t.Fatalf(
			"revisions span %v (earliest: %s, latest: %s), which exceeds allowed duration of %v",
			span,
			minTime.Format(time.RFC3339),
			maxTime.Format(time.RFC3339),
			d,
		)
	}
}

func TestEvaluationRetentionWithoutAnyRevisions(t *testing.T) {
	require := require.New(t)
	now := rfc3339(t, "2026-09-22T11:00:00.000Z")
	revs := []Revision{}
	checkRevs(t, revs)
	plan, explanation := evaluateRetention(revs, defaultPolicy, now, true)
	explain(t, plan, explanation)
	require.Empty(plan.Retained())
	require.Empty(plan.Deleted())
}

func TestEvaluationRetentionWithThreeRevisionsInEachBucket(t *testing.T) {
	require := require.New(t)

	now := rfc3339(t, "2026-09-22T11:00:00.000Z")

	// === 1. revisions that have an mtime within 1 minute of now.

	// they are bucketed by groups of 10 seconds.
	// for each of those buckets, only the newest is retained.
	ax1 := newPlainRevision("ax1", rfc3339(t, "2026-09-22T10:59:30.000Z"), 1) // within a minute | 10sec bucket x
	ax2 := newPlainRevision("ax2", rfc3339(t, "2026-09-22T10:59:31.000Z"), 1) // within a minute | 10sec bucket x
	ax3 := newPlainRevision("ax3", rfc3339(t, "2026-09-22T10:59:32.000Z"), 1) // within a minute | 10sec bucket x <- RETAINED

	ensureWithin(t, time.Duration(10)*time.Second, ax1, ax2, ax3) // ensure they are all within a 10sec bucket

	ensureAllWithinRange(t, now, // ensure they are all within
		-time.Duration(1)*time.Minute, // the last minute of now
		0,
		ax1, ax2, ax3)

	// this is another bucket with three revisions within the same minute, but in another bucket of 10sec
	ay1 := newPlainRevision("ay1", rfc3339(t, "2026-09-22T10:59:40.000Z"), 1) // within a minute | 10sec bucket y
	ay2 := newPlainRevision("ay2", rfc3339(t, "2026-09-22T10:59:41.000Z"), 1) // within a minute | 10sec bucket y
	ay3 := newPlainRevision("ay3", rfc3339(t, "2026-09-22T10:59:42.000Z"), 1) // within a minute | 10sec bucket y <-- RETAINED
	ensureWithin(t, time.Duration(10)*time.Second, ay1, ay2, ay3)
	ensureAllWithinRange(t, now, -time.Duration(1)*time.Minute, 0, ay1, ay2, ay3)

	// this is another bucket with three revisions within the same minute, but in another bucket of 10sec
	az1 := newPlainRevision("az1", rfc3339(t, "2026-09-22T10:59:51.000Z"), 1) // within a minute | 10sec bucket z
	az2 := newPlainRevision("az2", rfc3339(t, "2026-09-22T10:59:52.000Z"), 1) // within a minute | 10sec bucket z
	az3 := newPlainRevision("az3", rfc3339(t, "2026-09-22T10:59:53.000Z"), 1) // within a minute | 10sec bucket z <-- RETAINED
	ensureWithin(t, time.Duration(10)*time.Second, az1, az2, az3)
	ensureAllWithinRange(t, now, -time.Duration(1)*time.Minute, 0, az1, az2, az3)

	// === 2. revisions that have an mtime within 1 hour of now, but more than 1 minute.

	// they are bucketed by groups of 1 minute.
	// for each of those buckets, only the newest is retained.
	bx1 := newPlainRevision("bx1", rfc3339(t, "2026-09-22T10:05:31.000Z"), 1) // within an hour | 1m bucket x
	bx2 := newPlainRevision("bx2", rfc3339(t, "2026-09-22T10:05:41.000Z"), 1) // within an hour | 1m bucket x
	bx3 := newPlainRevision("bx3", rfc3339(t, "2026-09-22T10:05:51.000Z"), 1) // within an hour | 1m bucket x <-- RETAINED

	ensureWithin(t, time.Duration(1)*time.Minute, bx1, bx2, bx3) // ensure they are all within a 1m bucket

	ensureAllWithinRange(t, now, // ensure they are all within
		-time.Duration(1)*time.Hour,   // the last hour
		-time.Duration(1)*time.Minute, // + not within the last minute
		bx1, bx2, bx3)

	// this is another bucket with three revisions within the same hour, but in another bucket of 1m
	by1 := newPlainRevision("by1", rfc3339(t, "2026-09-22T10:16:31.000Z"), 1) // within an hour | 1m bucket y
	by2 := newPlainRevision("by2", rfc3339(t, "2026-09-22T10:16:41.000Z"), 1) // within an hour | 1m bucket y
	by3 := newPlainRevision("by3", rfc3339(t, "2026-09-22T10:16:51.000Z"), 1) // within an hour | 1m bucket y <-- RETAINED
	ensureWithin(t, time.Duration(1)*time.Minute, by1, by2, by3)              // ensure they are all within a 1m bucket
	ensureAllWithinRange(t, now, -time.Duration(1)*time.Hour, -time.Duration(1)*time.Minute, by1, by2, by3)

	// this is another bucket with three revisions within the same hour, but in another bucket of 1m
	bz1 := newPlainRevision("bz1", rfc3339(t, "2026-09-22T10:47:31.000Z"), 1) // within an hour | 1m bucket z
	bz2 := newPlainRevision("bz2", rfc3339(t, "2026-09-22T10:47:41.000Z"), 1) // within an hour | 1m bucket z
	bz3 := newPlainRevision("bz3", rfc3339(t, "2026-09-22T10:47:51.000Z"), 1) // within an hour | 1m bucket z <-- RETAINED
	ensureWithin(t, time.Duration(1)*time.Minute, bz1, bz2, bz3)              // ensure they are all within a 1m bucket
	ensureAllWithinRange(t, now, -time.Duration(1)*time.Hour, -time.Duration(1)*time.Minute, bz1, bz2, bz3)

	// === 3. revisions that have an mtime within 24 hours of now, but more than 1 hour.

	// they are bucketed by groups of 1 hour.
	// for each of those buckets, only the newest is retained.
	cx1 := newPlainRevision("cx1", rfc3339(t, "2026-09-21T11:05:30.000Z"), 1) // within 24 hours | 1h bucket x
	cx2 := newPlainRevision("cx2", rfc3339(t, "2026-09-21T11:15:30.000Z"), 1) // within 24 hours | 1h bucket x
	cx3 := newPlainRevision("cx3", rfc3339(t, "2026-09-21T11:25:30.000Z"), 1) // within 24 hours | 1h bucket x <-- RETAINED

	ensureWithin(t, time.Duration(1)*time.Hour, cx1, cx2, cx3) // ensure they are all within a 1h bucket

	ensureAllWithinRange(t, now, // ensure they are all within
		-time.Duration(1)*24*time.Hour, // the last 24 hours
		-time.Duration(1)*time.Hour,    // + not within the last hour
		cx1, cx2, cx3)

	// this is another bucket with three revisions within the same 24 hours, but in another bucket of 1h
	cy1 := newPlainRevision("cx1", rfc3339(t, "2026-09-21T12:05:30.000Z"), 1) // within 24 hours | 1h bucket y
	cy2 := newPlainRevision("cy2", rfc3339(t, "2026-09-21T12:15:30.000Z"), 1) // within 24 hours | 1h bucket y
	cy3 := newPlainRevision("cy3", rfc3339(t, "2026-09-21T12:25:30.000Z"), 1) // within 24 hours | 1h bucket y <-- RETAINED
	ensureWithin(t, time.Duration(1)*time.Hour, cy1, cy2, cy3)
	ensureAllWithinRange(t, now, -time.Duration(1)*24*time.Hour, -time.Duration(1)*time.Hour, cy1, cy2, cy3)

	// this is another bucket with three revisions within the same 24 hours, but in another bucket of 1h
	cz1 := newPlainRevision("cz1", rfc3339(t, "2026-09-21T18:05:30.000Z"), 1) // within 24 hours | 1h bucket z
	cz2 := newPlainRevision("cz2", rfc3339(t, "2026-09-21T18:15:30.000Z"), 1) // within 24 hours | 1h bucket z
	cz3 := newPlainRevision("cz3", rfc3339(t, "2026-09-21T18:25:30.000Z"), 1) // within 24 hours | 1h bucket z <-- RETAINED
	ensureWithin(t, time.Duration(1)*time.Hour, cz1, cz2, cz3)
	ensureAllWithinRange(t, now, -time.Duration(1)*24*time.Hour, -time.Duration(1)*time.Hour, cz1, cz2, cz3)

	// === 4. revisions that have an mtime within 30 days of now, but more than 24 hours.

	// they are bucketed by groups of 1 day.
	// for each of those buckets, only the newest is retained.
	dx1 := newPlainRevision("dx1", rfc3339(t, "2026-08-23T11:05:30.000Z"), 1) // within 30 days | 1d bucket x
	dx2 := newPlainRevision("dx2", rfc3339(t, "2026-08-23T12:05:30.000Z"), 1) // within 30 days | 1d bucket x
	dx3 := newPlainRevision("dx3", rfc3339(t, "2026-08-23T13:05:30.000Z"), 1) // within 30 days | 1d bucket x <-- RETAINED

	ensureWithin(t, time.Duration(1)*24*time.Hour, dx1, dx2, dx3) // ensure they are all within a 24h bucket

	ensureAllWithinRange(t, now, // ensure they are all within
		-time.Duration(1)*24*30*time.Hour, // the last 30 days
		-time.Duration(1)*24*time.Hour,    // + not within the last 24 hours
		dx1, dx2, dx3)

	// this is another bucket with three revisions within the same 30 days, but in another bucket of 24h
	dy1 := newPlainRevision("dx1", rfc3339(t, "2026-08-24T11:05:30.000Z"), 1) // within 30 days | 1d bucket y
	dy2 := newPlainRevision("dx2", rfc3339(t, "2026-08-24T12:05:30.000Z"), 1) // within 30 days | 1d bucket y
	dy3 := newPlainRevision("dx3", rfc3339(t, "2026-08-24T13:05:30.000Z"), 1) // within 30 days | 1d bucket y <-- RETAINED
	ensureWithin(t, time.Duration(1)*24*time.Hour, dy1, dy2, dy3)
	ensureAllWithinRange(t, now, -time.Duration(1)*24*30*time.Hour, -time.Duration(1)*24*time.Hour, dy1, dy2, dy3)

	// this is another bucket with three revisions within the same 30 days, but in another bucket of 24h
	dz1 := newPlainRevision("dx1", rfc3339(t, "2026-09-20T11:05:30.000Z"), 1) // within 30 days | 1d bucket z
	dz2 := newPlainRevision("dx2", rfc3339(t, "2026-09-20T12:05:30.000Z"), 1) // within 30 days | 1d bucket z
	dz3 := newPlainRevision("dx3", rfc3339(t, "2026-09-20T13:05:30.000Z"), 1) // within 30 days | 1d bucket z <-- RETAINED
	ensureWithin(t, time.Duration(1)*24*time.Hour, dz1, dz2, dz3)
	ensureAllWithinRange(t, now, -time.Duration(1)*24*30*time.Hour, -time.Duration(1)*24*time.Hour, dz1, dz2, dz3)

	// === 5. revisions that have an mtime older than 30 days of now.

	// they are bucketed by groups of 1 week.
	// for each of those buckets, only the newest is retained.
	ex1 := newPlainRevision("ex1", rfc3339(t, "2026-07-22T11:00:30.000Z"), 1) // older than 30 days | 1w bucket x
	ex2 := newPlainRevision("ex2", rfc3339(t, "2026-07-23T11:00:30.000Z"), 1) // older than 30 days | 1w bucket x
	ex3 := newPlainRevision("ex3", rfc3339(t, "2026-07-24T11:00:30.000Z"), 1) // older than 30 days | 1w bucket x <-- RETAINED

	ensureWithin(t, time.Duration(1)*24*7*time.Hour, ex1, ex2, ex3) // ensure they are all within a 7d bucket bucket

	ensureAllWithinRange(t, now, // ensure they are all within
		-time.Duration(10000)*24*time.Hour, // (let's use 10 000 days for "unbounded")
		-time.Duration(1)*24*30*time.Hour,  // + not within the last 30 days
		ex1, ex2, ex3)

	// this is another bucket with three revisions older than 30 days, but in another bucket of 1 week
	ey1 := newPlainRevision("ey1", rfc3339(t, "2026-06-01T11:00:30.000Z"), 1) // older than 30 days | 1w bucket y
	ey2 := newPlainRevision("ey2", rfc3339(t, "2026-06-02T11:00:30.000Z"), 1) // older than 30 days | 1w bucket y
	ey3 := newPlainRevision("ey3", rfc3339(t, "2026-06-03T11:00:30.000Z"), 1) // older than 30 days | 1w bucket y <-- RETAINED
	ensureWithin(t, time.Duration(1)*24*7*time.Hour, ey1, ey2, ey3)
	ensureAllWithinRange(t, now, -time.Duration(10000)*24*time.Hour, -time.Duration(1)*24*30*time.Hour, ey1, ey2, ey3)

	// this is another bucket with three revisions older than 30 days, but in another bucket of 1 week
	ez1 := newPlainRevision("ez1", rfc3339(t, "2026-06-08T11:00:30.000Z"), 1) // older than 30 days | 1w bucket z
	ez2 := newPlainRevision("ez2", rfc3339(t, "2026-06-09T11:00:30.000Z"), 1) // older than 30 days | 1w bucket z
	ez3 := newPlainRevision("ez3", rfc3339(t, "2026-06-10T11:00:30.000Z"), 1) // older than 30 days | 1w bucket z <-- RETAINED
	ensureWithin(t, time.Duration(1)*24*7*time.Hour, ez1, ez2, ez3)
	ensureAllWithinRange(t, now, -time.Duration(10000)*24*time.Hour, -time.Duration(1)*24*30*time.Hour, ez1, ez2, ez3)

	// make sure they are all included in this slice
	revs := []Revision{
		ax1, ax2, ax3,
		ay1, ay2, ay3,
		az1, az2, az3,
		bx1, bx2, bx3,
		by1, by2, by3,
		bz1, bz2, bz3,
		cx1, cx2, cx3,
		cy1, cy2, cy3,
		cz1, cz2, cz3,
		dx1, dx2, dx3,
		dy1, dy2, dy3,
		dz1, dz2, dz3,
		ex1, ex2, ex3,
		ey1, ey2, ey3,
		ez1, ez2, ez3,
	}
	SortRevisionsCanonically(revs)

	r := slices.Clone(revs)
	checkRevs(t, r)
	plan, explanation := evaluateRetention(r, defaultPolicy, now, true)
	explain(t, plan, explanation)

	require.ElementsMatch([]Revision{
		ax3, ay3, az3,
		bx3, by3, bz3,
		cx3, cy3, cz3,
		dx3, dy3, dz3,
		ex3, ey3, ez3,
	}, plan.Retained())
	require.ElementsMatch([]Revision{
		ax1, ax2, ay1, ay2, az1, az2,
		bx1, bx2, by1, by2, bz1, bz2,
		cx1, cx2, cy1, cy2, cz1, cz2,
		dx1, dx2, dy1, dy2, dz1, dz2,
		ex1, ex2, ey1, ey2, ez1, ez2,
	}, plan.Deleted())

	{
		strategy, err := newAutoRevisionCleanupStrategyFromString(autoStrategy)
		require.NoError(err)
		r := slices.Clone(revs)
		checkRevs(t, r)

		logger := zerolog.New(os.Stdout)

		plan, err = strategy.Cleanup(now, r, context.TODO(), &logger)
		require.NoError(err)
		require.ElementsMatch([]Revision{
			ax1, ax2, ay1, ay2, az1, az2,
			bx1, bx2, by1, by2, bz1, bz2,
			cx1, cx2, cy1, cy2, cz1, cz2,
			dx1, dx2, dy1, dy2, dz1, dz2,
			ex1, ex2, ey1, ey2, ez1, ez2,
		}, plan.Deleted())
	}

	{
		strategy, err := newAutoRevisionCleanupStrategyFromString("not" + autoStrategy)
		require.NoError(err)
		require.Nil(strategy)
	}
}
