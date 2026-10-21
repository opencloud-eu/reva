// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// The metrics are global to the process, so the tests compare against the values from
// before the test instead of absolute values.

// metric names a metric, and for QueueFull the value of its outcome label.
type metric struct {
	name    string
	outcome string
}

var (
	mWorkers  = metric{name: "reva_revision_cleanup_workers"}
	mCapacity = metric{name: "reva_revision_cleanup_queue_capacity"}
	mLength   = metric{name: "reva_revision_cleanup_queue_length"}
	mRunning  = metric{name: "reva_revision_cleanup_running"}
	mEnabled  = metric{name: "reva_revision_cleanup_enabled"}
	mDropped  = metric{name: "reva_revision_cleanup_queue_full_total", outcome: queueFullDropped}
	mBlocked  = metric{name: "reva_revision_cleanup_queue_full_total", outcome: queueFullBlocked}
)

// metricValue reads a metric through the default gatherer, like a scrape would, which
// avoids a dependency on the prometheus testutil package. Metrics that were never set
// read as 0.
func metricValue(t *testing.T, m metric) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != m.name {
			continue
		}
		for _, sample := range family.GetMetric() {
			if m.outcome != "" {
				matches := false
				for _, label := range sample.GetLabel() {
					matches = matches || (label.GetName() == "outcome" && label.GetValue() == m.outcome)
				}
				if !matches {
					continue
				}
			}
			if sample.GetGauge() != nil {
				return sample.GetGauge().GetValue()
			}
			return sample.GetCounter().GetValue()
		}
	}
	return 0
}

// requireDelta waits until the metric differs by delta from before.
func requireDelta(t *testing.T, m metric, before, delta float64) {
	t.Helper()
	require.Eventually(t, func() bool { return metricValue(t, m) == before+delta }, 5*time.Second, 10*time.Millisecond,
		"%s: expected %v, got %v", m.name, before+delta, metricValue(t, m))
}

func TestMetricsQueueAndWorkers(t *testing.T) {
	workers, capacity, length, running := metricValue(t, mWorkers), metricValue(t, mCapacity), metricValue(t, mLength), metricValue(t, mRunning)

	ts := newTestService(t, 2, 5, QueueFullBlock)
	requireDelta(t, mWorkers, workers, 2)
	requireDelta(t, mCapacity, capacity, 5)

	ctx := context.Background()
	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.HandleRevisionCreated(ctx, testNode("b"))
	ts.waitStartedAll("a", "b")
	requireDelta(t, mRunning, running, 2)
	requireDelta(t, mLength, length, 0)

	ts.HandleRevisionCreated(ctx, testNode("c"))
	ts.HandleRevisionCreated(ctx, testNode("d"))
	ts.HandleRevisionCreated(ctx, testNode("d")) // merged, still one waiting node
	requireDelta(t, mLength, length, 2)

	ts.releaseOne()
	ts.releaseOne()
	ts.waitStartedAll("c", "d")
	requireDelta(t, mLength, length, 0)
	ts.releaseOne()
	ts.releaseOne()
	requireDelta(t, mRunning, running, 0)

	require.NoError(t, ts.Shutdown(ctx))
	requireDelta(t, mWorkers, workers, 0)
	requireDelta(t, mCapacity, capacity, 0)
}

func TestMetricsQueueFull(t *testing.T) {
	dropped, blocked := metricValue(t, mDropped), metricValue(t, mBlocked)
	length := metricValue(t, mLength)
	ctx := context.Background()

	drop := newTestService(t, 1, 1, QueueFullDrop)
	drop.HandleRevisionCreated(ctx, testNode("a"))
	drop.waitStarted("a")
	drop.HandleRevisionCreated(ctx, testNode("b")) // fills the queue
	drop.HandleRevisionCreated(ctx, testNode("c")) // dropped
	requireDelta(t, mDropped, dropped, 1)
	requireDelta(t, mLength, length, 1) // only b is waiting, c was dropped
	drop.releaseOne()
	drop.waitStarted("b")
	drop.releaseOne()
	requireDelta(t, mLength, length, 0)

	block := newTestService(t, 1, 1, QueueFullBlock)
	block.HandleRevisionCreated(ctx, testNode("a"))
	block.waitStarted("a")
	block.HandleRevisionCreated(ctx, testNode("b")) // fills the queue
	go block.HandleRevisionCreated(ctx, testNode("c"))
	requireDelta(t, mBlocked, blocked, 1)
	requireDelta(t, mLength, length, 2) // b is queued, c's caller is waiting for room
	block.releaseOne()
	block.waitStarted("b")
	block.releaseOne()
	block.waitStarted("c")
	block.releaseOne()
	requireDelta(t, mLength, length, 0)

	// a caller that waits in vain until the timeout counts as blocked, then as dropped
	timeout := newTestService(t, 1, 1, QueueFullBlock)
	timeout.queueFullTimeout = 50 * time.Millisecond
	timeout.HandleRevisionCreated(ctx, testNode("a"))
	timeout.waitStarted("a")
	timeout.HandleRevisionCreated(ctx, testNode("b")) // fills the queue
	timeout.HandleRevisionCreated(ctx, testNode("c")) // gives up after the timeout
	requireDelta(t, mBlocked, blocked, 2)
	requireDelta(t, mDropped, dropped, 2)
	requireDelta(t, mLength, length, 1) // only b is waiting, c was dropped
	timeout.releaseOne()
	timeout.waitStarted("b")
	timeout.releaseOne()
	requireDelta(t, mLength, length, 0)
}

