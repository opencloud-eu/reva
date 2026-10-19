// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

var _ = Describe("RevisionCleanupStrategy", func() {
	var (
		now      time.Time
		revs     []Revision
		logger   *zerolog.Logger
		strategy RevisionCleanupStrategy
	)

	BeforeEach(func() {
		rfc3339 := func(s string) time.Time {
			result, err := time.Parse(time.RFC3339, s)
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		zl := zerolog.New(os.Stdout)
		logger = &zl

		now = rfc3339("2026-09-28T15:00:00.000Z")

		revs = []Revision{
			// three revisions that are older than an hour, but not older than a day
			newPlainRevision("a1", rfc3339("2026-09-28T13:59:00.000Z"), 1), // -1h
			newPlainRevision("a2", rfc3339("2026-09-28T13:58:00.000Z"), 1), // -1h
			newPlainRevision("a3", rfc3339("2026-09-28T13:57:00.000Z"), 1), // -1h
			// three revisions that are older than a day, but not older than two days
			newPlainRevision("b1", rfc3339("2026-09-27T14:00:00.000Z"), 1), // -1d1h
			newPlainRevision("b2", rfc3339("2026-09-27T13:59:00.000Z"), 1), // -1d1h
			newPlainRevision("b3", rfc3339("2026-09-27T13:58:00.000Z"), 1), // -1d1h
			// three revisions that are older than two days, but not older than three days
			newPlainRevision("c1", rfc3339("2026-09-26T14:00:00.000Z"), 1), // -2d1h
			newPlainRevision("c2", rfc3339("2026-09-26T13:59:00.000Z"), 1), // -2d1h
			newPlainRevision("c3", rfc3339("2026-09-26T13:58:00.000Z"), 1), // -2d1h
			// three revisions that are older than three days
			newPlainRevision("d1", rfc3339("2026-09-25T14:00:00.000Z"), 1), // -3d1h
			newPlainRevision("d2", rfc3339("2026-09-25T13:59:00.000Z"), 1), // -3d1h
			newPlainRevision("d3", rfc3339("2026-09-25T13:58:00.000Z"), 1), // -3d1h
		}
	})

	Context("using 'auto'", func() {
		JustBeforeEach(func() {
			var err error
			strategy, err = NewRevisionCleanupStrategy("auto")
			Expect(err).NotTo(HaveOccurred())
			Expect(strategy).NotTo(BeNil())
		})
		Describe("the strategy", func() {
			var plan Plan
			It("does not produce an error", func() {
				var err error
				plan, err = strategy.Cleanup(now, revs, context.TODO(), logger)
				Expect(err).NotTo(HaveOccurred())
			})

			It("deletes all but the newest revision in each bucket", func() {
				Expect(keysOf(plan.Deleted())).To(ConsistOf(
					"a2", "a3", // keeping the youngest in the 1h bucket (a1)
					"b2", "b3", // keeping the youngest in the 1d bucket (b1)
					"c2", "c3", // keeping the youngest in the 2d bucket (c1)
					"d2", "d3", // keeping the youngest in the 3d bucket (d1)
				))
			})
			It("retains the newest revision in each bucket", func() {
				Expect(keysOf(plan.Retained())).To(ConsistOf(
					"a1", "b1", "c1", "d1",
				))
			})
			It("does not safeguard any revisions", func() {
				Expect(plan.Safeguarded()).To(BeEmpty())
			})
		})
	})

	Context("using '2d, auto'", func() {
		JustBeforeEach(func() {
			var err error
			strategy, err = NewRevisionCleanupStrategy("2d, auto")
			Expect(err).NotTo(HaveOccurred())
		})
		Describe("the strategy", func() {
			var plan Plan
			It("does not produce an error", func() {
				var err error
				plan, err = strategy.Cleanup(now, revs, context.TODO(), logger)
				Expect(err).NotTo(HaveOccurred())
			})
			It("safeguards the revisions that are up to 2 days old", func() {
				Expect(keysOf(plan.Safeguarded())).To(ConsistOf(
					"a1", "a2", "a3", "b1", "b2", "b3",
				))
			})
			It("retains the newest revision in each bucket", func() {
				Expect(keysOf(plan.Retained())).To(ConsistOf(
					"c1", "d1",
				))
			})
			It("deletes the revisions that are older than the newest in each bucket", func() {
				Expect(keysOf(plan.Deleted())).To(ConsistOf(
					"c2", "c3", // keeping the youngest in the 2d bucket (c1)
					"d2", "d3", // keeping the youngest in the 3d bucket (d1)
				))
			})
		})
	})

	Context("using 'auto, 1d'", func() {
		JustBeforeEach(func() {
			var err error
			strategy, err = NewRevisionCleanupStrategy("auto, 1d")
			Expect(err).NotTo(HaveOccurred())
		})
		Describe("the strategy", func() {
			// should delete:
			// b1, b2, b3: older than 1d
			// c1, c2, c3: older than 1d
			// d1, d2, d3: older than 1d
			// a2, a3: keep only a1 as the newest in the 1h bucket

			var plan Plan
			It("does not produce an error", func() {
				var err error
				plan, err = strategy.Cleanup(now, revs, context.TODO(), logger)
				Expect(err).NotTo(HaveOccurred())
			})
			It("does not safeguard any revision", func() {
				Expect(plan.Safeguarded()).To(BeEmpty())
			})
			It("retains the newest revision in the 1h bucket", func() {
				Expect(keysOf(plan.Retained())).To(ConsistOf(
					"a1",
				))
			})
			It("deletes the revisions that are older than 1d and older than the newest in each bucket", func() {
				Expect(keysOf(plan.Deleted())).To(ConsistOf(
					"b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3", // older then 1d
					"a2", "a3", // keeping the youngest in the 1h bucket (a1)
				))
			})
		})
	})

	Context("using 'auto, 2d'", func() {
		JustBeforeEach(func() {
			var err error
			strategy, err = NewRevisionCleanupStrategy("auto, 2d")
			Expect(err).NotTo(HaveOccurred())
		})
		Describe("the strategy", func() {
			// should delete:
			// c1, c2, c3: older than 2d
			// d1, d2, d3: older than 2d
			// a2, a3: keep only a1 as the newest in the 1h bucket
			// b2, b3: keep only b1 as the newest in the 1d bucket

			var plan Plan
			It("does not produce an error", func() {
				var err error
				plan, err = strategy.Cleanup(now, revs, context.TODO(), logger)
				Expect(err).NotTo(HaveOccurred())
			})
			It("does not safeguard any revision", func() {
				Expect(plan.Safeguarded()).To(BeEmpty())
			})
			It("retains the newest revision in the 1h and in the 1d buckets", func() {
				Expect(keysOf(plan.Retained())).To(ConsistOf(
					"a1",
					"b1",
				))
			})
			It("deletes the revisions that are older than 2d and older than the newest in each bucket", func() {
				Expect(keysOf(plan.Deleted())).To(ConsistOf(
					"c1", "c2", "c3", "d1", "d2", "d3", // older then 1d
					"a2", "a3", // keeping the youngest in the 1h bucket (a1)
					"b2", "b3", // keeping the youngest in the 1d bucket (b1)
				))
			})
		})
	})

	// The fixture has 12 revisions, newest first:
	//   a1-a3: about 1h old, b1-b3: about 1d1h old, c1-c3: about 2d1h old, d1-d3: about 3d1h old
	// Each entry lists the revisions expected to be safeguarded, retained and deleted.
	DescribeTable("plans",
		func(config string, safeguarded, retained, deleted []string) {
			strategy, err := NewRevisionCleanupStrategy(config)
			Expect(err).NotTo(HaveOccurred())
			plan, err := strategy.Cleanup(now, revs, context.TODO(), logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(keysOf(plan.Safeguarded())).To(ConsistOf(safeguarded))
			Expect(keysOf(plan.Retained())).To(ConsistOf(retained))
			Expect(keysOf(plan.Deleted())).To(ConsistOf(deleted))
		},

		// "X": plain deletion, no thinning
		Entry("'2d' deletes the revisions older than 2 days", "2d",
			[]string{},
			[]string{"a1", "a2", "a3", "b1", "b2", "b3"},
			[]string{"c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'4' keeps the 4 newest revisions", "4",
			[]string{},
			[]string{"a1", "a2", "a3", "b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		// deliberately supported: lets admins get rid of all accumulated revisions
		Entry("'0' deletes all revisions", "0",
			[]string{},
			[]string{},
			[]string{"a1", "a2", "a3", "b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1d, 0' deletes all revisions older than 1 day", "1d, 0",
			[]string{"a1", "a2", "a3"},
			[]string{},
			[]string{"b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),

		// "X1, X2" with a count and a duration
		Entry("'4, 2d' deletes the revisions older than 2 days but keeps the 4 newest", "4, 2d",
			[]string{"a1", "a2", "a3", "b1"},
			[]string{"b2", "b3"},
			[]string{"c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'8, 2d' keeps the 8 newest even if they are older than 2 days", "8, 2d",
			[]string{"a1", "a2", "a3", "b1", "b2", "b3", "c1", "c2"},
			[]string{},
			[]string{"c3", "d1", "d2", "d3"}),
		Entry("'1d, 4' keeps 4 revisions in total, the ones younger than 1 day included", "1d, 4",
			[]string{"a1", "a2", "a3"},
			[]string{"b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1d, 2' never deletes revisions younger than 1 day, even beyond the limit", "1d, 2",
			[]string{"a1", "a2", "a3"},
			[]string{},
			[]string{"b1", "b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),

		// "X1, auto, X2"
		Entry("'1d, auto, 2d' thins out the revisions between 1 and 2 days", "1d, auto, 2d",
			[]string{"a1", "a2", "a3"},
			[]string{"b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'2, auto, 6' thins out the revisions between the 2 newest and the 6 newest", "2, auto, 6",
			[]string{"a1", "a2"},
			[]string{"a3", "b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'4, auto, 2d' mixes a count and a duration", "4, auto, 2d",
			[]string{"a1", "a2", "a3", "b1"},
			[]string{"b2"},
			[]string{"b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1d, auto, 4' counts the safeguarded revisions towards the limit", "1d, auto, 4",
			[]string{"a1", "a2", "a3"},
			[]string{"b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),

		// durations aligned to midnight: now is 2026-09-28 15:00, so "1D" is since 2026-09-27 00:00
		Entry("'1D' deletes the revisions from before yesterday", "1D",
			[]string{},
			[]string{"a1", "a2", "a3", "b1", "b2", "b3"},
			[]string{"c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1D, auto, 2D' thins out the revisions of the day before yesterday", "1D, auto, 2D",
			[]string{"a1", "a2", "a3", "b1", "b2", "b3"},
			[]string{"c1"},
			[]string{"c2", "c3", "d1", "d2", "d3"}),
		Entry("'4, 1D' mixes a count and a midnight aligned duration", "4, 1D",
			[]string{"a1", "a2", "a3", "b1"},
			[]string{"b2", "b3"},
			[]string{"c1", "c2", "c3", "d1", "d2", "d3"}),

		// the spaces around the commas are optional: the same plans as with "4, 2d", "1d, auto, 2d",
		// "2, auto, 6" and "1D, auto, 2D" above
		Entry("'4,2d' without spaces", "4,2d",
			[]string{"a1", "a2", "a3", "b1"},
			[]string{"b2", "b3"},
			[]string{"c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1d,auto,2d' without spaces", "1d,auto,2d",
			[]string{"a1", "a2", "a3"},
			[]string{"b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("' 2 ,auto ,  6 ' with uneven spaces", " 2 ,auto ,  6 ",
			[]string{"a1", "a2"},
			[]string{"a3", "b1"},
			[]string{"b2", "b3", "c1", "c2", "c3", "d1", "d2", "d3"}),
		Entry("'1D,\tauto,\t2D' with tabs", "1D,\tauto,\t2D",
			[]string{"a1", "a2", "a3", "b1", "b2", "b3"},
			[]string{"c1"},
			[]string{"c2", "c3", "d1", "d2", "d3"}),
	)

	DescribeTable("accepts",
		func(config string) {
			_, err := NewRevisionCleanupStrategy(config)
			Expect(err).ToNot(HaveOccurred())
		},
		// equal only at midnight
		Entry("'X1, auto, X2' with X2 the same duration as X1, aligned to midnight", "1d, auto, 1D"),
		// "1D" can reach slightly beyond "2d" on the night daylight saving time ends, see valueRange
		Entry("'X1, auto, X2' with a midnight aligned X1 just below X2", "1D, auto, 2d"),
		// the spaces around the commas are optional, in every form
		Entry("'X, auto' without spaces", "14d,auto"),
		Entry("'auto, X' without spaces", "auto,90d"),
		Entry("'auto, N' without spaces", "auto,50"),
		Entry("'X1, X2' without spaces", "5,30d"),
		Entry("'X1, auto, X2' without spaces", "7d,auto,90d"),
		Entry("'X1, auto, X2' midnight aligned, without spaces", "1D,auto,30d"),
		Entry("uneven spaces", "7d , auto ,  90d"),
		Entry("spaces before and after", "  auto, 90d  "),
		Entry("tabs", "auto,\t90d"),
		Entry("'auto' with spaces around it", " auto "),
		Entry("'X' with spaces around it", " 30d "),
	)

	DescribeTable("rejects",
		func(config string, errorContains string) {
			_, err := NewRevisionCleanupStrategy(config)
			Expect(err).To(MatchError(ContainSubstring(errorContains)))
		},
		Entry("two durations", "1d, 2d", `the same as "2d"`),
		Entry("two durations, larger first", "30d, 7d", `the same as "30d"`),
		Entry("two counts", "5, 10", `the same as "10"`),
		Entry("two counts, larger first", "10, 5", `the same as "10"`),
		Entry("'X1, auto, X2' with the larger duration first", "2d, auto, 1d", `must be smaller than "1d"`),
		Entry("'X1, auto, X2' with equal durations", "1w, auto, 7d", `must be smaller than "7d"`),
		Entry("'X1, auto, X2' with the larger count first", "6, auto, 2", `must be smaller than "2"`),
		Entry("'X1, auto, X2' where X1 is only sometimes smaller", "1D, auto, 30h", `at any time of day`),
		Entry("'X1, auto, X2' with a midnight aligned X1 overlapping X2", "1D, auto, 1d", `must be smaller than "1d"`),
		Entry("two durations, one aligned to midnight", "1D, 3d", `the same as "3d"`),
		Entry("two durations whose order depends on the time of day", "1D, 30h", `depending on the time of day`),
		Entry("a duration and the same one aligned to midnight", "1d, 1D", `the same as "1D";`),
		Entry("a duration aligned to midnight and the same one", "1D, 1d", `the same as "1D";`),
		Entry("the same duration aligned to midnight twice", "1D, 1D", `the same as "1D";`),
		Entry("midnight alignment of hours", "1H", "unsupported revision cleanup strategy"),
		Entry("midnight alignment of minutes", "auto, 30M", "unsupported revision cleanup strategy"),
		Entry("midnight alignment of seconds", "30S, auto", "unsupported revision cleanup strategy"),
		Entry("a '=' after days", "1d=", "unsupported revision cleanup strategy"),
		Entry("a '=' after weeks", "auto, 2w=", "unsupported revision cleanup strategy"),
		Entry("a '=' after a count", "5=, auto", "unsupported revision cleanup strategy"),
		Entry("unknown forms", "auto, auto", "unsupported revision cleanup strategy"),
		Entry("a number of versions too large for an int", "99999999999999999999", "out of range"),
		// these would overflow a time.Duration, and the cutoff would end up in the future
		Entry("a duration too large", "auto, 200000d", "too large"),
		Entry("a retention too large", "20000w, auto", "too large"),
		Entry("a duration too large in seconds", "10000000000s", "too large"),
		Entry("a duration too large in a chain", "15251w, auto, 1d", "too large"),
		Entry("a duration too large for an int", "auto, 99999999999999999999d", "out of range"),
		Entry("a too large number in a same-kind check", "5, 99999999999999999999", "out of range"),
		Entry("too many values", "1d, auto, 2d, 3d", "unsupported revision cleanup strategy"),
		// handled by the callers, see NewRevisionCleanupStrategy
		Entry("disabled", "disabled", "unsupported revision cleanup strategy"),
		Entry("empty", "", "unsupported revision cleanup strategy"),
	)
})
