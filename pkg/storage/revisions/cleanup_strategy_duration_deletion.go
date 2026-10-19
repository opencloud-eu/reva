// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// A RevisionCleanupStrategy that deletes files that are older than a given duration.
//
// With truncateToMidnight ("ND" or "NW" instead of "Nd" or "Nw"), the cutoff is aligned to
// local midnight instead of being exactly the duration before now, see computeCutoffFromRetention.
type durationDeletionRevisionCleanupStrategy struct {
	retention          time.Duration
	truncateToMidnight bool
}

var _ RevisionCleanupStrategy = &durationDeletionRevisionCleanupStrategy{}

func newDurationDeletionRevisionCleanupStrategy(retention time.Duration, truncateToMidnight bool) (RevisionCleanupStrategy, error) {
	return &durationDeletionRevisionCleanupStrategy{
		retention:          retention,
		truncateToMidnight: truncateToMidnight,
	}, nil
}

func newDurationDeletionRevisionCleanupStrategyFromString(config string) (RevisionCleanupStrategy, error) {
	if matches, duration, truncateToMidnight, err := durationFromConfig(config); !matches {
		return nil, nil
	} else if err != nil {
		return nil, err
	} else {
		return newDurationDeletionRevisionCleanupStrategy(duration, truncateToMidnight)
	}
}

func (s *durationDeletionRevisionCleanupStrategy) Cleanup(now time.Time, revs []Revision, ctx context.Context, logger *zerolog.Logger) (Plan, error) {
	cutoff := computeCutoffFromRetention(now, -s.retention, s.truncateToMidnight)
	plan := NewSimplePlan()
	for _, rev := range revs {
		if rev.LastModificationTime().Before(cutoff) {
			plan.Delete(rev)
		} else {
			plan.Retain(rev)
		}
	}
	return plan, nil
}
