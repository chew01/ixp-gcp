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
	"github.com/chew01/ixp-gcp/shared/scenario"
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
	case "backoff":
		return strategy.NewBackoff(params), nil
	case "budget_aware":
		return strategy.BudgetAware{}, nil
	case "exploratory":
		return strategy.NewExploratory(params), nil
	case "q_learning":
		return strategy.NewQLearning(params), nil
	default:
		return nil, fmt.Errorf("unknown strategy %q", name)
	}
}

func main() {
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
		Timeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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
	log.Printf("running once for %s", cfg.CustomerID)
	credits, err := fetchCredits(ctx, client, cfg)
	if err != nil {
		log.Printf("failed to fetch credits: %v", err)
	}

	for _, cp := range ports {
		for _, in := range cp.Ingress {
			for _, eg := range cp.Egress {
				log.Printf("placing bid for %s in=%d eg=%d", cp.SwitchID, in, eg)
				if err := placeBidForFlow(ctx, client, cfg, scene, cp.SwitchID, in, eg, credits, strat); err != nil {
					log.Printf("placeBidForFlow error for %s in=%d eg=%d: %v", cp.SwitchID, in, eg, err)
				}
			}
		}
	}
	log.Printf("finished running once for %s", cfg.CustomerID)
	return nil
}

func fetchCredits(ctx context.Context, client *http.Client, cfg config) (shared.CustomerCredits, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.APIBaseURL+"/credits", nil)
	if err != nil {
		return shared.CustomerCredits{}, err
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		return shared.CustomerCredits{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return shared.CustomerCredits{}, fmt.Errorf("GET /credits status %d", resp.StatusCode)
	}

	var cred shared.CustomerCredits
	if err := json.NewDecoder(resp.Body).Decode(&cred); err != nil {
		return shared.CustomerCredits{}, err
	}
	return cred, nil
}

func placeBidForFlow(ctx context.Context, client *http.Client, cfg config, scene *scenario.Scenario, switchID string, ingress, egress uint32, credits shared.CustomerCredits, strat strategy.Bidder) error {
	metrics, err := fetchFlowMetrics(ctx, client, cfg, switchID, ingress, egress)
	if err != nil {
		// If the flow is not found, skip silently.
		var httpErr *httpError
		if errorsAs(err, &httpErr) && httpErr.status == http.StatusNotFound {
			return nil
		}
		return err
	}

	lastClearing := fetchLastClearingPrice(ctx, client, cfg, egress)

	bidCtx := strategy.BidContext{
		Scene:             scene,
		CustomerID:        cfg.CustomerID,
		SwitchID:          switchID,
		IngressPort:       ingress,
		EgressPort:        egress,
		Metrics:           metrics,
		Credits:           credits,
		LastClearingPrice: lastClearing,
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
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+"/bids", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /bids status %d", resp.StatusCode)
	}

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
	url := fmt.Sprintf("%s/flows?switch_id=%s&ingress_port=%d&egress_port=%d", cfg.APIBaseURL, switchID, ingress, egress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return shared.FlowMetricsValue{}, err
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		return shared.FlowMetricsValue{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return shared.FlowMetricsValue{}, &httpError{status: resp.StatusCode, err: fmt.Errorf("flow not found")}
	}
	if resp.StatusCode != http.StatusOK {
		return shared.FlowMetricsValue{}, fmt.Errorf("GET /flows status %d", resp.StatusCode)
	}

	var body map[string]shared.FlowMetricsValue
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return shared.FlowMetricsValue{}, err
	}
	for _, v := range body {
		return v, nil
	}
	return shared.FlowMetricsValue{}, fmt.Errorf("empty flow metrics response")
}

func fetchLastClearingPrice(ctx context.Context, client *http.Client, cfg config, egress uint32) int {
	url := fmt.Sprintf("%s/auctions?egress_port=%d", cfg.APIBaseURL, egress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("X-Customer-ID", cfg.CustomerID)

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var records []shared.AuctionHistoryRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return 0
	}
	if len(records) == 0 {
		return 0
	}

	// Take the last record in the slice as "most recent".
	last := records[len(records)-1]
	return last.ClearingPrice
}
