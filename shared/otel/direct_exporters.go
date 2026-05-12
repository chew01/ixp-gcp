package otel

import (
	"context"
	"errors"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/trace"
)

// SetupDirectExporters initializes native backend clients (Jaeger, Prometheus, Loki)
// instead of using OTLP exporters to the OTel Collector.
func SetupDirectExporters(ctx context.Context) (func(context.Context) error, error) {
	log.Println("Setting up direct exporters (native backend clients)")

	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup
	handleErr := func(inErr error) error {
		return errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator (same as OTLP mode)
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up Jaeger trace exporter
	tracerProvider, err := newJaegerTracerProvider()
	if err != nil {
		return shutdown, handleErr(err)
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up Prometheus metrics exporter
	meterProvider, err := newPrometheusMeterProvider()
	if err != nil {
		return shutdown, handleErr(err)
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	// Set up Loki logger
	loggerProvider, err := newLokiLoggerProvider()
	if err != nil {
		return shutdown, handleErr(err)
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)

	log.Println("Direct exporters initialized successfully")
	return shutdown, nil
}

// newJaegerTracerProvider creates a tracer provider that exports directly to Jaeger
func newJaegerTracerProvider() (*trace.TracerProvider, error) {
	// Jaeger collector endpoint
	endpoint := "http://jaeger.observability.svc.cluster.local:14250"
	log.Printf("Creating Jaeger exporter with endpoint: %s", endpoint)

	// Create Jaeger exporter
	jaegerExporter, err := jaeger.New(
		jaeger.WithCollectorEndpoint(
			jaeger.WithEndpoint(endpoint),
		),
	)
	if err != nil {
		log.Printf("Failed to create Jaeger exporter: %v", err)
		return nil, err
	}
	log.Println("Jaeger exporter created successfully")

	// Create tracer provider with batch span processor
	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(jaegerExporter,
			// Match OTLP batch timeout for fair comparison
			trace.WithBatchTimeout(time.Second),
		),
	)

	return tracerProvider, nil
}
