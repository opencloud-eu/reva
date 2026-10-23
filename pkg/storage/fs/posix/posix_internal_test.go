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

type fakeFSPaths map[string][2]string

func (f fakeFSPaths) SpacePathFromFSPath(ctx context.Context, p string) (string, string, error) {
	r, ok := f[p]
	if !ok {
		return "", "", errtypes.NotFound(p)
	}
	return r[0], r[1], nil
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

// posixFS resolves a path on disk to its space and its path within the space, which the
// Decomposedfs doesn't know about.
func TestPosixFSResolvesFSPaths(t *testing.T) {
	inner := &fakeOnDemand{}
	fs := &posixFS{revisions: inner, fsPaths: fakeFSPaths{
		"/data/users/einstein/dir1": {"space", "dir1"},
		"/data/users/einstein":      {"space", ""},
	}}
	report := func(revisioncleanup.Result) {}

	require.NoError(t, fs.CleanupRevisionsUnchecked(context.Background(), revisioncleanup.Request{FSPath: "/data/users/einstein/dir1", Strategy: "0", DryRun: true}, report))
	require.Equal(t, revisioncleanup.Request{SpaceIDs: []string{"space"}, Path: "dir1", Strategy: "0", DryRun: true}, inner.req)

	require.NoError(t, fs.CleanupRevisionsUnchecked(context.Background(), revisioncleanup.Request{FSPath: "/data/users/einstein"}, report))
	require.Equal(t, revisioncleanup.Request{SpaceIDs: []string{"space"}}, inner.req)

	inner.req = revisioncleanup.Request{}
	err := fs.CleanupRevisionsUnchecked(context.Background(), revisioncleanup.Request{FSPath: "/elsewhere"}, report)
	require.ErrorAs(t, err, new(errtypes.NotFound))
	err = fs.CleanupRevisionsUnchecked(context.Background(), revisioncleanup.Request{FSPath: "/data/users/einstein/dir1", SpaceIDs: []string{"space"}}, report)
	require.ErrorContains(t, err, "path on disk")
	require.Zero(t, inner.req, "nothing must be cleaned up")
}
