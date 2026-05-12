package otel

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

var promRegistry *prometheus.Registry

// newPrometheusMeterProvider creates a meter provider that exposes metrics via Prometheus HTTP endpoint
func newPrometheusMeterProvider() (*metric.MeterProvider, error) {
	log.Println("Creating Prometheus exporter")

	// Create Prometheus registry
	promRegistry = prometheus.NewRegistry()

	// Create Prometheus exporter that bridges OTel metrics to Prometheus
	promExporter, err := prometheusexporter.New(
		prometheusexporter.WithRegisterer(promRegistry),
	)
	if err != nil {
		log.Printf("Failed to create Prometheus exporter: %v", err)
		return nil, err
	}
	log.Println("Prometheus exporter created successfully")

	// Create meter provider with Prometheus exporter as reader
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(promExporter),
	)

	return meterProvider, nil
}

// ServePrometheusMetrics starts an HTTP server to expose Prometheus metrics
// This should be called in a goroutine from the service main function
func ServePrometheusMetrics(addr string) {
	if promRegistry == nil {
		log.Println("WARNING: Prometheus registry not initialized, cannot serve metrics")
		return
	}

	log.Printf("Starting Prometheus metrics HTTP server on %s", addr)

	// Create HTTP handler for Prometheus metrics
	http.Handle("/metrics", promhttp.HandlerFor(
		promRegistry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	// Start HTTP server
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Printf("ERROR: Prometheus metrics server failed: %v", err)
	}
}
