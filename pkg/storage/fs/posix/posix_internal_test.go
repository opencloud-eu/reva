// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package posix

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"
)

type fakeOnDemand struct {
	req revisioncleanup.Request
}

func (f *fakeOnDemand) CleanupRevisionsUnchecked(ctx context.Context, req revisioncleanup.Request, report func(revisioncleanup.Result)) error {
	f.req = req
	report(revisioncleanup.Result{NodeID: "reported"})
	return nil
}

// posixFS wraps the Decomposedfs in a middleware that doesn't pass optional interfaces
// through, so it forwards the on-demand revision cleanup itself.
func TestPosixFSForwardsCleanupRevisions(t *testing.T) {
	inner := &fakeOnDemand{}
	fs := &posixFS{revisions: inner}
	req := revisioncleanup.Request{SpaceIDs: []string{"space"}, Strategy: "0", DryRun: true}
	var reported []string

	err := fs.CleanupRevisionsUnchecked(context.Background(), req, func(r revisioncleanup.Result) { reported = append(reported, r.NodeID) })

	require.NoError(t, err)
	require.Equal(t, req, inner.req)
	require.Equal(t, []string{"reported"}, reported)

	err = (&posixFS{}).CleanupRevisionsUnchecked(context.Background(), req, nil)
	require.ErrorAs(t, err, new(errtypes.NotSupported))
}
