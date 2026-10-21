// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
)

var tracer = otel.Tracer("github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup")

// QueueFullPolicy defines what happens when a cleanup is scheduled while the queue is full.
type QueueFullPolicy string

const (
	// QueueFullBlock makes the caller wait until there is room in the queue, so that no
	// cleanup is ever skipped, at the cost of slowing down writes under heavy load.
	QueueFullBlock QueueFullPolicy = "block"
	// QueueFullDrop skips the cleanup and logs it, so that writes never wait for the
	// cleanup. The node is cleaned up again when it gets its next revision.
	QueueFullDrop QueueFullPolicy = "drop"
)

// queueSizePerWorker is the number of queue slots per worker when the queue size is
// determined automatically.
const queueSizePerWorker = 64

// AsyncOptions configures how a Service runs the cleanup. They are passed as
// parameters rather than read from the driver options, so that other callers, like a
// command line tool, can use their own settings.
type AsyncOptions struct {
	// Enabled runs the cleanup in background workers. Otherwise HandleRevisionCreated runs it
	// right away, in the caller's goroutine.
	Enabled bool
	// Workers is the number of background workers, 0 determines it automatically:
	// half the available CPUs, but at least 2.
	Workers int
	// QueueSize is the number of nodes that can wait for a cleanup, 0 determines it
	// automatically from the number of workers.
	QueueSize int
	// QueueFull is the policy when the queue is full, "" means QueueFullDrop.
	QueueFull QueueFullPolicy
	// QueueFullTimeout bounds how long QueueFullBlock waits for room in a full queue: after
	// that, the cleanup of the node is skipped, as with QueueFullDrop. 0 waits as long as it
	// takes.
	QueueFullTimeout time.Duration
}

// resolve validates the options and determines the automatic values.
func (o AsyncOptions) resolve() (workers int, queueSize int, policy QueueFullPolicy, err error) {
	switch {
	case o.Workers < 0:
		return 0, 0, "", fmt.Errorf("invalid number of revision cleanup workers %d", o.Workers)
	case o.Workers == 0:
		// GOMAXPROCS rather than NumCPU, as it respects container CPU limits (cgroups)
		workers = max(2, runtime.GOMAXPROCS(0)/2)
	default:
		workers = o.Workers
	}

	switch {
	case o.QueueSize < 0:
		return 0, 0, "", fmt.Errorf("invalid revision cleanup queue size %d", o.QueueSize)
	case o.QueueSize == 0:
		queueSize = workers * queueSizePerWorker
	default:
		queueSize = o.QueueSize
	}

	if o.QueueFullTimeout < 0 {
		return 0, 0, "", fmt.Errorf("invalid revision cleanup queue full timeout %s", o.QueueFullTimeout)
	}

	switch o.QueueFull {
	case "":
		policy = QueueFullDrop
	case QueueFullBlock, QueueFullDrop:
		policy = o.QueueFull
	default:
		return 0, 0, "", fmt.Errorf("invalid revision cleanup queue full policy %q, must be %q or %q", o.QueueFull, QueueFullBlock, QueueFullDrop)
	}
	return workers, queueSize, policy, nil
}

// Service is the revision cleanup of a storage driver. Callers report that a new revision
// was created (HandleRevisionCreated), and the Service decides whether, when and how the
// old revisions are cleaned up; callers don't need to know whether that happens right away
// or in background workers (see AsyncOptions). It binds a Cleaner to the driver's lookup
// and tree, so that callers only need to name the node.
// A disabled Service, created without a strategy, never cleans up anything; see Enabled.
// A nil *Service behaves the same, as a safeguard, but NewService never returns one.
//
// In asynchronous mode, the cleanup runs in background workers fed by a bounded queue, so
// that it doesn't slow down the upload, restore or change on disk that created the new
// revision. Queued requests only carry the space and node id, and the worker reads the
// node again: a *node.Node must not be shared between goroutines, as its lock-held flag is
// per goroutine and its cached attributes go stale.
//
// Requests are de-duplicated per node: a node that is already waiting in the queue is not
// queued again (the revisions to keep are merged into the waiting request). A node whose
// cleanup is already running is not queued either, as another worker would only wait for
// the node lock: when that cleanup is done, the node is queued once more, at the end of the
// queue, as new revisions may have been created in the meantime, with the revisions to keep
// of all the requests that came in until then. A node therefore never occupies more than
// one worker, and a single busy file can't keep the workers from the others.
type Service struct {
	cleaner *Cleaner
	lu      node.PathLookup
	rs      RevisionStore
	log     *zerolog.Logger

	async   bool
	policy  QueueFullPolicy
	process func(ctx context.Context, spaceID, nodeID string, keep []string)
	queue   chan *request
	done    chan struct{}
	wg      sync.WaitGroup

	// how long a caller waits for room in a full queue with QueueFullBlock, 0 for as long as
	// it takes
	queueFullTimeout time.Duration

	// the numbers this Service added to the Workers and QueueCapacity gauges
	workers   int
	queueSize int

	mu     sync.Mutex
	queued map[string]*request // the requests waiting in the queue, by node; QueueLength counts them
	// the nodes being cleaned up by a worker, with the request to queue once more when it
	// is done, nil if there is none; QueueLength counts those requests as well
	running map[string]*request
	closed  bool
}

