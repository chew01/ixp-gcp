package otel

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Helper to inject context into a map-like carrier
type StringMapCarrier map[string]string

func (c StringMapCarrier) Set(key, value string) { c[key] = value }
func (c StringMapCarrier) Get(key string) string { return c[key] }
func (c StringMapCarrier) Keys() []string        { /* not needed for inject */ return nil }

// Package-level OTel instruments for use across all files
var (
	Logger *slog.Logger = slog.Default()
	Tracer trace.Tracer = otel.Tracer("ixp-default")
	Meter  metric.Meter = otel.Meter("ixp-default")
)

// InitInstruments sets up the OTel-integrated logger, tracer, and meter.
// Must be called after SetupOTelSDK.
func InitInstruments() {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "ixp-service"
	}
	Logger = otelslog.NewLogger(serviceName)
	Tracer = otel.Tracer(serviceName)
	Meter = otel.Meter(serviceName)
	slog.SetDefault(Logger)
}
