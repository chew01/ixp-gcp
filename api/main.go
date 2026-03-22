package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

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

	// 1. Setup OTel (Your feature branch logic)
	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		return err
	}
	localotel.InitInstruments()
	defer func() {
		_ = otelShutdown(context.Background())
	}()

	// 2. Initialize Stores (Incoming changes logic)
	scenarioPath := os.Getenv("SCENARIO_PATH")
	if scenarioPath == "" {
		scenarioPath = "/etc/scenario/scenario.yaml"
	}
	scen, err := scenario.Load(scenarioPath)
	if err != nil {
		log.Fatalf("failed to load scenario: %v", err)
	}

	fs, err := NewAtomixFlowStore(ctx)
	if err != nil {
		return err
	}
	bs := NewAtomixBidStore()
	cs, err := NewAtomixCreditsStore(ctx)
	if err != nil {
		log.Fatalf("failed to create credits store: %v", err)
	}
	hs, err := NewAtomixAuctionHistoryStore(ctx)
	if err != nil {
		log.Fatalf("failed to create auction history store: %v", err)
	}
	// Ensure every customer from the scenario has a credits entry (total_spent=0) so GET /credits and Prometheus show them from the start.
	seen := make(map[string]bool)
	for _, c := range scen.Customers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if err := cs.InitCustomerIfMissing(ctx, c.ID); err != nil {
			log.Fatalf("failed to init credits for customer %s: %v", c.ID, err)
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
