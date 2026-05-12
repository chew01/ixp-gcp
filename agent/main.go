package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type config struct {
	CustomerID   string
	APIBaseURL   string
	ScenarioPath string
}

type customerPorts struct {
	SwitchID string
	Ingress  []uint32
	Egress   []uint32
}

func loadConfig() (config, error) {
	customerID := os.Getenv("CUSTOMER_ID")
	if customerID == "" {
		return config{}, fmt.Errorf("CUSTOMER_ID is required")
	}
	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	scPath := os.Getenv("SCENARIO_PATH")
	if scPath == "" {
		scPath = "/etc/scenario/scenario.yaml"
	}
	return config{
		CustomerID:   customerID,
		APIBaseURL:   apiBase,
		ScenarioPath: scPath,
	}, nil
}

// customerStrategy returns the strategy name and params for the given customer
// from the scenario. Defaults to "conservative" with no params if not set.
func customerStrategy(scene *scenario.Scenario, customerID string) (name string, params map[string]string) {
	for _, c := range scene.Customers {
		if c.ID == customerID {
			name = c.Strategy
			if name == "" {
				name = "conservative"
			}
			return name, c.StrategyParams
		}
	}
	return "conservative", nil
}

func selectStrategy(name string, params map[string]string) (strategy.Bidder, error) {
	switch name {
	case "conservative":
		return strategy.Conservative{}, nil
	case "demand_corrected":
		return strategy.DemandCorrected{}, nil
	case "price_insensitive":
		return strategy.NewPriceInsensitive(params), nil
	case "budget_aware":
		return strategy.NewBudgetAware(params), nil
	case "exploratory":
		return strategy.NewExploratory(params), nil
	case "q_learning":
		return strategy.NewQLearning(params), nil
	case "valuation_based":
		return strategy.ValuationBased{}, nil
	case "throughput_optimizer":
		return strategy.NewThroughputOptimizer(params), nil
	default:
		return nil, fmt.Errorf("unknown strategy %q", name)
	}
}

func main() {
	ctx := context.Background()
	// Set up OpenTelemetry.
	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		log.Fatalf("Failed to setup otel: %v", err)
	}

	// Start Prometheus metrics HTTP server if in direct mode
	if os.Getenv("TELEMETRY_MODE") == "direct" {
		go localotel.ServePrometheusMetrics(":9090")
		log.Println("Started Prometheus metrics server on :9090")
	}
	defer func() {
		if shutdownErr := otelShutdown(ctx); shutdownErr != nil {
			log.Printf("otel shutdown error: %v", shutdownErr)
		}
	}()
	localotel.InitInstruments()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	scene, err := scenario.Load(cfg.ScenarioPath)
	if err != nil {
		log.Fatalf("failed to load scenario: %v", err)
	}

	stratName, stratParams := customerStrategy(scene, cfg.CustomerID)
	strat, err := selectStrategy(stratName, stratParams)
	if err != nil {
		log.Fatalf("strategy error: %v", err)
	}

	customerPorts := deriveCustomerPorts(scene, cfg.CustomerID)
	if len(customerPorts) == 0 {
		log.Fatalf("no ingress ports found for customer %q in scenario", cfg.CustomerID)
	}

	interval, err := time.ParseDuration(scene.AuctionInterval)
	if err != nil || interval <= 0 {
		log.Printf("invalid auction interval %q, defaulting to 30s", scene.AuctionInterval)
		interval = 30 * time.Second
	}

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("customer agent starting for %s, interval=%s, api=%s, strategy=%s", cfg.CustomerID, interval, cfg.APIBaseURL, stratName)

	// Run immediately once, then on each tick.
	if err := runOnce(ctx, client, cfg, scene, customerPorts, strat); err != nil {
		log.Printf("runOnce error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("customer agent shutting down")
			return
		case <-ticker.C:
			if err := runOnce(ctx, client, cfg, scene, customerPorts, strat); err != nil {
				log.Printf("runOnce error: %v", err)
			}
		}
	}
}

func deriveCustomerPorts(scene *scenario.Scenario, customerID string) []customerPorts {
	// Map switchID -> customerPorts
	m := make(map[string]*customerPorts)

	// Index egress ports per switch.
	egressBySwitch := make(map[string][]uint32)
	for _, sw := range scene.Switches {
		egressBySwitch[sw.ID] = append([]uint32{}, sw.EgressPorts...)
	}

	for _, c := range scene.Customers {
		if c.ID != customerID {
			continue
		}
		cp, ok := m[c.SwitchID]
		if !ok {
			cp = &customerPorts{
				SwitchID: c.SwitchID,
			}
			m[c.SwitchID] = cp
		}
		cp.Ingress = append(cp.Ingress, c.IngressPorts...)
		if len(cp.Egress) == 0 {
			cp.Egress = append(cp.Egress, egressBySwitch[c.SwitchID]...)
		}
	}

	var out []customerPorts
	for _, cp := range m {
		if len(cp.Ingress) == 0 || len(cp.Egress) == 0 {
			continue
		}
		out = append(out, *cp)
	}
	return out
}

