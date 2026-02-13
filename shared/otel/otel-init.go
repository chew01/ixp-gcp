package otel

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var serviceName = os.Getenv("OTEL_SERVICE_NAME")

// Package-level OTel instruments for use across all files
var (
	Logger *slog.Logger
	Tracer trace.Tracer
	Meter  metric.Meter
)

// InitInstruments sets up the OTel-integrated logger, tracer, and meter.
// Must be called after setupOTelSDK.
func InitInstruments() {
	Logger = otelslog.NewLogger(serviceName)
	Tracer = otel.Tracer(serviceName)
	Meter = otel.Meter(serviceName)
	slog.SetDefault(Logger)
}
