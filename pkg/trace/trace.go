// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package trace

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/credentials"
)

type exporterKind string

const (
	exporterNone    exporterKind = "none"
	exporterConsole exporterKind = "console"
	exporterOTLP    exporterKind = "otlp"
)

type resolvedOptions struct {
	exporter  exporterKind
	warnings  []string
	legacyEnv map[string]string
}

var (
	// Propagator is the default Reva propagator.
	Propagator      = propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	defaultProvider = revaDefaultTracerProvider{}
)

type revaDefaultTracerProvider struct {
	mutex       sync.RWMutex
	initialized bool
}

// NewTracerProvider returns a new TracerProvider, configure for the specified service
func NewTracerProvider(opts ...Option) trace.TracerProvider {
	options := Options{}
	for _, o := range opts {
		o(&options)
	}

	if options.TransportCredentials == nil {
		options.TransportCredentials = credentials.NewClientTLSFromCert(nil, "")
	}

	resolved, err := resolveOptions(options)
	if err != nil {
		panic(fmt.Errorf("invalid tracing configuration: %w", err))
	}

	serviceName := resolveServiceName(options.ServiceName)
	logger := log.Logger
	if serviceName != "" {
		logger = logger.With().Str("service", serviceName).Logger()
	}

	for _, warn := range resolved.warnings {
		logger.Warn().Msg(warn)
	}

	applyLegacyEnv(resolved.legacyEnv)

	switch resolved.exporter {
	case exporterNone:
		return noop.NewTracerProvider()
	case exporterConsole:
		tp, err := createConsoleProvider(serviceName)
		if err != nil {
			panic(err)
		}
		return tp
	case exporterOTLP:
		tp, err := createOTLPProvider(serviceName, options)
		if err != nil {
			panic(err)
		}
		return tp
	default:
		panic(fmt.Errorf("unsupported trace exporter %q", resolved.exporter))
	}
}

// SetDefaultTracerProvider sets the default trace provider
func SetDefaultTracerProvider(tp trace.TracerProvider) {
	otel.SetTracerProvider(tp)
	defaultProvider.mutex.Lock()
	defer defaultProvider.mutex.Unlock()
	defaultProvider.initialized = true
}

// InitDefaultTracerProvider initializes a global default jaeger TracerProvider at a package level.
//
// Deprecated: Use NewTracerProvider and SetDefaultTracerProvider to properly initialize a tracer provider with options
func InitDefaultTracerProvider(collector, endpoint string) {
	defaultProvider.mutex.Lock()
	defer defaultProvider.mutex.Unlock()
	if !defaultProvider.initialized {
		tp, err := createOTLPProvider(
			"reva default otlp provider",
			Options{
				Enabled:   true,
				Exporter:  string(exporterOTLP),
				Endpoint:  endpoint,
				Collector: collector,
				Insecure:  true,
			},
		)
		if err != nil {
			panic(err)
		}
		SetDefaultTracerProvider(tp)
	}
}

// DefaultProvider returns the "global" default TracerProvider
// Currently used by the pool to get the global tracer
func DefaultProvider() trace.TracerProvider {
	defaultProvider.mutex.RLock()
	defer defaultProvider.mutex.RUnlock()
	return otel.GetTracerProvider()
}

func createConsoleProvider(serviceName string) (trace.TracerProvider, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("failed to create console trace exporter: %w", err)
	}

	res, err := buildResource(context.Background(), serviceName)
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	), nil
}

func createOTLPProvider(serviceName string, options Options) (trace.TracerProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exporter, err := newOTLPExporter(ctx, options)
	if err != nil {
		return nil, err
	}

	res, err := buildResource(context.Background(), serviceName)
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	), nil
}

func buildResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build tracing resource: %w", err)
	}
	return res, nil
}

func newOTLPExporter(ctx context.Context, options Options) (sdktrace.SpanExporter, error) {
	protocol := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
		os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
	)))

	switch protocol {
	case "", "grpc":
		return newOTLPGRPCExporter(ctx, options)
	case "http", "http/protobuf":
		return newOTLPHTTPExporter(ctx, options)
	default:
		log.Warn().Str("protocol", protocol).Msg("unsupported OTLP protocol; defaulting to gRPC")
		return newOTLPGRPCExporter(ctx, options)
	}
}

