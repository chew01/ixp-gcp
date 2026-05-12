package otel

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
// Detects TELEMETRY_MODE env var to choose between OTLP collector or direct exporters.
func SetupOTelSDK(ctx context.Context) (func(context.Context) error, error) {
	// Check telemetry mode
	telemetryMode := os.Getenv("TELEMETRY_MODE")
	if telemetryMode == "" {
		telemetryMode = "collector" // default
	}

	log.Printf("Telemetry mode: %s", telemetryMode)

	// Use direct exporters if configured
	if telemetryMode == "direct" {
		return SetupDirectExporters(ctx)
	}

	// Otherwise, use OTLP exporters to collector
	// Get endpoint with validation and default
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
		log.Printf("OTEL_EXPORTER_OTLP_ENDPOINT not set, using default: %s", endpoint)
	} else {
		log.Printf("OTEL_EXPORTER_OTLP_ENDPOINT set to: %s", endpoint)
	}

	var shutdownFuncs []func(context.Context) error
	var err error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	tracerProvider, err := newTracerProvider(endpoint)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up meter provider.
	meterProvider, err := newMeterProvider(endpoint)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}

	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(endpoint)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return shutdown, err
}

// Setup propagator
func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// Setup Tracer Provider
func newTracerProvider(endpoint string) (*trace.TracerProvider, error) {
	log.Printf("Creating tracer provider with endpoint: %s", endpoint)

	// Use a timeout context to prevent hanging
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	traceExporter, err := otlptracegrpc.New(
		timeoutCtx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("Failed to create trace exporter: %v", err)
		return nil, err
	}
	log.Printf("Trace exporter created successfully")

	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter,
			// Increased from 1s to 10s to match OTEL collector batch timeout
			trace.WithBatchTimeout(10*time.Second)),
	)
	return tracerProvider, nil
}

// Setup Meter Provider
func newMeterProvider(endpoint string) (*metric.MeterProvider, error) {
	log.Printf("Creating meter provider with endpoint: %s", endpoint)

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metricExporter, err := otlpmetricgrpc.New(
		timeoutCtx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("Failed to create metric exporter: %v", err)
		return nil, err
	}
	log.Printf("Metric exporter created successfully")

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			// Increased from 1s to 10s to match OTEL collector batch timeout
			// This prevents overwhelming the collector with constant exports
			metric.WithInterval(10*time.Second))),
	)
	return meterProvider, nil
}

// Setup Logger Provider
func newLoggerProvider(endpoint string) (*sdklog.LoggerProvider, error) {
	log.Printf("Creating logger provider with endpoint: %s", endpoint)

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logExporter, err := otlploggrpc.New(
		timeoutCtx,
		otlploggrpc.WithEndpoint(endpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("Failed to create log exporter: %v", err)
		return nil, err
	}
	log.Printf("Log exporter created successfully")

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter,
			// Reduce batch size to prevent buffering issues during high concurrency
			sdklog.WithMaxQueueSize(512),
			// Increased from 100ms to 2s to reduce export frequency
			sdklog.WithExportInterval(2*time.Second),
		)),
	)
	return loggerProvider, nil
}
