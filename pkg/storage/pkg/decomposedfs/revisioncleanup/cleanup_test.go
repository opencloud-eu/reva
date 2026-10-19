// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"testing"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

func TestRevisionTime(t *testing.T) {
	ts := time.Date(2026, 8, 24, 10, 33, 16, 535011272, time.UTC)
	mtime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		key  string
		want time.Time
	}{
		{"plain", "a3995a3f.REV." + ts.Format(time.RFC3339Nano), ts},
		{"collision suffix", "a3995a3f.REV." + ts.Format(time.RFC3339Nano) + ".2", ts},
		{"unparsable falls back to mtime", "a3995a3f.REV.garbage", mtime},
		{"no delimiter falls back to mtime", "a3995a3f", mtime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := revisionTime(&provider.FileVersion{Key: tc.key, Mtime: uint64(mtime.Unix())})
			if !got.Equal(tc.want) {
				t.Errorf("revisionTime(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
