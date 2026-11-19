// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/options"
)

func asyncOptions(root, strategy string) *options.Options {
	return &options.Options{
		Root:                         root,
		RevisionsCleanupStrategy:     strategy,
		RevisionsCleanupAsync:        true,
		RevisionsCleanupAsyncWorkers: 2,
	}
}

// The instances of a driver for the same storage, e.g. the one of the storage provider and the
// one of the data provider, share one Service, which runs until the last of them shuts it down.
func TestNewServiceFromOptionsSharesTheServiceOfAStorage(t *testing.T) {
	root := t.TempDir()
	workers := metricValue(t, mWorkers)

	first, err := NewServiceFromOptions(asyncOptions(root, "1"), nil, nil, nil)
	require.NoError(t, err)
	// the same root, spelled differently
	second, err := NewServiceFromOptions(asyncOptions(root+"/", "1"), nil, nil, nil)
	require.NoError(t, err)
	require.Same(t, first, second)
	require.Equal(t, workers+2, metricValue(t, mWorkers), "the shared Service starts its workers once")

	require.NoError(t, first.Shutdown(context.Background()))
	require.Equal(t, workers+2, metricValue(t, mWorkers), "still in use by the second instance")
	require.False(t, second.closed)

	require.NoError(t, second.Shutdown(context.Background()))
	require.Equal(t, workers, metricValue(t, mWorkers))
	require.True(t, second.closed)

	// once all instances shut it down, the next one gets a new Service
	third, err := NewServiceFromOptions(asyncOptions(root, "1"), nil, nil, nil)
	require.NoError(t, err)
	require.NotSame(t, first, third)
	require.NoError(t, third.Shutdown(context.Background()))
}

func TestNewServiceFromOptionsSharesNothingBetweenStorages(t *testing.T) {
	first, err := NewServiceFromOptions(asyncOptions(t.TempDir(), "1"), nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })
	second, err := NewServiceFromOptions(asyncOptions(t.TempDir(), "1"), nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })

	require.NotSame(t, first, second)
}

// A storage set up twice with different settings would be cleaned up with the settings of
// whichever instance came first: it is rejected instead.
func TestNewServiceFromOptionsRejectsDifferentSettingsForTheSameStorage(t *testing.T) {
	root := t.TempDir()
	first, err := NewServiceFromOptions(asyncOptions(root, "1"), nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })

	_, err = NewServiceFromOptions(asyncOptions(root, "2"), nil, nil, nil)
	require.ErrorContains(t, err, "different revision cleanup settings")

	other := asyncOptions(root, "1")
	other.RevisionsCleanupAsyncWorkers = 3
	_, err = NewServiceFromOptions(other, nil, nil, nil)
	require.ErrorContains(t, err, "different revision cleanup settings")
}

// A disabled Service is shared as well, so that an instance with a strategy can't silently
// differ from one without.
func TestNewServiceFromOptionsSharesADisabledService(t *testing.T) {
	root := t.TempDir()
	first, err := NewServiceFromOptions(asyncOptions(root, ""), nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Shutdown(context.Background()) })
	require.False(t, first.Enabled())

	_, err = NewServiceFromOptions(asyncOptions(root, "1"), nil, nil, nil)
	require.ErrorContains(t, err, "different revision cleanup settings")
}
