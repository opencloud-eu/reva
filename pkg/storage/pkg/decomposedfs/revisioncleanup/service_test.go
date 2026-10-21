// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/opencloud-eu/reva/v2/pkg/appctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

func TestAsyncOptionsResolve(t *testing.T) {
	autoWorkers := max(2, runtime.GOMAXPROCS(0)/2)

	for _, tc := range []struct {
		name      string
		opts      AsyncOptions
		workers   int
		queueSize int
		policy    QueueFullPolicy
	}{
		{"automatic", AsyncOptions{}, autoWorkers, autoWorkers * queueSizePerWorker, QueueFullDrop},
		{"explicit", AsyncOptions{Workers: 3, QueueSize: 10, QueueFull: QueueFullDrop}, 3, 10, QueueFullDrop},
		{"automatic queue size", AsyncOptions{Workers: 3}, 3, 3 * queueSizePerWorker, QueueFullDrop},
		{"explicit block", AsyncOptions{QueueFull: QueueFullBlock}, autoWorkers, autoWorkers * queueSizePerWorker, QueueFullBlock},
		{"block with a timeout", AsyncOptions{QueueFull: QueueFullBlock, QueueFullTimeout: time.Second}, autoWorkers, autoWorkers * queueSizePerWorker, QueueFullBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workers, queueSize, policy, err := tc.opts.resolve()
			require.NoError(t, err)
			require.Equal(t, tc.workers, workers)
			require.Equal(t, tc.queueSize, queueSize)
			require.Equal(t, tc.policy, policy)
		})
	}

	for _, opts := range []AsyncOptions{
		{Workers: -1},
		{QueueSize: -1},
		{QueueFull: "wait"},
		{QueueFull: QueueFullBlock, QueueFullTimeout: -time.Second},
	} {
		_, _, _, err := opts.resolve()
		require.Error(t, err, "%+v", opts)
	}
}

func TestNewServiceWithoutStrategy(t *testing.T) {
	ctx := context.Background()
	disabled, err := New("disabled")
	require.NoError(t, err)
	for _, cleaner := range []*Cleaner{disabled, nil} {
		s, err := NewService(cleaner, nil, nil, AsyncOptions{Enabled: true}, nil)
		require.NoError(t, err)
		require.NotNil(t, s)
		require.False(t, s.Enabled())
		require.False(t, s.async, "a disabled Service starts no workers")
		// a disabled Service does nothing: it has no lookup it could read a node with
		s.HandleRevisionCreated(ctx, testNode("a"))
		s.HandleRevisionCreatedByID(ctx, "space", "a")
		require.NoError(t, s.Shutdown(ctx))
	}

	// a nil Service does nothing either
	var none *Service
	require.False(t, none.Enabled())
	none.HandleRevisionCreated(ctx, testNode("a"))
	none.HandleRevisionCreatedByID(ctx, "space", "a")
	require.NoError(t, none.Shutdown(ctx))

	// invalid settings are reported even though the cleanup is disabled
	_, err = NewService(nil, nil, nil, AsyncOptions{Enabled: true, QueueFull: "wait"}, nil)
	require.Error(t, err)
}

func testNode(id string) *node.Node {
	return &node.Node{BaseNode: node.BaseNode{SpaceID: "space", ID: id}}
}

type processed struct {
	nodeID string
	keep   []string
}

// testService is a Service whose workers record the processed nodes. Each node
// blocks its worker until it is released, so that tests can control what is running
// and what is waiting in the queue.
type testService struct {
	*Service
	t         *testing.T
	started   chan string
	release   chan struct{}
	mu        sync.Mutex
	processed []processed
}

