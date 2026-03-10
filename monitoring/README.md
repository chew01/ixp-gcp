

## Fixed Issue: Prometheus Metric Naming

OpenTelemetry counters automatically get '_total' suffix when exported to Prometheus:
- ixp.auction.bids.allocated → ixp_auction_bids_allocated_total
- ixp.auction.units.allocated (unit: kbps) → ixp_auction_units_allocated_kbps_total

The dashboard queries have been updated to use the correct suffixes.

