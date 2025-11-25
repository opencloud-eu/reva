package trace

import (
	"os"
	"testing"
)

func TestResolveOptions_DefaultDisabled(t *testing.T) {
	res, err := resolveOptions(Options{})
	if err != nil {
		t.Fatalf("resolveOptions returned error: %v", err)
	}
	if res.exporter != exporterNone {
		t.Fatalf("expected exporter none, got %v", res.exporter)
	}
	if len(res.warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", res.warnings)
	}
}

func TestResolveOptions_LegacyEnabledDefaultsToOTLP(t *testing.T) {
	res, err := resolveOptions(Options{Enabled: true})
	if err != nil {
		t.Fatalf("resolveOptions returned error: %v", err)
	}
	if res.exporter != exporterOTLP {
		t.Fatalf("expected exporter otlp, got %v", res.exporter)
	}
	if len(res.warnings) == 0 {
		t.Fatalf("expected legacy warning when only enabled is set")
	}
}

func TestResolveOptions_HTTPCollector(t *testing.T) {
	res, err := resolveOptions(Options{Exporter: string(exporterOTLP), Collector: "https://tempo.example/v1/traces"})
	if err != nil {
		t.Fatalf("resolveOptions returned error: %v", err)
	}
	if res.exporter != exporterOTLP {
		t.Fatalf("expected exporter otlp, got %v", res.exporter)
	}
	if res.legacyEnv["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://tempo.example/v1/traces" {
		t.Fatalf("expected collector legacy endpoint to be propagated, got %q", res.legacyEnv["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/protobuf" {
		t.Fatalf("expected http/protobuf protocol, got %q", res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"])
	}
}

func TestResolveOptions_OtlpGrpcDeprecated(t *testing.T) {
	res, err := resolveOptions(Options{Exporter: "otlp_grpc"})
	if err != nil {
		t.Fatalf("resolveOptions returned error: %v", err)
	}
	if res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"] != "grpc" {
		t.Fatalf("expected grpc protocol override, got %q", res.legacyEnv["OTEL_EXPORTER_OTLP_PROTOCOL"])
	}
	if len(res.warnings) == 0 {
		t.Fatalf("expected warning for deprecated otlp_grpc exporter")
	}
}

func TestResolveOptions_UnknownExporter(t *testing.T) {
	if _, err := resolveOptions(Options{Exporter: "bogus"}); err == nil {
		t.Fatalf("expected error for unknown exporter")
	}
}

func TestApplyLegacyEnvRespectsExisting(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "existing")
	applyLegacyEnv(map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "new",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
	})

	if got := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); got != "existing" {
		t.Fatalf("expected existing value to be preserved, got %q", got)
	}
	if got := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); got != "http/protobuf" {
		t.Fatalf("expected protocol to be set, got %q", got)
	}
}
