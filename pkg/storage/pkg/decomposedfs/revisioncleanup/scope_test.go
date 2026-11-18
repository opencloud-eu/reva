// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingScoper is a SpaceScoper that counts how often it switched to the group of a space.
type countingScoper struct {
	scoped   []string
	unscoped int
}

func (c *countingScoper) ScopeToSpace(ctx context.Context, spaceID string) (context.Context, func() error, error) {
	c.scoped = append(c.scoped, spaceID)
	return ctx, func() error { c.unscoped++; return nil }, nil
}

func TestScopeToSpaceSwitchesOncePerSpace(t *testing.T) {
	scoper := &countingScoper{}

	ctx, unscope, err := scopeToSpace(context.Background(), scoper, "space-1")
	require.NoError(t, err)

	// e.g. the Cleaner, after the worker switched before reading the node
	_, again, err := scopeToSpace(ctx, scoper, "space-1")
	require.NoError(t, err)
	require.NoError(t, again())
	require.Equal(t, []string{"space-1"}, scoper.scoped)
	require.Zero(t, scoper.unscoped, "switching back from a switch that wasn't made")

	// another space needs its own group
	_, other, err := scopeToSpace(ctx, scoper, "space-2")
	require.NoError(t, err)
	require.NoError(t, other())
	require.Equal(t, []string{"space-1", "space-2"}, scoper.scoped)

	require.NoError(t, unscope())
	require.Equal(t, 2, scoper.unscoped)
}

func TestScopeToSpaceDoesNothingWithoutAScoper(t *testing.T) {
	ctx := context.Background()
	scoped, unscope, err := scopeToSpace(ctx, struct{}{}, "space-1")
	require.NoError(t, err)
	require.Equal(t, ctx, scoped)
	require.NoError(t, unscope())
}