func newTestService(t *testing.T, workers, queueSize int, policy QueueFullPolicy) *testService {
	// an enabled Cleaner, the workers below never run it
	cleaner, err := New("1")
	require.NoError(t, err)
	ts := &testService{
		Service: &Service{cleaner: cleaner, log: &zerolog.Logger{}},
		t:       t,
		started: make(chan string, 100),
		release: make(chan struct{}),
	}
	ts.startWorkers(workers, queueSize, policy, func(ctx context.Context, spaceID, nodeID string, keep []string) {
		ts.started <- nodeID
		<-ts.release
		ts.mu.Lock()
		ts.processed = append(ts.processed, processed{nodeID, keep})
		ts.mu.Unlock()
	})
	t.Cleanup(func() {
		close(ts.release)
		_ = ts.Shutdown(context.Background())
	})
	return ts
}

// waitStarted waits until a worker started processing the given node.
func (ts *testService) waitStarted(nodeID string) {
	ts.t.Helper()
	select {
	case got := <-ts.started:
		require.Equal(ts.t, nodeID, got)
	case <-time.After(5 * time.Second):
		ts.t.Fatalf("node %s was not processed", nodeID)
	}
}

// waitWaiting waits until the given node is waiting in the queue, i.e. its caller has
// registered the request and is at most about to block on a full queue.
func (ts *testService) waitWaiting(nodeID string) {
	ts.t.Helper()
	require.Eventually(ts.t, func() bool {
		// the Service's mutex, not testService.mu, which only guards the processed nodes
		ts.Service.mu.Lock()
		defer ts.Service.mu.Unlock()
		_, ok := ts.queued[testNode(nodeID).SpaceID+"/"+nodeID]
		return ok
	}, 5*time.Second, time.Millisecond, "node %s is not waiting", nodeID)
}

// waitStartedAll waits until workers started processing all the given nodes, in any
// order, as with several workers the order is not deterministic.
func (ts *testService) waitStartedAll(nodeIDs ...string) {
	ts.t.Helper()
	got := make([]string, 0, len(nodeIDs))
	for range nodeIDs {
		select {
		case id := <-ts.started:
			got = append(got, id)
		case <-time.After(5 * time.Second):
			ts.t.Fatalf("only %v of %v were processed", got, nodeIDs)
		}
	}
	require.ElementsMatch(ts.t, nodeIDs, got)
}

// releaseOne lets one blocked worker finish its node.
func (ts *testService) releaseOne() {
	ts.release <- struct{}{}
}

func (ts *testService) processedNodes() []processed {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]processed{}, ts.processed...)
}