func runOnce(ctx context.Context, client *http.Client, cfg config, scene *scenario.Scenario, ports []customerPorts, strat strategy.Bidder) error {
	ctx, span := localotel.Tracer.Start(ctx, "agent-run-once")
	defer span.End()
	span.SetAttributes(attribute.String("customer_id", cfg.CustomerID))

	log.Printf("running once for %s", cfg.CustomerID)
	credits, err := fetchCredits(ctx, client, cfg)
	if err != nil {
		span.SetStatus(codes.Error, "fetch-credits-failed")
		span.RecordError(err)
		log.Printf("failed to fetch credits: %v", err)
	}

	for _, cp := range ports {
		for _, in := range cp.Ingress {
			for _, eg := range cp.Egress {
				log.Printf("placing bid for %s in=%d eg=%d", cp.SwitchID, in, eg)
				if err := placeBidForFlow(ctx, client, cfg, scene, cp.SwitchID, in, eg, credits, strat); err != nil {
					span.RecordError(err)
					log.Printf("placeBidForFlow error for %s in=%d eg=%d: %v", cp.SwitchID, in, eg, err)
				}
			}
		}
	}
	log.Printf("finished running once for %s", cfg.CustomerID)
	return nil
}

func fetchCredits(ctx context.Context, client *http.Client, cfg config) (shared.CustomerCredits, error) {
	ctx, span := localotel.Tracer.Start(ctx, "agent-fetch-credits")
	defer span.End()
	span.SetAttributes(attribute.String("customer_id", cfg.CustomerID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.APIBaseURL+"/credits", nil)
	if err != nil {
		span.SetStatus(codes.Error, "build-request-failed")
		span.RecordError(err)
		return shared.CustomerCredits{}, err
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "http-request-failed")
		span.RecordError(err)
		return shared.CustomerCredits{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "non-200-response")
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return shared.CustomerCredits{}, fmt.Errorf("GET /credits status %d", resp.StatusCode)
	}

	var cred shared.CustomerCredits
	if err := json.NewDecoder(resp.Body).Decode(&cred); err != nil {
		span.SetStatus(codes.Error, "decode-failed")
		span.RecordError(err)
		return shared.CustomerCredits{}, err
	}
	span.SetAttributes(attribute.Int("credits.total_spent", cred.TotalSpent))
	return cred, nil
}

func placeBidForFlow(ctx context.Context, client *http.Client, cfg config, scene *scenario.Scenario, switchID string, ingress, egress uint32, credits shared.CustomerCredits, strat strategy.Bidder) error {
	ctx, span := localotel.Tracer.Start(ctx, "agent-place-bid-for-flow")
	defer span.End()
	span.SetAttributes(
		attribute.String("switch_id", switchID),
		attribute.Int64("ingress_port", int64(ingress)),
		attribute.Int64("egress_port", int64(egress)),
	)

	metrics, err := fetchFlowMetrics(ctx, client, cfg, switchID, ingress, egress)
	if err != nil {
		// If the flow is not found, skip silently.
		var httpErr *httpError
		if errorsAs(err, &httpErr) && httpErr.status == http.StatusNotFound {
			span.SetAttributes(attribute.Bool("flow_found", false))
			return nil
		}
		span.SetStatus(codes.Error, "fetch-flow-failed")
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Bool("flow_found", true))

	lastClearing, lastAllocated := fetchLastAuctionResult(ctx, client, cfg, egress)

	// Look up this customer's valuation_per_unit from the scenario.
	var valuationPerUnit int
	for _, c := range scene.Customers {
		if c.ID == cfg.CustomerID {
			valuationPerUnit = c.ValuationPerUnit
			break
		}
	}

	bidCtx := strategy.BidContext{
		Scene:              scene,
		CustomerID:         cfg.CustomerID,
		SwitchID:           switchID,
		IngressPort:        ingress,
		EgressPort:         egress,
		Metrics:            metrics,
		Credits:            credits,
		LastClearingPrice:  lastClearing,
		ValuationPerUnit:   valuationPerUnit,
		LastAllocatedUnits: lastAllocated,
	}

	units, priceU64, skip := strat.ComputeBid(bidCtx)
	if skip {
		return nil
	}

	price := int(priceU64)

	// Construct bid request.
	in64 := uint64(ingress)
	eg64 := uint64(egress)
	unitsPtr := units
	pricePtr := price

	bid := shared.BidRequest{
		IngressPort: &in64,
		EgressPort:  &eg64,
		Units:       &unitsPtr,
		UnitPrice:   &pricePtr,
	}

	payload, err := json.Marshal(bid)
	if err != nil {
		span.SetStatus(codes.Error, "marshal-bid-failed")
		span.RecordError(err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+"/bids", bytes.NewReader(payload))
	if err != nil {
		span.SetStatus(codes.Error, "build-bid-request-failed")
		span.RecordError(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "post-bid-failed")
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "bid-not-accepted")
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return fmt.Errorf("POST /bids status %d", resp.StatusCode)
	}

	span.SetAttributes(
		attribute.Int64("bid.units", int64(units)),
		attribute.Int("bid.price", price),
	)

	log.Printf("bid placed: switch=%s ingress=%d egress=%d units=%d price=%d", switchID, ingress, egress, units, price)
	return nil
}

