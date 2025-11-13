package tree

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"
)

// Config is the configuration needed for a NATS event stream.
type Config struct {
	Endpoint             string        `mapstructure:"address"`
	Cluster              string        `mapstructure:"clusterID"`
	TLSInsecure          bool          `mapstructure:"tls-insecure"`
	TLSRootCACertificate string        `mapstructure:"tls-root-ca-cert"`
	EnableTLS            bool          `mapstructure:"enable-tls"`
	AuthUsername         string        `mapstructure:"username"`
	AuthPassword         string        `mapstructure:"password"`
	MaxAckPending        int           `mapstructure:"max-ack-pending"`
	AckWait              time.Duration `mapstructure:"ack-wait"`
}

// natsEvent represents the event encoded in MessagePack.
// we abbreviate the the properties to save some space
type natsEvent struct {
	Event           string `msgpack:"e"`
	Path            string `msgpack:"p,omitempty"`
	DestinationPath string `msgpack:"d,omitempty"`
	BytesWritten    int64  `msgpack:"b,omitempty"`
	Type            string `msgpack:"t,omitempty"`
}

// NatsWatcher consumes filesystem-style events from NATS JetStream.
type NatsWatcher struct {
	tree      *Tree
	log       *zerolog.Logger
	watchRoot string
	config    Config
	group     string
}

// NewNatsWatcher creates a new NATS watcher.
func NewNatsWatcher(tree *Tree, cfg Config, group string, log *zerolog.Logger) (*NatsWatcher, error) {
	return &NatsWatcher{
		tree:      tree,
		log:       log,
		watchRoot: tree.options.WatchRoot,
		config:    cfg,
		group:     group,
	}, nil
}

// Watch starts consuming events from a NATS JetStream subject
func (w *NatsWatcher) Watch(ctx context.Context, streamName, subject string) error {
	w.log.Info().Str("stream", streamName).Str("subject", subject).Msg("starting NATS watcher with auto-reconnect")

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("context cancelled, stopping NATS watcher")
			return ctx.Err()
		default:
		}

		// Try to connect with exponential backoff
		nc, js, err := w.connectWithBackoff(ctx)
		if err != nil {
			w.log.Error().Err(err).Msg("failed to establish NATS connection after retries")
			time.Sleep(5 * time.Second)
			continue
		}

		if err := w.consume(ctx, js, streamName, subject); err != nil {
			w.log.Error().Err(err).Msg("NATS consumer exited with error, reconnecting")
		}

		_ = nc.Drain()
		nc.Close()
		time.Sleep(2 * time.Second)
	}
}

// connectWithBackoff repeatedly attempts to connect to NATS JetStream with exponential backoff.
func (w *NatsWatcher) connectWithBackoff(ctx context.Context) (*nats.Conn, jetstream.JetStream, error) {
	var nc *nats.Conn
	var js jetstream.JetStream

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 1 * time.Second
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = 0 // never stop

	connect := func() error {
		select {
		case <-ctx.Done():
			return backoff.Permanent(ctx.Err())
		default:
		}

		var err error
		nc, err = w.connect()
		if err != nil {
			w.log.Warn().Err(err).Msg("failed to connect to NATS, retrying")
			return err
		}

		js, err = jetstream.New(nc)
		if err != nil {
			nc.Close()
			w.log.Warn().Err(err).Msg("failed to create jetstream context, retrying")
			return err
		}

		w.log.Info().Str("endpoint", w.config.Endpoint).Msg("connected to NATS JetStream")
		return nil
	}

	if err := backoff.Retry(connect, backoff.WithContext(b, ctx)); err != nil {
		return nil, nil, err
	}
	return nc, js, nil
}

// consume subscribes to JetStream and handles messages.
func (w *NatsWatcher) consume(ctx context.Context, js jetstream.JetStream, streamName, subject string) error {
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return fmt.Errorf("failed to get stream: %w", err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       w.group,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: w.config.MaxAckPending,
		AckWait:       w.config.AckWait,
		FilterSubject: subject,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	w.log.Info().
		Str("stream", streamName).
		Str("subject", subject).
		Msg("started consuming from JetStream")

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		defer func() {
			if ackErr := msg.Ack(); ackErr != nil {
				w.log.Warn().Err(ackErr).Msg("failed to ack message")
			}
		}()

		var ev natsEvent
		if err := msgpack.Unmarshal(msg.Data(), &ev); err != nil {
			w.log.Error().Err(err).Msg("failed to decode MessagePack event")
			return
		}

		w.handleEvent(ev)
	})

	if err != nil {
		return fmt.Errorf("consumer error: %w", err)
	}

	<-ctx.Done()
	return ctx.Err()
}

// connect establishes a single NATS connection with optional TLS and auth.
func (w *NatsWatcher) connect() (*nats.Conn, error) {
	var tlsConf *tls.Config
	if w.config.EnableTLS {
		var rootCAPool *x509.CertPool
		if w.config.TLSRootCACertificate != "" {
			rootCrtFile, err := os.ReadFile(w.config.TLSRootCACertificate)
			if err != nil {
				return nil, fmt.Errorf("failed to read root CA: %w", err)
			}
			rootCAPool = x509.NewCertPool()
			rootCAPool.AppendCertsFromPEM(rootCrtFile)
			w.config.TLSInsecure = false
		}
		tlsConf = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: w.config.TLSInsecure,
			RootCAs:            rootCAPool,
		}
	}

	opts := []nats.Option{nats.Name("opencloud-posixfs-nats")}
	if tlsConf != nil {
		opts = append(opts, nats.Secure(tlsConf))
	}
	if w.config.AuthUsername != "" && w.config.AuthPassword != "" {
		opts = append(opts, nats.UserInfo(w.config.AuthUsername, w.config.AuthPassword))
	}
	return nats.Connect(w.config.Endpoint, opts...)
}

// handleEvent applies the event to the local tree.
func (w *NatsWatcher) handleEvent(ev natsEvent) {
	var err error

	// Determine the relevant path
	path := filepath.Join(w.watchRoot, ev.Path)

	isDir := ev.Type == "dir"

	switch ev.Event {
	case "CREATE":
		err = w.tree.Scan(path, ActionCreate, isDir)
	case "MOVED_TO":
		err = w.tree.Scan(path, ActionMove, isDir)
	case "MOVE_FROM":
		err = w.tree.Scan(path, ActionMoveFrom, isDir)
	case "MOVE": // support event with source and target path
		dst := filepath.Join(w.watchRoot, ev.DestinationPath)
		if dst == "" {
			w.log.Warn().Interface("event", ev).Msg("MOVE event missing destination path")
			return
		}
		err = w.tree.Scan(path, ActionMoveFrom, isDir)
		if err == nil {
			err = w.tree.Scan(dst, ActionMove, isDir)
		}
	case "CLOSE_WRITE":
		err = w.tree.Scan(path, ActionUpdate, isDir)
	case "DELETE":
		err = w.tree.Scan(path, ActionDelete, isDir)
	default:
		w.log.Warn().Str("event", ev.Event).Msg("unhandled event type")
	}

	if err != nil {
		w.log.Error().Err(err).Interface("event", ev).Msg("error processing event")
	}
}
