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
	"go.opentelemetry.io/otel"

	"github.com/chew01/ixp-gcp/shared/scenario"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Enable OTel SDK error logging to diagnose metric export failures
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("OpenTelemetry SDK Error", "error", err)
	}))

	// 1. Setup OTel (Your feature branch logic)
	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		return err
	}
	localotel.InitInstruments()

	slog.Info("API Gateway starting", "version", "1.0.0")

	// Defer OTel shutdown to ensure it runs at the very end
	// This ensures all metrics are flushed before the process exits
	defer func() {
		slog.Info("Flushing OpenTelemetry metrics and logs...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown OpenTelemetry", "error", err)
		} else {
			slog.Info("OpenTelemetry shutdown complete")
		}
	}()

	// 3. Initialize Stores (Incoming changes logic)
	scenarioPath := os.Getenv("SCENARIO_PATH")
	if scenarioPath == "" {
		scenarioPath = "/etc/scenario/scenario.yaml"
	}
	scen, err := scenario.Load(scenarioPath)
	if err != nil {
		slog.Error("failed to load scenario", "scenario_path", scenarioPath, "error", err)
		return err
	}

	// Try to initialize stores. If Atomix is unavailable, log warning and continue
	// with nil stores - handlers will record errors when they try to use them.
	fs, err := NewAtomixFlowStore(ctx)
	if err != nil {
		slog.Warn("Failed to initialize flow store (Atomix unavailable?)", "error", err)
		fs = nil
	}

	bs := NewAtomixBidStore()

	cs, err := NewAtomixCreditsStore(ctx)
	if err != nil {
		slog.Warn("Failed to initialize credits store (Atomix unavailable?)", "error", err)
		cs = nil
	}

	hs, err := NewAtomixAuctionHistoryStore(ctx)
	if err != nil {
		slog.Warn("Failed to initialize auction history store (Atomix unavailable?)", "error", err)
		hs = nil
	}

	// Ensure every customer from the scenario has a credits entry (total_spent=0) so GET /credits and Prometheus show them from the start.
	// Skip if credits store is unavailable.
	if cs != nil {
		seen := make(map[string]bool)
		for _, c := range scen.Customers {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			if err := cs.InitCustomerIfMissing(ctx, c.ID, c.StartingBalance); err != nil {
				slog.Warn("Failed to init credits for customer at startup", "customer_id", c.ID, "error", err)
			}
		}
	}

	server := &Server{
		fs:       fs,
		bs:       bs,
		cs:       cs,
		hs:       hs,
		scenario: scen,
	}

	server.InitServerMetrics()

	// 3. Main API Mux (Port 8080)
	appMux := http.NewServeMux()
	appMux.HandleFunc("/flows", server.getFlows)
	appMux.HandleFunc("/bids", server.postBid)
	appMux.HandleFunc("/credits", server.getCredits)
	appMux.HandleFunc("/auctions", server.getAuctions)

	// Wrap with OTel instrumentation
	handler := otelhttp.NewHandler(appMux, "api-gateway")

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		BaseContext:  func(net.Listener) context.Context { return ctx },
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start servers
	errChan := make(chan error, 2)
	go func() {
		slog.Info("API Gateway listening", "port", 8080)
		errChan <- srv.ListenAndServe()
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		slog.Info("Shutting down gracefully...")
	}

	// Graceful Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(srv.Shutdown(shutdownCtx))
}
