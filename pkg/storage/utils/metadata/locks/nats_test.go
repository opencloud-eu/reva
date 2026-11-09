// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// newTestNatsKV starts an in-process NATS server with JetStream and returns a
// lock bucket created with NatsLockBucketConfig and the client connection.
func newTestNatsKV(t *testing.T, ttl time.Duration) (jetstream.KeyValue, *nats.Conn) {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true,
		StoreDir:  t.TempDir(),
		Port:      -1,
		NoSigs:    true,
		NoLog:     true,
	})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect("", nats.InProcessServer(srv))
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	kv, err := js.CreateOrUpdateKeyValue(context.Background(), NatsLockBucketConfig("test-locks", ttl))
	if err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return kv, nc
}

func TestNatsLocker_SerializesSameKey(t *testing.T) {
	kv, _ := newTestNatsKV(t, DefaultNatsLockTTL)
	runConcurrent(t, NewNatsLocker(kv), context.Background(), "users/u1/created.json", 20)
}

// TestNatsLocker_SerializesAcrossLockers uses one locker per goroutine, like
// separate replicas sharing the bucket.
func TestNatsLocker_SerializesAcrossLockers(t *testing.T) {
	kv, _ := newTestNatsKV(t, DefaultNatsLockTTL)
	a, b := NewNatsLocker(kv), NewNatsLocker(kv)
	ctx := context.Background()

	unlockA, err := a.Lock(ctx, "storages/s1/sp1$sp1.json")
	if err != nil {
		t.Fatalf("lock a: %v", err)
	}
	acquired := make(chan struct{})
	go func() {
		unlockB, err := b.Lock(ctx, "storages/s1/sp1$sp1.json")
		if err != nil {
			t.Errorf("lock b: %v", err)
			return
		}
		close(acquired)
		unlockB()
	}()

	select {
	case <-acquired:
		t.Fatal("b acquired the lock while a held it")
	case <-time.After(200 * time.Millisecond):
	}
	unlockA()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("b did not acquire the lock after a released it")
	}
}

func TestNatsLocker_DifferentKeysDoNotBlock(t *testing.T) {
	kv, _ := newTestNatsKV(t, DefaultNatsLockTTL)
	l := NewNatsLocker(kv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	unlockA, err := l.Lock(ctx, "a.json")
	if err != nil {
		t.Fatalf("lock a: %v", err)
	}
	defer unlockA()
	unlockB, err := l.Lock(ctx, "b.json")
	if err != nil {
		t.Fatalf("lock b: %v", err)
	}
	unlockB()
}

// TestNatsLocker_ExpiresAndStaleUnlockKeepsNewHolder covers a holder that
// stalls past the TTL: the lock expires, the next writer takes it, and the
// late unlock of the first holder must not release the new holder's lock.
func TestNatsLocker_ExpiresAndStaleUnlockKeepsNewHolder(t *testing.T) {
	kv, _ := newTestNatsKV(t, time.Second)
	a, b := NewNatsLocker(kv), NewNatsLocker(kv)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	unlockA, err := a.Lock(ctx, "f.json")
	if err != nil {
		t.Fatalf("lock a: %v", err)
	}
	start := time.Now()
	unlockB, err := b.Lock(ctx, "f.json")
	if err != nil {
		t.Fatalf("lock b after expiry: %v", err)
	}
	if waited := time.Since(start); waited < 500*time.Millisecond {
		t.Errorf("b got the lock after %s, before a's lock expired", waited)
	}

	unlockA() // stale
	if _, err := kv.Get(ctx, natsKey("f.json")); err != nil {
		t.Errorf("stale unlock released the new holder's lock: %v", err)
	}
	unlockB()
	if _, err := kv.Get(ctx, natsKey("f.json")); !errors.Is(err, jetstream.ErrKeyNotFound) {
		t.Errorf("expected the lock to be released, got %v", err)
	}
}

func TestNatsLocker_CancelWhileWaiting(t *testing.T) {
	kv, _ := newTestNatsKV(t, DefaultNatsLockTTL)
	l := NewNatsLocker(kv)
	unlock, err := l.Lock(context.Background(), "f.json")
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := l.Lock(ctx, "f.json"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

// TestNatsLocker_ProceedsWithoutNats checks that an unavailable NATS does not
// block writes: the lock is only an optimisation.
func TestNatsLocker_ProceedsWithoutNats(t *testing.T) {
	kv, nc := newTestNatsKV(t, DefaultNatsLockTTL)
	l := NewNatsLocker(kv)
	nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := l.Lock(ctx, "f.json")
	if err != nil {
		t.Fatalf("expected to proceed without a lock, got %v", err)
	}
	unlock()
}

// validNatsKey is the key format accepted by JetStream key-value buckets.
var validNatsKey = regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)

func TestNatsKey_IsValid(t *testing.T) {
	for _, key := range []string{"users/u1/created.json", "storages/a$b/c!d.json", "/publicshares.json", "ü ber.json"} {
		if !validNatsKey.MatchString(natsKey(key)) {
			t.Errorf("invalid key for %q: %q", key, natsKey(key))
		}
	}
}
