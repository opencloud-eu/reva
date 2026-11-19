// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestNewDurationDeletionRevisionCleanupStrategyFromString(t *testing.T) {
	now := rfc3339(t, "2026-09-28T15:00:00.000Z")
	revs := []Revision{
		newPlainRevision("a", rfc3339(t, "2026-09-28T14:00:00.000Z"), 1), // -1h
		newPlainRevision("b", rfc3339(t, "2026-09-28T00:00:01.000Z"), 2), // same day, midnight
		newPlainRevision("c", rfc3339(t, "2026-09-27T14:00:00.000Z"), 1), // -1d1h
		newPlainRevision("d", rfc3339(t, "2026-09-26T14:00:00.000Z"), 1), // -2d1h
		newPlainRevision("e", rfc3339(t, "2026-09-25T14:00:00.000Z"), 1), // -3d1h
	}

	for _, tt := range []struct {
		input             string
		expectedDeletions []string
	}{
		{"1d", []string{"c", "d", "e"}},
		{"1D", []string{"d", "e"}},
		{"50m", []string{"a", "b", "c", "d", "e"}},
		{"1h", []string{"b", "c", "d", "e"}},
		{"1w", []string{}},
		{"0D", []string{"c", "d", "e"}},
		{"2D", []string{"e"}},
		{"1W", []string{}},
	} {
		t.Run(fmt.Sprintf("%s: %q", t.Name(), tt.input), func(t *testing.T) {
			require := require.New(t)
			strategy, err := newDurationDeletionRevisionCleanupStrategyFromString(tt.input)
			require.NoError(err)

			r := slices.Clone(revs)
			checkRevs(t, r)

			logger := zerolog.New(os.Stdout)
			plan, err := strategy.Cleanup(now, r, context.TODO(), &logger)
			require.NoError(err)

			require.Equal(tt.expectedDeletions, keysOf(plan.Deleted()))
		})
	}

	for _, tt := range []struct {
		input string
	}{
		{""},
		{"auto"},
		{"disabled"},
		{"5"},
		{"xyz"},
		// only durations in days or weeks can be aligned to midnight
		{"1h="},
		{"5m="},
		{"3s="},
		{"5="},
	} {
		t.Run(fmt.Sprintf("%s: %q", t.Name(), tt.input), func(t *testing.T) {
			strategy, err := newDurationDeletionRevisionCleanupStrategyFromString(tt.input)
			require.NoError(t, err)
			require.Nil(t, strategy)
		})
	}
}
