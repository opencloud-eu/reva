// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// A RevisionCleanupStrategy that retains files that are older than a given duration.
//
// With truncateToMidnight ("ND" or "NW" instead of "Nd" or "Nw"), the cutoff is aligned to
// local midnight instead of being exactly the duration before now, see computeCutoffFromRetention.
type durationRetentionRevisionCleanupStrategy struct {
	retention          time.Duration
	truncateToMidnight bool
}

var _ RevisionCleanupStrategy = &durationRetentionRevisionCleanupStrategy{}

func newDurationRetentionRevisionCleanupStrategy(retention time.Duration, truncateToMidnight bool) (RevisionCleanupStrategy, error) {
	return &durationRetentionRevisionCleanupStrategy{
		retention:          retention,
		truncateToMidnight: truncateToMidnight,
	}, nil
}

func newDurationRetentionRevisionCleanupStrategyFromString(config string) (RevisionCleanupStrategy, error) {
	if matches, duration, truncateToMidnight, err := durationFromConfig(config); !matches {
		return nil, nil
	} else if err != nil {
		return nil, err
	} else {
		return newDurationRetentionRevisionCleanupStrategy(duration, truncateToMidnight)
	}
}

func (s *durationRetentionRevisionCleanupStrategy) Cleanup(now time.Time, revs []Revision, ctx context.Context, logger *zerolog.Logger) (Plan, error) {
	cutoff := computeCutoffFromRetention(now, -s.retention, s.truncateToMidnight)
	plan := NewSimplePlan()
	for _, rev := range revs {
		if rev.LastModificationTime().After(cutoff) {
			// revision is younger than the cutoff time:
			// do nothing in order to drop the revision from the revisions that
			// need to be processed by following-up strategies
			plan.Safeguard(rev)
		} else {
			// revision is older than the cutoff time:
			// mark it for retention, in order to be processed by any
			// strategy that comes next
			plan.Retain(rev)
		}
	}
	return plan, nil
}