func TestServiceDeduplicatesWaitingNodes(t *testing.T) {
	ts := newTestService(t, 1, 10, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a") // a is running, the worker is busy

	ts.HandleRevisionCreated(ctx, testNode("b"), "k1")
	ts.HandleRevisionCreated(ctx, testNode("b"), "k2") // merged into the waiting request
	ts.HandleRevisionCreated(ctx, testNode("b"), "k1") // no duplicate keep key
	ts.HandleRevisionCreated(ctx, testNode("a"))       // a is running, not waiting: queued again

	ts.releaseOne()
	ts.waitStarted("b")
	ts.releaseOne()
	ts.waitStarted("a")
	ts.releaseOne()

	require.Eventually(t, func() bool { return len(ts.processedNodes()) == 3 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []processed{
		{"a", nil},
		{"b", []string{"k1", "k2"}},
		{"a", nil},
	}, ts.processedNodes())
}

// A node that gets new revisions while it is being cleaned up occupies no other worker: it is
// cleaned up once more afterwards, with the revisions to keep of all those requests, and the
// other nodes get the other workers.
func TestServiceRunsANodeOnOneWorkerAtATime(t *testing.T) {
	ts := newTestService(t, 3, 10, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("busy"))
	ts.waitStarted("busy")
	for _, k := range []string{"k1", "k2", "k1"} {
		ts.HandleRevisionCreated(ctx, testNode("busy"), k)
	}
	ts.HandleRevisionCreated(ctx, testNode("other"))
	ts.waitStarted("other") // a free worker takes the other node
	select {
	case id := <-ts.started:
		t.Fatalf("%s started while busy was still being cleaned up", id)
	case <-time.After(200 * time.Millisecond):
	}

	ts.releaseOne()
	ts.releaseOne()
	ts.waitStarted("busy") // once more, for the requests that came in meanwhile
	ts.releaseOne()

	require.Eventually(t, func() bool { return len(ts.processedNodes()) == 3 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(ts.processedNodes()) > 3 }, 200*time.Millisecond, 10*time.Millisecond)
	require.ElementsMatch(t, []processed{
		{"busy", nil},
		{"other", nil},
		{"busy", []string{"k1", "k2"}},
	}, ts.processedNodes())
}

// A node given by its ids is queued like one given as a node, and only the worker reads it.
func TestServiceQueuesANodeGivenByItsIDs(t *testing.T) {
	ts := newTestService(t, 1, 10, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreatedByID(ctx, "space", "a", "k1")
	ts.waitStarted("a")
	ts.HandleRevisionCreatedByID(ctx, "space", "b")
	ts.HandleRevisionCreated(ctx, testNode("b"), "k2") // the same node: merged into the waiting request

	ts.releaseOne()
	ts.waitStarted("b")
	ts.releaseOne()

	require.Eventually(t, func() bool { return len(ts.processedNodes()) == 2 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []processed{
		{"a", []string{"k1"}},
		{"b", []string{"k2"}},
	}, ts.processedNodes())
}

// A node collected to be cleaned up once more goes to the end of the queue, so that a node
// that always gets new revisions doesn't keep a worker from the other nodes.
func TestServiceDetachesTheCleanupFromTheRequest(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	cleaner, err := New("1")
	require.NoError(t, err)
	s := &Service{cleaner: cleaner, log: &zerolog.Logger{}}
	workerCtx := make(chan context.Context, 1)
	s.startWorkers(1, 1, QueueFullBlock, func(ctx context.Context, spaceID, nodeID string, keep []string) {
		workerCtx <- ctx
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	type requestValue struct{}
	var logs bytes.Buffer
	logger := zerolog.New(&logs).With().Str("service", "storage-users").Logger()
	ctx := context.WithValue(appctx.WithLogger(context.Background(), &logger), requestValue{}, "only valid during the request")
	ctx = context.WithValue(ctx, middleware.RequestIDKey, "request-1")
	ctx, requestSpan := provider.Tracer("test").Start(ctx, "upload")
	s.HandleRevisionCreatedByID(ctx, "space", "node")
	requestSpan.End()
	got := <-workerCtx

	assert.Nil(t, got.Value(requestValue{}), "the values of the request are left behind")
	// except for the logger, with the fields of the request, including its id
	appctx.GetLogger(got).Info().Msg("cleaning up")
	assert.Contains(t, logs.String(), `"service":"storage-users"`)
	assert.Contains(t, logs.String(), `"request-id":"request-1"`)
	span := trace.SpanContextFromContext(got)
	require.True(t, span.IsValid())
	assert.NotEqual(t, requestSpan.SpanContext().TraceID(), span.TraceID(), "the cleanup has a trace of its own")
	// linked to the request's span, once the worker ended its span
	require.Eventually(t, func() bool {
		for _, ended := range recorder.Ended() {
			if ended.SpanContext().SpanID() == span.SpanID() {
				links := ended.Links()
				return len(links) == 1 && links[0].SpanContext.SpanID() == requestSpan.SpanContext().SpanID()
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
}

func TestServiceQueuesANodeOnceMoreAtTheEnd(t *testing.T) {
	ts := newTestService(t, 1, 10, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("busy"))
	ts.waitStarted("busy")
	ts.HandleRevisionCreated(ctx, testNode("other"))
	ts.HandleRevisionCreated(ctx, testNode("busy"))

	ts.releaseOne()
	ts.waitStarted("other")
	ts.releaseOne()
	ts.waitStarted("busy")
	ts.releaseOne()
}

func TestServiceDropsWhenQueueIsFull(t *testing.T) {
	ts := newTestService(t, 1, 1, QueueFullDrop)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("b")) // fills the queue
	ts.HandleRevisionCreated(ctx, testNode("c")) // dropped, returns right away

	ts.releaseOne()
	ts.waitStarted("b")
	ts.releaseOne()

	require.Eventually(t, func() bool { return len(ts.processedNodes()) == 2 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(ts.processedNodes()) > 2 }, 200*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, "a", ts.processedNodes()[0].nodeID)
	require.Equal(t, "b", ts.processedNodes()[1].nodeID)

	// c was forgotten, so it can be scheduled again
	ts.HandleRevisionCreated(ctx, testNode("c"))
	ts.waitStarted("c")
	ts.releaseOne()
}

func TestServiceBlocksUpToTheTimeoutWhenQueueIsFull(t *testing.T) {
	ts := newTestService(t, 1, 1, QueueFullBlock)
	ts.queueFullTimeout = 200 * time.Millisecond
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("b")) // fills the queue

	start := time.Now()
	ts.HandleRevisionCreated(ctx, testNode("c")) // waits for the timeout, then drops c
	require.GreaterOrEqual(t, time.Since(start), ts.queueFullTimeout, "waited for room")

	ts.releaseOne()
	ts.waitStarted("b")
	ts.releaseOne()
	require.Eventually(t, func() bool { return len(ts.processedNodes()) == 2 }, 5*time.Second, 10*time.Millisecond)
	select {
	case node := <-ts.started:
		t.Fatalf("%s was cleaned up although it was dropped after the timeout", node)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestServiceBlocksWhenQueueIsFull(t *testing.T) {
	ts := newTestService(t, 1, 1, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("b")) // fills the queue

	returned := make(chan struct{})
	go func() {
		ts.HandleRevisionCreated(ctx, testNode("c")) // blocks until there is room
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("HandleRevisionCreated returned although the queue is full")
	case <-time.After(200 * time.Millisecond):
	}

	ts.releaseOne() // a is done, b moves on to the worker, c gets into the queue
	ts.waitStarted("b")
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleRevisionCreated did not return after the queue had room again")
	}
	ts.releaseOne()
	ts.waitStarted("c") // nothing was dropped
	ts.releaseOne()
}

func TestServiceShutdown(t *testing.T) {
	ts := newTestService(t, 1, 1, QueueFullBlock)
	ctx := context.Background()

	ts.HandleRevisionCreated(ctx, testNode("a"))
	ts.waitStarted("a")
	ts.HandleRevisionCreated(ctx, testNode("b")) // fills the queue

	blocked := make(chan struct{})
	go func() {
		ts.HandleRevisionCreated(ctx, testNode("c")) // blocks, released by Shutdown
		close(blocked)
	}()
	// only shut down once c's caller is about to block on the full queue, otherwise it
	// could see the shutdown before blocking and the release would not be tested
	ts.waitWaiting("c")

	stopped := make(chan error)
	go func() { stopped <- ts.Shutdown(ctx) }()

	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not release the blocked HandleRevisionCreated call")
	}

	// the running cleanup is finished before Shutdown returns
	select {
	case <-stopped:
		t.Fatal("Shutdown returned while a cleanup was still running")
	case <-time.After(200 * time.Millisecond):
	}
	ts.releaseOne()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return")
	}

	// b was abandoned, and nothing is scheduled anymore after the shutdown
	ts.HandleRevisionCreated(ctx, testNode("d"))
	require.Equal(t, []processed{{"a", nil}}, ts.processedNodes())
	require.NoError(t, ts.Shutdown(ctx), "a second Shutdown is a no-op")
}

func TestServiceShutdownTimeout(t *testing.T) {
	ts := newTestService(t, 1, 1, QueueFullBlock)
	ts.HandleRevisionCreated(context.Background(), testNode("a"))
	ts.waitStarted("a")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, ts.Shutdown(ctx), context.DeadlineExceeded)
	ts.releaseOne()
}
