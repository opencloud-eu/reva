// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request Request
		wantErr string
	}{
		{name: "a space", request: Request{SpaceIDs: []string{"space"}}},
		{name: "all spaces", request: Request{All: true}},
		{name: "a path within a space", request: Request{SpaceIDs: []string{"space"}, Path: "/dir"}},
		{name: "a path on disk", request: Request{FSPath: "/var/lib/opencloud/dir"}},
		{name: "a forced strategy", request: Request{All: true, Strategy: "0"}},
		{name: "strategies per space type", request: Request{All: true, StrategyPersonal: "1", StrategyProject: "90d"}},
		{name: "no space", request: Request{}, wantErr: "no space to clean up given"},
		{name: "all and some spaces", request: Request{All: true, SpaceIDs: []string{"space"}}, wantErr: "either all spaces or specific spaces"},
		{name: "a path in more than one space", request: Request{SpaceIDs: []string{"a", "b"}, Path: "/dir"}, wantErr: "a path requires exactly one space"},
		{name: "a path on disk and a space", request: Request{FSPath: "/dir", SpaceIDs: []string{"space"}}, wantErr: "a path on disk can't be combined"},
		{name: "a forced strategy and one for personal spaces", request: Request{All: true, Strategy: "0", StrategyPersonal: "1"}, wantErr: "can't be combined with strategies per space type"},
		{name: "a forced strategy and one for project spaces", request: Request{All: true, Strategy: "0", StrategyProject: "1"}, wantErr: "can't be combined with strategies per space type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.request.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