type request struct {
	ctx context.Context // see detach
	// link points to the span of the request that triggered the cleanup, see detach
	link    trace.Link
	spaceID string
	nodeID  string
	keep    []string
}

func (r *request) key() string {
	return r.spaceID + "/" + r.nodeID
}

// merge adds the revisions to keep of other to r, s.mu must be held.
func (r *request) merge(other *request) {
	for _, k := range other.keep {
		if !slices.Contains(r.keep, k) {
			r.keep = append(r.keep, k)
		}
	}
}

// NewServiceFromOptions creates the Service of a storage driver from its options: the cleanup
// strategy (RevisionsCleanupStrategy) and the RevisionsCleanupAsync* settings.
// It returns a disabled Service when no strategy is configured, and an error for invalid
// settings.
func NewServiceFromOptions(o *options.Options, lu node.PathLookup, rs RevisionStore, log *zerolog.Logger) (*Service, error) {
	cleaner, err := New(o.RevisionsCleanupStrategy)
	if err != nil {
		return nil, err
	}
	return NewService(cleaner, lu, rs, AsyncOptions{
		Enabled:          o.RevisionsCleanupAsync,
		Workers:          o.RevisionsCleanupAsyncWorkers,
		QueueSize:        o.RevisionsCleanupAsyncQueueSize,
		QueueFull:        QueueFullPolicy(o.RevisionsCleanupAsyncQueueFull),
		QueueFullTimeout: o.RevisionsCleanupAsyncQueueFullTimeout,
	}, log)
}

// NewService creates a Service that cleans up with the given Cleaner, and starts its
// workers if opts.Enabled. With a disabled (or nil) Cleaner, it returns a disabled Service,
// which starts no workers and never cleans up anything.
// The options are validated in any case, so that invalid settings are reported even while
// the cleanup is disabled.
func NewService(cleaner *Cleaner, lu node.PathLookup, rs RevisionStore, opts AsyncOptions, log *zerolog.Logger) (*Service, error) {
	workers, queueSize, policy, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = &zerolog.Logger{}
	}
	if !cleaner.Enabled() {
		return &Service{cleaner: cleaner, log: log}, nil
	}
	s := &Service{cleaner: cleaner, lu: lu, rs: rs, log: log}
	Enabled.Set(1)
	if opts.Enabled {
		s.queueFullTimeout = opts.QueueFullTimeout
		s.startWorkers(workers, queueSize, policy, s.cleanupNode)
		log.Info().Str("config", cleaner.config).Int("workers", workers).Int("queuesize", queueSize).
			Str("queuefull", string(policy)).Dur("queuefulltimeout", opts.QueueFullTimeout).
			Msg("revision cleanup: running in the background")
	}
	return s, nil
}

// Enabled tells whether s cleans up anything, i.e. whether its Cleaner has a strategy.
// It is false for a disabled Service, and for a nil one.
func (s *Service) Enabled() bool {
	return s != nil && s.cleaner.Enabled()
}

// startWorkers switches the Service to asynchronous mode. process does the work for one
// node; it is a parameter so that tests can observe the queueing without a storage.
func (s *Service) startWorkers(workers, queueSize int, policy QueueFullPolicy, process func(ctx context.Context, spaceID, nodeID string, keep []string)) {
	s.async = true
	s.policy = policy
	s.process = process
	s.queue = make(chan *request, queueSize)
	s.done = make(chan struct{})
	s.queued = make(map[string]*request)
	s.running = make(map[string]*request)
	s.workers = workers
	s.queueSize = queueSize
	Workers.Add(float64(workers))
	QueueCapacity.Add(float64(queueSize))
	for range workers {
		s.wg.Add(1)
		go s.work()
	}
}

// HandleRevisionCreated handles the event that a new revision of n was created and has
// settled: it cleans up the old revisions of n according to the strategy, never deleting
// the revisions with a key in keep. In asynchronous mode it only queues n and returns,
// see Service and AsyncOptions.
// Errors are logged, never returned: a failed cleanup must not fail the operation that
// triggered it.
//
// It checks no permissions, see Cleaner.Cleanup.
func (s *Service) HandleRevisionCreated(ctx context.Context, n *node.Node, keep ...string) {
	if !s.Enabled() || n == nil {
		return
	}
	s.handle(ctx, n.SpaceID, n.ID, n, keep)
}

