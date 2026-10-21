// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics of the revision cleanup, e.g. for Grafana dashboards.
//
// They are registered once per process, at package level: a process can create several
// storage drivers, and with them several Services (e.g. the storageprovider and the
// dataprovider each create their own driver), and registering a metric twice panics.
// The gauges describing the queue and the workers are therefore the sum over all Services of
// the process: each Service adds its share when it starts and removes it when it shuts down.
// As a sum can hide one queue being full while another one is empty, QueueFull counts every
// request that found its queue full, which is the most direct sign that the number of workers
// or the queue size is too small.
var (
	// RevisionsDeleted counts the revisions deleted by the cleanup.
	RevisionsDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "reva_revision_cleanup_revisions_deleted_total",
		Help: "Number of old file revisions deleted by the revision cleanup",
	})

	// BytesDeleted counts the size of the revisions deleted by the cleanup.
	BytesDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "reva_revision_cleanup_bytes_deleted_total",
		Help: "Total size in bytes of the old file revisions deleted by the revision cleanup",
	})

	// PurgeFailures counts the revisions that could not be deleted. They are retried by the
	// next cleanup of their node, i.e. when the node gets its next revision.
	PurgeFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "reva_revision_cleanup_purge_failures_total",
		Help: "Number of old file revisions the revision cleanup failed to delete, completely or only their blob",
	})

	// QueueFull counts the cleanup requests that found the queue full, by outcome: "blocked"
	// (the caller waited for room, queue full policy "block") or "dropped" (the cleanup was
	// skipped until the node's next revision, queue full policy "drop"). A caller that waited
	// in vain until the timeout of "block" counts as both: blocked when it starts to wait, and
	// dropped when it gives up.
	QueueFull = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "reva_revision_cleanup_queue_full_total",
		Help: "Number of revision cleanup requests that found the queue full, by outcome (blocked or dropped)",
	}, []string{"outcome"})

	// QueueLength is the number of nodes waiting for a background cleanup, including the
	// ones whose callers wait for room in a full queue. A node waits at most once (requests
	// are de-duplicated per node), how many of its revisions get deleted is only known once a
	// worker processes it.
	QueueLength = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "reva_revision_cleanup_queue_length",
		Help: "Number of files waiting for a background revision cleanup, summed over all queues of the process",
	})

	// QueueCapacity is the size of the background cleanup queues.
	QueueCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "reva_revision_cleanup_queue_capacity",
		Help: "Number of files that can wait for a background revision cleanup, summed over all queues of the process",
	})

	// Workers is the number of background cleanup workers, 0 when the cleanup runs synchronously.
	Workers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "reva_revision_cleanup_workers",
		Help: "Number of background revision cleanup workers, summed over all queues of the process",
	})

	// Running is the number of nodes being cleaned up by background workers right now.
	Running = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "reva_revision_cleanup_running",
		Help: "Number of files whose revisions are being cleaned up by background workers right now",
	})

	// Enabled is 1 if a revision cleanup strategy is configured, 0 otherwise.
	Enabled = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "reva_revision_cleanup_enabled",
		Help: "Whether the revision cleanup is enabled (1) or not (0)",
	})
)

// labels of QueueFull
const (
	queueFullBlocked = "blocked"
	queueFullDropped = "dropped"
)
