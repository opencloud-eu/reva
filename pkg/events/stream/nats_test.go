// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"context"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensureMainQueueMaxAge", func() {
	var (
		ctx context.Context
		js  jetstream.JetStream
	)

	BeforeEach(func() {
		ctx = context.Background()

		server, err := natsserver.NewServer(&natsserver.Options{
			DontListen: true,
			JetStream:  true,
			StoreDir:   GinkgoT().TempDir(),
		})
		Expect(err).ToNot(HaveOccurred())
		server.Start()
		Expect(server.ReadyForConnections(5 * time.Second)).To(BeTrue())
		DeferCleanup(server.Shutdown)

		nc, err := nats.Connect("", nats.InProcessServer(server))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(nc.Close)

		js, err = jetstream.New(nc)
		Expect(err).ToNot(HaveOccurred())
	})

	mainQueueConfig := func() jetstream.StreamConfig {
		stream, err := js.Stream(ctx, mainQueueName)
		Expect(err).ToNot(HaveOccurred())
		return stream.CachedInfo().Config
	}

	It("creates the main queue with a max age", func() {
		Expect(ensureMainQueueMaxAge(ctx, js)).To(Succeed())

		Expect(mainQueueConfig().MaxAge).To(Equal(mainQueueMaxAge))
	})

	It("sets the max age on an existing main queue without touching other settings", func() {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:        mainQueueName,
			Description: "set by an admin",
			MaxMsgs:     1000,
			Discard:     jetstream.DiscardNew,
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(ensureMainQueueMaxAge(ctx, js)).To(Succeed())

		cfg := mainQueueConfig()
		Expect(cfg.MaxAge).To(Equal(mainQueueMaxAge))
		Expect(cfg.Description).To(Equal("set by an admin"))
		Expect(cfg.MaxMsgs).To(Equal(int64(1000)))
		Expect(cfg.Discard).To(Equal(jetstream.DiscardNew))
	})

	It("keeps a max age that is already set", func() {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:   mainQueueName,
			MaxAge: time.Hour,
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(ensureMainQueueMaxAge(ctx, js)).To(Succeed())

		Expect(mainQueueConfig().MaxAge).To(Equal(time.Hour))
	})
})
