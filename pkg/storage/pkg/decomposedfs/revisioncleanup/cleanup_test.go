// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"slices"
	"testing"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

func TestContentTime(t *testing.T) {
	ts := time.Date(2026, 8, 24, 10, 33, 16, 535011272, time.UTC)
	mtime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		key        string
		want       time.Time
		wantSuffix int
	}{
		{"plain", "a3995a3f.REV." + ts.Format(time.RFC3339Nano), ts, 0},
		{"collision suffix", "a3995a3f.REV." + ts.Format(time.RFC3339Nano) + ".2", ts, 2},
		{"unparsable falls back to mtime", "a3995a3f.REV.garbage", mtime, 0},
		{"no delimiter falls back to mtime", "a3995a3f", mtime, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, suffix := contentTime(&provider.FileVersion{Key: tc.key, Mtime: uint64(mtime.Unix())})
			if !got.Equal(tc.want) || suffix != tc.wantSuffix {
				t.Errorf("contentTime(%q) = %v, %d, want %v, %d", tc.key, got, suffix, tc.want, tc.wantSuffix)
			}
		})
	}
}

func TestCompareContentTime(t *testing.T) {
	ts := time.Date(2026, 8, 24, 10, 33, 16, 0, time.UTC).Format(time.RFC3339Nano)
	later := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	// CreateRevision renames a colliding revision to the next free suffix and gives the new
	// one the key without a suffix: oldest first, that's .1, .2, then the one without
	versions := []*provider.FileVersion{
		{Key: "n.REV." + later},
		{Key: "n.REV." + ts},
		{Key: "n.REV." + ts + ".2"},
		{Key: "n.REV." + ts + ".1"},
	}
	slices.SortStableFunc(versions, compareContentTime)
	var got []string
	for _, v := range versions {
		got = append(got, v.Key)
	}
	want := []string{"n.REV." + ts + ".1", "n.REV." + ts + ".2", "n.REV." + ts, "n.REV." + later}
	if !slices.Equal(got, want) {
		t.Errorf("sorted by content time: %v, want %v", got, want)
	}
}
