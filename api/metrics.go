// metrics.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	serverMetricsOnce    sync.Once
	flowThroughput       metric.Float64ObservableGauge
	flowDropRate         metric.Float64ObservableGauge
	flowDropKbps         metric.Float64ObservableGauge
	flowEgressKbps       metric.Float64ObservableGauge
	customerCreditsSpent metric.Float64ObservableGauge
	auctionClearingPrice metric.Float64ObservableGauge
	customerAllocation   metric.Float64ObservableGauge
	agentUtilityTotal    metric.Float64ObservableGauge
	bidPriceHistogram    metric.Int64Histogram
	bidUnitHistogram     metric.Int64Histogram
	bidCounter           metric.Int64Counter
	apiPolicyViolations  metric.Int64Counter
)

// InitServerMetrics initializes the instruments using the Meter
// created by your boilerplate code.
func (s *Server) InitServerMetrics() {
	serverMetricsOnce.Do(func() {
		var err error

		// Callback for flow throughput metric
		callback_refreshThroughput := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("failed to parse flow metrics for %s: %v", flowKey, err))
					continue
				}

				attrs := attribute.NewSet(
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.ThroughputKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow drop rate metric
		callback_refreshDropRate := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.DropRatePct, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow drop kbps metric
		callback_refreshDropKbps := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.DropKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow egress kbps metric
		callback_refreshEgressKbps := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.EgressKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for customer credits metric
		callback_refreshCreditMetrics := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.cs.List(timeoutCtx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list credits keys: %v", err))
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, customerID := range keys {
				cred, err := s.cs.Get(ctx, customerID)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("customer_id", customerID),
				)

				observer.Observe(float64(cred.TotalSpent), metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for latest auction metrics per egress port.
		callback_refreshAuctionMetrics := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.hs.List(timeoutCtx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list auction history keys: %v", err))
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			latest := make(map[uint64]*shared.AuctionHistoryRecord)
			for _, key := range keys {
				raw, err := s.hs.Get(ctx, key)
				if err != nil || raw == "" {
					continue
				}

				var rec shared.AuctionHistoryRecord
				if err := json.Unmarshal([]byte(raw), &rec); err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("failed to unmarshal auction history %s: %v", key, err))
					continue
				}

				prev, ok := latest[rec.EgressPort]
				if !ok || rec.Interval > prev.Interval {
					r := rec
					latest[rec.EgressPort] = &r
				}
			}

			for egressPort, rec := range latest {
				egressAttrs := attribute.NewSet(attribute.Int64("egress_port", int64(egressPort)))
				observer.Observe(float64(rec.ClearingPrice), metric.WithAttributeSet(egressAttrs))

				totalsByCustomer := make(map[string]float64)
				for _, alloc := range rec.Allocations {
					totalsByCustomer[alloc.CustomerID] += float64(alloc.Units)
				}
				for customerID, total := range totalsByCustomer {
					attrs := attribute.NewSet(
						attribute.String("customer_id", customerID),
						attribute.Int64("egress_port", int64(egressPort)),
					)
					observer.Observe(total, metric.WithAttributeSet(attrs))
				}
			}

			return nil
		}

		// Callback for agent utility metric (NEW: from teammates)
		callback_refreshAgentUtility := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.us.List(timeoutCtx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list utility keys: %v", err))
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, customerID := range keys {
				total, err := s.us.Get(ctx, customerID)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("customer_id", customerID),
				)

				observer.Observe(float64(total), metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Create flow throughput gauge
		flowThroughput, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.throughput",
			metric.WithDescription("Flow throughput in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshThroughput),
		)
		if err != nil {
			slog.Error("Failed to initialise flowThroughput metric", "error", err)
		}

		// Create flow drop rate gauge
		flowDropRate, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.drop_rate",
			metric.WithDescription("Flow packet drop rate as a percentage"),
			metric.WithUnit("%"),
			metric.WithFloat64Callback(callback_refreshDropRate),
		)
		if err != nil {
			slog.Error("Failed to initialise flowDropRate metric", "error", err)
		}

		// Create flow drop kbps gauge
		flowDropKbps, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.drop_kbps",
			metric.WithDescription("Flow packet drop rate in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshDropKbps),
		)
		if err != nil {
			slog.Error("Failed to initialise flowDropKbps metric", "error", err)
		}

		// Create flow egress kbps gauge
		flowEgressKbps, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.egress_kbps",
			metric.WithDescription("Flow egress bandwidth in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshEgressKbps),
		)
		if err != nil {
			slog.Error("Failed to initialise flowEgressKbps metric", "error", err)
		}

		// Create customer credits spent gauge
		customerCreditsSpent, err = localotel.Meter.Float64ObservableGauge(
			"ixp.customer.credits_spent",
			metric.WithDescription("Total credits spent by customer (accounting only)"),
			metric.WithUnit("credits"),
			metric.WithFloat64Callback(callback_refreshCreditMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise customerCreditsSpent metric", "error", err)
		}

		auctionClearingPrice, err = localotel.Meter.Float64ObservableGauge(
			"ixp.auction.clearing_price",
			metric.WithDescription("Latest auction clearing price per egress port"),
			metric.WithUnit("price"),
			metric.WithFloat64Callback(callback_refreshAuctionMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise auctionClearingPrice metric", "error", err)
		}

		customerAllocation, err = localotel.Meter.Float64ObservableGauge(
			"ixp.customer.allocation_kbps",
			metric.WithDescription("Allocated bandwidth in Kbps per customer per egress port (latest auction round)"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshAuctionMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise customerAllocation metric", "error", err)
		}

		// Create agent utility total gauge (NEW: from teammates, converted to OTEL)
		agentUtilityTotal, err = localotel.Meter.Float64ObservableGauge(
			"ixp.agent.utility_total",
			metric.WithDescription("Cumulative agent utility across all rounds: sum of (valuation_per_unit - clearing_price) * allocated_units"),
			metric.WithUnit("1"),
			metric.WithFloat64Callback(callback_refreshAgentUtility),
		)
		if err != nil {
			slog.Error("Failed to initialize agentUtilityTotal metric", "error", err)
		}

		// Initialize Bid Price Histogram
		bidPriceHistogram, err = localotel.Meter.Int64Histogram(
			"ixp.bid.price",
			metric.WithDescription("Distribution of bid unit prices"),
			metric.WithUnit("SGD"), // or whatever your currency is
		)
		if err != nil {
			slog.Error("Failed to initialize bidPriceHistogram", "error", err)
		}

		// Initialize Bid Units Histogram (Bandwidth demand)
		bidUnitHistogram, err = localotel.Meter.Int64Histogram(
			"ixp.bid.units",
			metric.WithDescription("Distribution of bandwidth units requested"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("Failed to initialize bid histograms", "error", err)
		}

		// Initialize Bid Counter (for DDoS/flood detection)
		bidCounter, err = localotel.Meter.Int64Counter(
			"ixp.bid.submitted",
			metric.WithDescription("Total number of bid requests submitted"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("Failed to initialize bidCounter", "error", err)
		}

		apiPolicyViolations, err = localotel.Meter.Int64Counter(
			"ixp.api.policy.violations",
			metric.WithDescription("HTTP 400/403 errors from invalid agent bids"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("Failed to initialize apiPolicyViolations", "error", err)
		} else {
			apiPolicyViolations.Add(context.Background(), 1, metric.WithAttributes(attribute.String("reason", "startup_bootstrap")))
			slog.Info("Bootstrap increment emitted", "metric", "ixp_api_policy_violations_total", "reason", "startup_bootstrap")
		}

	})
}

// recordAtomixLatency is a convenience wrapper around localotel.RecordAtomixOperation
// Use this in the API gateway
func recordAtomixLatency(ctx context.Context, operation string, startTime time.Time, err error) {
	localotel.RecordAtomixOperation(ctx, operation, startTime, err)
}

func recordAPIPolicyViolation(ctx context.Context, reason string) {
	if apiPolicyViolations == nil {
		slog.ErrorContext(ctx, "apiPolicyViolations counter is nil", "reason", reason)
		return
	}
	ctxMetric := context.WithoutCancel(ctx)
	apiPolicyViolations.Add(ctxMetric, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// recordBidPrice records a bid unit price to the histogram
func recordBidPrice(ctx context.Context, price int64) {
	if bidPriceHistogram == nil {
		slog.ErrorContext(ctx, "bidPriceHistogram is nil")
		return
	}
	bidPriceHistogram.Record(ctx, price)
}

// recordBidUnits records bandwidth units (demand) to the histogram
func recordBidUnits(ctx context.Context, units int64) {
	if bidUnitHistogram == nil {
		slog.ErrorContext(ctx, "bidUnitHistogram is nil")
		return
	}
	bidUnitHistogram.Record(ctx, units)
}
