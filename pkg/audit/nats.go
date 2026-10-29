package audit

import (
	"context"
	"errors"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v2/pkg/events/raw"
)

// NewNatsEmitter connects to NATS and returns an emitter that publishes to
// the audit stream. It creates the stream with defaults when it is missing,
// so events published before the audit service started are kept. It never
// changes an existing stream, that is the job of the audit service.
func NewNatsEmitter(ctx context.Context, connName string, cfg raw.Config, opts Options) (*NatsEmitter, error) {
	js, err := raw.JetStream(ctx, connName, cfg)
	if err != nil {
		return nil, err
	}
	if err := createStreamIfMissing(ctx, js); err != nil {
		return nil, err
	}
	return NewEmitter(JetStreamPublisher{JS: js}, opts), nil
}

func createStreamIfMissing(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.Stream(ctx, StreamName); err == nil {
		return nil
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	// another service may create it at the same time, that is fine
	_, err := js.CreateStream(ctx, streamConfig(StreamConfig{}))
	if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		return nil
	}
	return err
}

// EmitterConfig is the audit part of a reva service config.
type EmitterConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Mode    string `mapstructure:"mode"`
	Service string `mapstructure:"service"`
	// NATS connection, same keys as the events config: address, clusterID, ...
	Nats raw.Config `mapstructure:",squash"`
}

// EmitterFromConfig returns an emitter for the config, one that drops
// everything when audit is off.
func EmitterFromConfig(ctx context.Context, connName string, cfg EmitterConfig, opts Options) (Emitter, error) {
	if !cfg.Enabled || cfg.Nats.Endpoint == "" {
		return Discard{}, nil
	}
	opts.Service.Name = cfg.Service
	opts.Mode = Mode(cfg.Mode)
	return NewNatsEmitter(ctx, connName, cfg.Nats, opts)
}