type httpError struct {
	status int
	err    error
}

func (e *httpError) Error() string {
	return fmt.Sprintf("http status %d: %v", e.status, e.err)
}

func errorsAs(err error, target interface{}) bool {
	switch t := target.(type) {
	case **httpError:
		if he, ok := err.(*httpError); ok {
			*t = he
			return true
		}
	}
	return false
}

func fetchFlowMetrics(ctx context.Context, client *http.Client, cfg config, switchID string, ingress, egress uint32) (shared.FlowMetricsValue, error) {
	ctx, span := localotel.Tracer.Start(ctx, "agent-fetch-flow-metrics")
	defer span.End()
	span.SetAttributes(
		attribute.String("switch_id", switchID),
		attribute.Int64("ingress_port", int64(ingress)),
		attribute.Int64("egress_port", int64(egress)),
	)

	url := fmt.Sprintf("%s/flows?switch_id=%s&ingress_port=%d&egress_port=%d", cfg.APIBaseURL, switchID, ingress, egress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		span.SetStatus(codes.Error, "build-request-failed")
		span.RecordError(err)
		return shared.FlowMetricsValue{}, err
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "http-request-failed")
		span.RecordError(err)
		return shared.FlowMetricsValue{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		span.SetStatus(codes.Error, "flow-not-found")
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return shared.FlowMetricsValue{}, &httpError{status: resp.StatusCode, err: fmt.Errorf("flow not found")}
	}
	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "non-200-response")
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return shared.FlowMetricsValue{}, fmt.Errorf("GET /flows status %d", resp.StatusCode)
	}

	var body map[string]shared.FlowMetricsValue
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		span.SetStatus(codes.Error, "decode-failed")
		span.RecordError(err)
		return shared.FlowMetricsValue{}, err
	}
	for _, v := range body {
		span.SetAttributes(
			attribute.Float64("flow.throughput_kbps", v.ThroughputKbps),
			attribute.Float64("flow.drop_kbps", v.DropKbps),
		)
		return v, nil
	}
	span.SetStatus(codes.Error, "empty-flow-response")
	return shared.FlowMetricsValue{}, fmt.Errorf("empty flow metrics response")
}

// fetchLastAuctionResult returns the clearing price and the caller's allocated
// units from the most recent auction record for the given egress port.
func fetchLastAuctionResult(ctx context.Context, client *http.Client, cfg config, egress uint32) (clearingPrice int, allocatedUnits uint64) {
	ctx, span := localotel.Tracer.Start(ctx, "agent-fetch-last-clearing-price")
	defer span.End()
	span.SetAttributes(attribute.Int64("egress_port", int64(egress)))

	url := fmt.Sprintf("%s/auctions?egress_port=%d", cfg.APIBaseURL, egress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		span.RecordError(err)
		return 0, 0
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return 0, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return 0, 0
	}

	var records []shared.AuctionHistoryRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		span.RecordError(err)
		return 0, 0
	}
	if len(records) == 0 {
		return 0, 0
	}

	last := records[len(records)-1]

	var units uint64
	for _, alloc := range last.Allocations {
		if alloc.CustomerID == cfg.CustomerID {
			units += alloc.Units
		}
	}

	span.SetAttributes(attribute.Int("auction.last_clearing_price", last.ClearingPrice))
	span.SetAttributes(attribute.Int("auction.units", int(units)))
	return last.ClearingPrice, units
}
