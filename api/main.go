package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Handle SIGINT (CTRL+C) gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Set up OpenTelemetry.
	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		return err
	}

	// Initialize OTel-integrated logger (must be after OTel SDK setup)
	localotel.InitInstruments()

	// Handle shutdown properly so nothing leaks.
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// Start HTTP server.
	srv := &http.Server{
		Addr:         ":8080",
		BaseContext:  func(net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      newHTTPHandler(),
	}

	slog.Info("HTTP telemetry API listening on :8080")

	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.ListenAndServe()
	}()

	// Wait for interruption.
	select {
	case err = <-srvErr:
		// Error when starting HTTP server.
		return err
	case <-ctx.Done():
		// Wait for first CTRL+C.
		// Stop receiving signal notifications as soon as possible.
		stop()
	}

	// When Shutdown is called, ListenAndServe immediately returns ErrServerClosed.
	err = srv.Shutdown(context.Background())
	return err
}

func newHTTPHandler() http.Handler {
	server := &Server{
		fs: &AtomixFlowStore{},
		bs: &AtomixBidStore{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/flows", server.getFlows)
	mux.HandleFunc("/bids", server.postBid)
	mux.HandleFunc("/metrics", server.getMetrics)
	// No instrumentation for healthz check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Add HTTP instrumentation for the whole server.
	handler := otelhttp.NewHandler(mux, "http-api-gateway")
	return handler
}