func TestMetricsShutdownAbandons(t *testing.T) {
	length, capacity := metricValue(t, mLength), metricValue(t, mCapacity)
	ctx := context.Background()

	ts := newTestService(t, 1, 3, QueueFullBlock)
	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("b"))
	ts.HandleRevisionCreated(ctx, testNode("c"))
	requireDelta(t, mLength, length, 2)

	stopped := make(chan error)
	go func() { stopped <- ts.Shutdown(ctx) }()
	// the abandoned requests are no longer counted as waiting, right away
	requireDelta(t, mLength, length, 0)
	requireDelta(t, mCapacity, capacity, 0)
	ts.releaseOne()
	require.NoError(t, <-stopped)
	require.Equal(t, []processed{{"a", nil}}, ts.processedNodes(), "b and c were abandoned")
}

// A node to clean up once more, as it got new revisions while it was being cleaned up, counts
// as waiting until a worker takes it, and is abandoned on shutdown like the queued ones.
func TestMetricsNodesToCleanUpOnceMore(t *testing.T) {
	length := metricValue(t, mLength)
	ctx := context.Background()

	ts := newTestService(t, 1, 3, QueueFullBlock)
	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.HandleRevisionCreated(ctx, testNode("a")) // merged
	requireDelta(t, mLength, length, 1)
	ts.releaseOne()
	ts.waitStarted("a") // queued once more and taken by the worker
	requireDelta(t, mLength, length, 0)

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.HandleRevisionCreated(ctx, testNode("b"))
	requireDelta(t, mLength, length, 2)
	stopped := make(chan error)
	go func() { stopped <- ts.Shutdown(ctx) }()
	requireDelta(t, mLength, length, 0)
	ts.releaseOne()
	require.NoError(t, <-stopped)
	require.Len(t, ts.processedNodes(), 2, "a once more and b were abandoned")
}

func TestMetricsEnabled(t *testing.T) {
	cleaner, err := New("auto")
	require.NoError(t, err)
	s, err := NewService(cleaner, nil, nil, AsyncOptions{Enabled: false}, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, float64(1), metricValue(t, mEnabled))
}

// A disabled Service leaves all the metrics alone, even with background workers configured:
// it starts no workers, and the cleanup counts as disabled.
func TestMetricsOfADisabledService(t *testing.T) {
	enabled := metricValue(t, mEnabled)
	Enabled.Set(0)
	defer Enabled.Set(enabled)
	workers, capacity := metricValue(t, mWorkers), metricValue(t, mCapacity)

	cleaner, err := New("disabled")
	require.NoError(t, err)
	s, err := NewService(cleaner, nil, nil, AsyncOptions{Enabled: true, Workers: 2, QueueSize: 3}, nil)
	require.NoError(t, err)
	defer func() { _ = s.Shutdown(context.Background()) }()

	require.Equal(t, float64(0), metricValue(t, mEnabled))
	requireDelta(t, mWorkers, workers, 0)
	requireDelta(t, mCapacity, capacity, 0)
}

// The Service of an on-demand Run leaves the metrics of the storage drivers' Services alone.
func TestMetricsOnDemandRun(t *testing.T) {
	enabled := metricValue(t, mEnabled)
	Enabled.Set(0)
	defer Enabled.Set(enabled)
	workers, capacity := metricValue(t, mWorkers), metricValue(t, mCapacity)

	cleaner, err := New("auto")
	require.NoError(t, err)
	s, err := newService(cleaner, nil, nil, AsyncOptions{Enabled: true, Workers: 2, QueueSize: 3}, nil, true, false, nil)
	require.NoError(t, err)

	require.Equal(t, float64(0), metricValue(t, mEnabled))
	require.Equal(t, workers, metricValue(t, mWorkers))
	require.Equal(t, capacity, metricValue(t, mCapacity))
	m := s.m()
	require.False(t, m.queueLength == QueueLength || m.running == Running || m.queueFull == QueueFull,
		"the queue metrics are the exported ones")

	require.NoError(t, s.Shutdown(context.Background()))
	require.Equal(t, workers, metricValue(t, mWorkers))
	require.Equal(t, capacity, metricValue(t, mCapacity))

	// Run uses such a Service
	require.NoError(t, Run(context.Background(), nil, nil, nil, cleaner, false, AsyncOptions{Enabled: true}, nil, nil))
	require.Equal(t, float64(0), metricValue(t, mEnabled))
}
