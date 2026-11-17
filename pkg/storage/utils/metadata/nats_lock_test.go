// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func newTestJetStream(t *testing.T) jetstream.JetStream {
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
	return js
}

// TestDiskUploadWithLock_NatsLockerAcrossReplicas runs two Disk storages on
// the same data dir, each with its own NatsLocker on a shared bucket, like two
// replicas. The disk storage relies on its lock, so lost updates would show.
func TestDiskUploadWithLock_NatsLockerAcrossReplicas(t *testing.T) {
	js := newTestJetStream(t)
	dir := t.TempDir()
	var replicas []*Disk
	for range 2 {
		l, err := NewNatsLockerFromJetStream(js, "test-locks", 0)
		if err != nil {
			t.Fatalf("NewNatsLockerFromJetStream: %v", err)
		}
		d, err := NewDiskStorage(dir, WithLocker(l))
		if err != nil {
			t.Fatalf("NewDiskStorage: %v", err)
		}
		replicas = append(replicas, d)
	}

	const n = 40
	var wg sync.WaitGroup
	for i := range n {
		d := replicas[i%len(replicas)]
		wg.Go(func() {
			if _, err := d.UploadWithLock(context.Background(), UploadRequest{Path: "/counter"}, increment); err != nil {
				t.Errorf("UploadWithLock: %v", err)
			}
		})
	}
	wg.Wait()

	got, err := replicas[0].SimpleDownload(context.Background(), "/counter")
	if err != nil {
		t.Fatalf("SimpleDownload: %v", err)
	}
	if string(got) != strconv.Itoa(n) {
		t.Errorf("expected counter %d, got %s (lost updates)", n, got)
	}
}

// TestLockConfig_Squash checks that drivers embedding LockConfig with
// `mapstructure:",squash"` read the lock settings from their top level.
func TestLockConfig_Squash(t *testing.T) {
	var c struct {
		DataDir    string `mapstructure:"data_dir"`
		LockConfig `mapstructure:",squash"`
	}
	err := mapstructure.Decode(map[string]any{
		"data_dir":     "/data",
		"lock_backend": "nats",
		"lock_nats": map[string]any{
			"nodes":  []string{"127.0.0.1:9233"},
			"bucket": "sharing-locks",
			"ttl":    10,
		},
	}, &c)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.LockBackend != "nats" || c.LockNats.Bucket != "sharing-locks" || c.LockNats.TTL != 10 || len(c.LockNats.Nodes) != 1 || c.DataDir != "/data" {
		t.Errorf("unexpected config: %+v", c)
	}
}
