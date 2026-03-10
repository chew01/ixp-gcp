# Auction Metrics Documentation

## Overview
The auction runner emits OpenTelemetry metrics to track allocation efficiency, clearing prices, and bandwidth utilization. These metrics are exported to Prometheus via the OTel Collector and visualized in Grafana.

## Instruments

### Counters

#### `ixp.auction.runs.total`
- **Type:** Counter
- **Unit:** `1`
- **Labels:** `egress_port`
- **Description:** Total number of auction runs executed.
- **Use Case:** Track auction execution rate per egress port.
- **Example Query:**
  ```promql
  sum(rate(ixp_auction_runs_total[1m])) * 60
  ```

#### `ixp.auction.bids.total`
- **Type:** Counter
- **Unit:** `1`
- **Labels:** `egress_port`
- **Description:** Total number of bids observed by auction runs.
- **Use Case:** Track bid volume and demand intensity.
- **Example Query:**
  ```promql
  sum by (egress_port) (rate(ixp_auction_bids_total[1m]))
  ```

#### `ixp.auction.bids.allocated`
- **Type:** Counter
- **Unit:** `1`
- **Labels:** `egress_port`
- **Description:** Total number of bids that received an allocation (allocated units > 0).
- **Use Case:** Calculate allocation success rate.
- **Example Query:**
  ```promql
  sum(rate(ixp_auction_bids_allocated[1m])) / sum(rate(ixp_auction_bids_total[1m]))
  ```

#### `ixp.auction.bids.unallocated`
- **Type:** Counter
- **Unit:** `1`
- **Labels:** `egress_port`
- **Description:** Total number of bids that received no allocation.
- **Use Case:** Identify capacity shortages or low-price bids.
- **Example Query:**
  ```promql
  sum(rate(ixp_auction_bids_unallocated_total[1m]))
  ```

#### `ixp.auction.units.requested`
- **Type:** Counter
- **Unit:** `kbps`
- **Labels:** `egress_port`
- **Description:** Total bandwidth (in Kbps) requested by all bids.
- **Use Case:** Measure total bandwidth demand.
- **Example Query:**
  ```promql
  sum by (egress_port) (rate(ixp_auction_units_requested[1m]))
  ```

#### `ixp.auction.units.allocated`
- **Type:** Counter
- **Unit:** `kbps`
- **Labels:** `egress_port`
- **Description:** Total bandwidth (in Kbps) allocated by auctions.
- **Use Case:** Compare allocated vs. requested bandwidth.
- **Example Query:**
  ```promql
  sum(rate(ixp_auction_units_allocated[1m])) / sum(rate(ixp_auction_units_requested[1m]))
  ```

### Histograms

#### `ixp.auction.clearing_price`
- **Type:** Histogram
- **Unit:** `SGD`
- **Labels:** `egress_port`
- **Description:** Distribution of auction clearing prices.
- **Use Case:** Analyze price volatility and median clearing prices.
- **Example Queries:**
  - **P50 Clearing Price:**
    ```promql
    histogram_quantile(0.5, sum by (le) (rate(ixp_auction_clearing_price_SGD_bucket[5m])))
    ```
  - **P99 Clearing Price:**
    ```promql
    histogram_quantile(0.99, sum by (le) (rate(ixp_auction_clearing_price_SGD_bucket[5m])))
    ```
  - **Heatmap:**
    ```promql
    sum by (le) (rate(ixp_auction_clearing_price_SGD_bucket[1m]))
    ```

#### `ixp.auction.bid_allocation_ratio`
- **Type:** Histogram
- **Unit:** `1` (ratio 0–1)
- **Labels:** `egress_port`
- **Description:** Per-run ratio of allocated bids to total bids.
- **Use Case:** Track allocation efficiency per auction cycle.
- **Example Query:**
  ```promql
  histogram_quantile(0.9, sum by (le) (rate(ixp_auction_bid_allocation_ratio_bucket[5m])))
  ```

#### `ixp.auction.unit_allocation_ratio`
- **Type:** Histogram
- **Unit:** `1` (ratio 0–1)
- **Labels:** `egress_port`
- **Description:** Per-run ratio of allocated units to requested units.
- **Use Case:** Measure how much bandwidth demand is satisfied.
- **Example Query:**
  ```promql
  histogram_quantile(0.5, sum by (le) (rate(ixp_auction_unit_allocation_ratio_bucket[5m])))
  ```