// HandleRevisionCreatedByID is HandleRevisionCreated for a node given by its ids, for callers
// whose node is still in use elsewhere, as a node must not be shared between goroutines. It
// reads a node of its own only to clean it up: not at all if the cleanup is disabled, and in
// asynchronous mode only in the worker, which reads the node anyway.
//
// It checks no permissions, see Cleaner.Cleanup.
func (s *Service) HandleRevisionCreatedByID(ctx context.Context, spaceID, nodeID string, keep ...string) {
	if !s.Enabled() {
		return
	}
	s.handle(ctx, spaceID, nodeID, nil, keep)
}

// handle cleans up the node right away in synchronous mode, n if given, otherwise read by its
// ids, and queues it in asynchronous mode.
func (s *Service) handle(ctx context.Context, spaceID, nodeID string, n *node.Node, keep []string) {
	if !s.async {
		if n == nil {
			s.cleanupNode(ctx, spaceID, nodeID, keep)
			return
		}
		s.cleaner.Cleanup(ctx, s.lu, s.rs, n, keep...)
		return
	}
	// the cleanup outlives the request that triggered it, see detach
	detached, link := detach(ctx)
	s.enqueue(&request{ctx: detached, link: link, spaceID: spaceID, nodeID: nodeID, keep: slices.Clone(keep)})
}

// detach returns the context for a cleanup in the background, which outlives the request that
// triggered it, and a link to the request's span.
//
// The context is never cancelled, and it carries none of the values of the request's context,
// which may only be valid during the request, and which a request waiting in the queue would
// otherwise keep alive, except for the logger, so that the log lines of the cleanup keep the
// fields of the request, e.g. its id. The cleanup gets a trace of its own (see startSpan) with
// the link, rather than a span of the request's trace, which would usually have ended long
// before.
func detach(ctx context.Context) (context.Context, trace.Link) {
	detached := appctx.WithLogger(context.Background(), appctx.GetLogger(ctx))
	return detached, trace.LinkFromContext(ctx)
}

// startSpan starts the span of the cleanup of r in a worker, linked to the span of the request
// that triggered it, if there was one, see detach.
func startSpan(r *request) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithAttributes(
		attribute.String("spaceid", r.spaceID),
		attribute.String("nodeid", r.nodeID),
	)}
	if r.link.SpanContext.IsValid() {
		opts = append(opts, trace.WithLinks(r.link))
	}
	return tracer.Start(r.ctx, "revision cleanup", opts...)
}

func (s *Service) enqueue(r *request) {
	log := appctx.GetLogger(r.ctx).With().Str("spaceid", r.spaceID).Str("nodeid", r.nodeID).Logger()
	key := r.key()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		log.Debug().Msg("revision cleanup: shutting down, not scheduling cleanup")
		return
	}
	if waiting, ok := s.queued[key]; ok {
		// already waiting in the queue, it will see the new revision as well
		waiting.merge(r)
		s.mu.Unlock()
		return
	}
	if again, ok := s.running[key]; ok {
		// being cleaned up right now: it is queued once more when that is done
		if again == nil {
			s.running[key] = r
			QueueLength.Inc()
		} else {
			again.merge(r)
		}
		s.mu.Unlock()
		return
	}
	s.queued[key] = r
	QueueLength.Inc()
	s.mu.Unlock()
	s.send(r)
}

// send puts r, which waits in s.queued, into the queue, applying the queue full policy.
// s.mu is not held while sending, so that a blocked sender does not block the workers.
func (s *Service) send(r *request) {
	log := appctx.GetLogger(r.ctx).With().Str("spaceid", r.spaceID).Str("nodeid", r.nodeID).Logger()
	select {
	case s.queue <- r:
		return
	default:
	}
	if s.policy == QueueFullDrop {
		QueueFull.WithLabelValues(queueFullDropped).Inc()
		s.forget(r)
		log.Warn().Str("path", s.internalPath(r.spaceID, r.nodeID)).
			Msg("revision cleanup: queue is full, skipping the cleanup of this node until its next revision")
		return
	}
	QueueFull.WithLabelValues(queueFullBlocked).Inc()
	log.Debug().Msg("revision cleanup: queue is full, waiting for room")
	// with a timeout, a caller doesn't wait for longer than that: under sustained load, it
	// would otherwise slow down every write, e.g. the consumer of the postprocessing events
	var timeout <-chan time.Time
	if s.queueFullTimeout > 0 {
		timer := time.NewTimer(s.queueFullTimeout)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case s.queue <- r:
	case <-s.done:
		s.forget(r)
	case <-timeout:
		QueueFull.WithLabelValues(queueFullDropped).Inc()
		s.forget(r)
		log.Warn().Str("path", s.internalPath(r.spaceID, r.nodeID)).Dur("timeout", s.queueFullTimeout).
			Msg("revision cleanup: queue is still full after the timeout, skipping the cleanup of this node until its next revision")
	}
}