func newOTLPGRPCExporter(ctx context.Context, options Options) (sdktrace.SpanExporter, error) {
	clientOpts := []otlptracegrpc.Option{}

	endpoint := firstNonEmpty(
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"),
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		options.Endpoint,
	)
	if endpoint != "" {
		if strings.Contains(endpoint, "://") {
			clientOpts = append(clientOpts, otlptracegrpc.WithEndpointURL(endpoint))
		} else {
			clientOpts = append(clientOpts, otlptracegrpc.WithEndpoint(endpoint))
		}
	}

	if useInsecure(options) {
		clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
	} else if options.TransportCredentials != nil {
		clientOpts = append(clientOpts, otlptracegrpc.WithTLSCredentials(options.TransportCredentials))
	}

	exporter, err := otlptracegrpc.New(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP gRPC exporter: %w", err)
	}
	return exporter, nil
}

func newOTLPHTTPExporter(ctx context.Context, options Options) (sdktrace.SpanExporter, error) {
	clientOpts := []otlptracehttp.Option{}

	endpoint := firstNonEmpty(
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"),
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		options.Collector,
	)
	if endpoint != "" {
		if strings.Contains(endpoint, "://") {
			clientOpts = append(clientOpts, otlptracehttp.WithEndpointURL(endpoint))
		} else {
			clientOpts = append(clientOpts, otlptracehttp.WithEndpoint(endpoint))
		}
	}

	if useInsecure(options) {
		clientOpts = append(clientOpts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP HTTP exporter: %w", err)
	}
	return exporter, nil
}

func resolveOptions(opts Options) (resolvedOptions, error) {
	res := resolvedOptions{
		exporter:  exporterNone,
		warnings:  make([]string, 0, 4),
		legacyEnv: map[string]string{},
	}

	normalized := strings.ToLower(strings.TrimSpace(opts.Exporter))

	switch normalized {
	case "":
		// handled after switch based on Enabled
	case string(exporterNone):
		res.exporter = exporterNone
	case string(exporterConsole):
		res.exporter = exporterConsole
	case string(exporterOTLP):
		res.exporter = exporterOTLP
	case "otlp_grpc":
		res.exporter = exporterOTLP
		res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"] = "grpc"
		res.warnings = append(res.warnings, "trace exporter 'otlp_grpc' is deprecated; use OTEL_EXPORTER_OTLP_PROTOCOL=grpc instead")
	case "otlp_http":
		res.exporter = exporterOTLP
		res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
		res.warnings = append(res.warnings, "trace exporter 'otlp_http' is deprecated; use OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf instead")
	case "jaeger":
		res.exporter = exporterOTLP
		res.warnings = append(res.warnings, "Jaeger exporter is no longer supported; falling back to OTLP")
	default:
		return resolvedOptions{}, fmt.Errorf("unknown trace exporter %q", opts.Exporter)
	}

	if normalized == "" {
		if opts.Enabled {
			res.exporter = exporterOTLP
			res.warnings = append(res.warnings, "tracing_enabled is deprecated; defaulting to exporter 'otlp'. Set tracing_exporter or OTEL_TRACES_EXPORTER explicitly.")
		} else {
			res.exporter = exporterNone
		}
	}

	if !opts.Enabled && res.exporter != exporterNone && normalized != "" {
		res.warnings = append(res.warnings, "tracing_enabled is deprecated and ignored when an exporter is set; use exporter 'none' to disable tracing")
	}

	if res.exporter == exporterOTLP {
		if endpoint := strings.TrimSpace(opts.Endpoint); endpoint != "" {
			res.legacyEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
			if _, exists := res.legacyEnv["OTEL_EXPORTER_OTLP_INSECURE"]; !exists {
				res.legacyEnv["OTEL_EXPORTER_OTLP_INSECURE"] = "true"
			}
		}
		if collector := strings.TrimSpace(opts.Collector); collector != "" {
			res.legacyEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] = collector
			res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
		}
		if opts.Insecure {
			res.legacyEnv["OTEL_EXPORTER_OTLP_INSECURE"] = "true"
		}
	}

	return res, nil
}

func applyLegacyEnv(values map[string]string) {
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if current, ok := os.LookupEnv(key); ok && strings.TrimSpace(current) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			log.Debug().Err(err).Str("env", key).Msg("failed to apply legacy tracing env override")
		}
	}
}

func resolveServiceName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "reva"
	}
	return name
}

func useInsecure(opts Options) bool {
	if v, ok := envBool("OTEL_EXPORTER_OTLP_TRACES_INSECURE"); ok {
		return v
	}
	if v, ok := envBool("OTEL_EXPORTER_OTLP_INSECURE"); ok {
		return v
	}
	return opts.Insecure
}

func envBool(key string) (bool, bool) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "t", "yes", "y":
		return true, true
	case "0", "false", "f", "no", "n":
		return false, true
	default:
		return false, true
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