---

## Key Metrics & Ratios

### Bid Allocation Ratio (Overall)
**Formula:**
```promql
(sum(ixp_auction_bids_allocated_total) / sum(ixp_auction_bids_total)) * 100
```
**Interpretation:** Percentage of bids that receive bandwidth (any amount > 0).

### Unit Allocation Ratio (Overall)
**Formula:**
```promql
(sum(ixp_auction_units_allocated_kbps_total) / sum(ixp_auction_units_requested_kbps_total)) * 100
```
**Interpretation:** Percentage of requested bandwidth that is allocated (capacity utilization).

### Unallocated Bid Rate
**Formula:**
```promql
sum(rate(ixp_auction_bids_unallocated_total[1m]))
```
**Interpretation:** Rate of bids failing to secure bandwidth (indicates congestion or low prices).

---

## Grafana Dashboard

The `ixp-auction.json` dashboard includes:

### Top Row (Stats & Gauges)
1. **Total Auctions**: Cumulative count of all auction runs
2. **Total Bids**: Cumulative count of all bids processed
3. **Bid Allocation Rate**: Percentage of bids that received bandwidth (gauge)
4. **Bandwidth Allocation Rate**: Percentage of requested bandwidth fulfilled (gauge)
5. **Current Clearing Price (P50)**: Latest median clearing price
6. **Bid Rejection Rate**: Percentage of bids that received zero allocation

### Time Series Panels
7. **Bid Allocation vs Rejection**: Stacked area showing allocated vs unallocated bid rates
8. **Bandwidth Demand vs Allocation**: Comparison of requested vs allocated bandwidth
9. **Clearing Price Trends**: P50/P90/P99 quantile trends over time
10. **Auction Run Rate**: Execution frequency per egress port

---

## Additional Metric Ideas (Future)

- **Auction Duration Histogram**: Measure how long each auction cycle takes.
- **Bid Price Histogram (per auction)**: Track bid price distribution within each auction.
- **Capacity Utilization Gauge**: `allocated_units / capacity` per egress port.
- **Bid Rejection Reasons Counter**: Count bids rejected due to price vs. capacity.
- **Average Allocated Units per Bid**: `allocated_units / allocated_bids`.
- **Overbidding Rate**: Count bids with requested units > capacity.

---

## Troubleshooting

- **Metrics not appearing in Prometheus?**
  - Verify OTel Collector exporter is configured with `namespace: ixp`.
  - Check Prometheus scrape config points to `otel-collector-opentelemetry-collector.observability.svc.cluster.local:8889`.
  - Inspect OTel Collector logs: `kubectl logs -n observability deploy/otel-collector-opentelemetry-collector`.

- **Panels showing "No data"?**
  - Check if allocation counters are incrementing: `ixp_auction_bids_allocated_total`, `ixp_auction_units_allocated_kbps_total`
  - If counters remain at 0, auctions may not be allocating bandwidth (all bids rejected due to low price or insufficient capacity)
  - Verify in Prometheus: `ixp_auction_bids_total > 0` (should show bid activity)
  - Use absolute counter queries instead of rate-based ratios for troubleshooting: `sum(ixp_auction_bids_allocated_total)`

- **Histogram buckets too coarse/fine?**
  - Adjust OTel SDK histogram bucket boundaries in `shared/otel/otel.go` if needed.
  - Default buckets: `[0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000]`.

- **Allocation ratio always 0 or 100%?**
  - Ensure auctions have multiple bidders with varying prices.
  - Check reservation price configuration in scenario YAML.
  - Verify capacity vs demand: If capacity >> demand, ratio will be 100%. If capacity << demand, ratio may be very low.

---

## Implementation Notes

- Metrics are initialized once in `initAuctionMetrics()` via `sync.Once` to prevent re-registration errors.
- Allocation tracking uses a set (`allocatedBidSet`) to count unique ingress ports that received non-zero allocations.
- Zero-allocation auctions (no bids or no capacity) correctly record 0% allocation ratios.
- All metrics use low-cardinality labels (`egress_port` only) to avoid explosion in Prometheus.