// internalPath returns the path of a node on disk, e.g. for logging what is skipped, so that
// admins can find it. It is empty for a Service without a lookup.
func (s *Service) internalPath(spaceID, nodeID string) string {
	if s.lu == nil {
		return ""
	}
	return s.lu.InternalPath(spaceID, nodeID)
}

// forget removes a request that did not make it into the queue.
func (s *Service) forget(r *request) {
	s.mu.Lock()
	if s.queued[r.key()] == r {
		delete(s.queued, r.key())
		QueueLength.Dec()
	}
	s.mu.Unlock()
}

func (s *Service) work() {
	defer s.wg.Done()
	for {
		// a select with several ready cases picks one at random, so check for the
		// shutdown first, otherwise a worker could keep taking requests from a
		// non-empty queue after Shutdown
		select {
		case <-s.done:
			return
		default:
		}
		select {
		case <-s.done:
			return
		case r := <-s.queue:
			select {
			case <-s.done:
				// shut down while waiting for this request: abandon it, Shutdown
				// counts it as abandoned
				return
			default:
			}
			// from here on, new requests for the node are collected to queue it once more
			// when this cleanup is done, instead of being merged into this one
			s.mu.Lock()
			if s.closed {
				// shut down in the meantime: Shutdown already counted this request as abandoned
				s.mu.Unlock()
				return
			}
			if s.queued[r.key()] == r {
				delete(s.queued, r.key())
				QueueLength.Dec()
			}
			s.running[r.key()] = nil
			keep := slices.Clone(r.keep)
			s.mu.Unlock()
			Running.Inc()
			ctx, span := startSpan(r)
			s.process(ctx, r.spaceID, r.nodeID, keep)
			span.End()
			Running.Dec()
			s.mu.Lock()
			again := s.running[r.key()]
			delete(s.running, r.key())
			if again == nil || s.closed {
				// done, or shut down, which abandoned a request to queue again
				s.mu.Unlock()
				continue
			}
			// queue the node once more, at the end of the queue, so that the other nodes get
			// their turn; sending in a goroutine of its own, as a worker must never wait for
			// room in the queue it empties. QueueLength already counts the request.
			s.queued[again.key()] = again
			s.mu.Unlock()
			go s.send(again)
		}
	}
}

// cleanupNode reads the node again and cleans it up, it runs in a worker.
func (s *Service) cleanupNode(ctx context.Context, spaceID, nodeID string, keep []string) {
	// read also in a disabled space, to tell it apart from an error
	n, err := node.ReadNode(ctx, s.lu, spaceID, nodeID, "", true, nil, false)
	if err != nil {
		appctx.GetLogger(ctx).Error().Err(err).Str("spaceid", spaceID).Str("nodeid", nodeID).Msg("revision cleanup: could not read node")
		return
	}
	if !n.Exists {
		// deleted in the meantime, its revisions went with it (or into the trash)
		return
	}
	if n.SpaceRoot != nil && n.SpaceRoot.IsDisabled(ctx) {
		// disabled in the meantime: the space is in the trash, leave its files alone
		appctx.GetLogger(ctx).Debug().Str("spaceid", spaceID).Str("nodeid", nodeID).Msg("revision cleanup: the space is disabled, skipping")
		return
	}
	s.cleaner.Cleanup(ctx, s.lu, s.rs, n, keep...)
}

// Shutdown stops the background workers. A cleanup that is running is finished, the ones
// still waiting in the queue are abandoned (and logged): they are picked up again when
// their nodes get their next revision. Blocked callers are released. It returns when the
// workers have stopped, or with ctx's error if ctx ends first.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil || !s.async {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	// abandon the requests still waiting, in the queue or to be queued once more: they won't
	// be processed anymore, and their nodes are cleaned up again when they get their next
	// revision
	abandoned := len(s.queued)
	clear(s.queued)
	for key, r := range s.running {
		if r != nil {
			abandoned++
			s.running[key] = nil
		}
	}
	QueueLength.Sub(float64(abandoned))
	Workers.Sub(float64(s.workers))
	QueueCapacity.Sub(float64(s.queueSize))
	s.mu.Unlock()

	stopped := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		return ctx.Err()
	}

	if abandoned > 0 {
		s.log.Info().Int("abandoned", abandoned).Msg("revision cleanup: stopped, abandoned the cleanups still waiting in the queue")
	}
	return nil
}
