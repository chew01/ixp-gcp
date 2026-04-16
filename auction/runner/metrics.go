package runner

import (
	"context"
	"log/slog"
	"sync"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	auctionMetricsOnce               sync.Once
	auctionRunsCounter               metric.Int64Counter
	auctionBidsReceivedCounter       metric.Int64Counter
	auctionNoBidIntervalsCounter     metric.Int64Counter
	auctionRequestedUnitsCounter     metric.Int64Counter
	auctionClearingPriceLatest       metric.Int64Gauge
	auctionErrorsCounter             metric.Int64Counter
	auctionKafkaProduceErrorsCounter metric.Int64Counter
)

// InitAuctionMetrics initializes all auction runner OTEL metrics
func InitAuctionMetrics() {
	auctionMetricsOnce.Do(func() {
		var err error

		auctionRunsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.runs.total",
			metric.WithDescription("Total number of auction runs"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionRunsCounter", "error", err)
		}

		auctionBidsReceivedCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.bids.received.total",
			metric.WithDescription("Total number of bids received by auction runner"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionBidsReceivedCounter", "error", err)
		}

		auctionNoBidIntervalsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.no_bid_intervals.total",
			metric.WithDescription("Total number of auction intervals with zero bids"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionNoBidIntervalsCounter", "error", err)
		}

		auctionRequestedUnitsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.units.requested",
			metric.WithDescription("Total number of bandwidth units requested by submitted bids"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionRequestedUnitsCounter", "error", err)
		}

		auctionClearingPriceLatest, err = localotel.Meter.Int64Gauge(
			"ixp.auction.clearing_price.latest",
			metric.WithDescription("Latest auction clearing price per egress port"),
			metric.WithUnit("SGD"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionClearingPriceLatest", "error", err)
		}

		auctionErrorsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.errors.total",
			metric.WithDescription("Critical dependency or logic errors"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionErrorsCounter", "error", err)
		}

		auctionKafkaProduceErrorsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.kafka.produce.errors",
			metric.WithDescription("Failed Kafka produce operations when publishing auction results"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionKafkaProduceErrorsCounter", "error", err)
		}

	})
}

// RecordAuctionRun increments the total auction run counter
func RecordAuctionRun(ctx context.Context, egressPort int64) {
	if auctionRunsCounter == nil {
		slog.Error("auctionRunsCounter is nil")
		return
	}
	auctionRunsCounter.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes())
}

// RecordBidsReceived increments total bids received by auction runner
func RecordBidsReceived(ctx context.Context, bids int64, attrs metric.MeasurementOption) {
	if auctionBidsReceivedCounter == nil {
		slog.Error("auctionBidsReceivedCounter is nil")
		return
	}
	if bids <= 0 {
		return
	}
	auctionBidsReceivedCounter.Add(context.WithoutCancel(ctx), bids, attrs)
}

// RecordNoBidInterval increments when an interval has zero bids
func RecordNoBidInterval(ctx context.Context, attrs metric.MeasurementOption) {
	if auctionNoBidIntervalsCounter == nil {
		slog.Error("auctionNoBidIntervalsCounter is nil")
		return
	}
	auctionNoBidIntervalsCounter.Add(context.WithoutCancel(ctx), 1, attrs)
}

// RecordRequestedUnits records the total bandwidth units requested in an auction
func RecordRequestedUnits(ctx context.Context, units int64) {
	if auctionRequestedUnitsCounter == nil {
		slog.Error("auctionRequestedUnitsCounter is nil")
		return
	}
	auctionRequestedUnitsCounter.Add(context.WithoutCancel(ctx), units)
}

// RecordClearingPrice records the latest clearing price for an egress port
func RecordClearingPrice(ctx context.Context, egressPort, price int64) {
	if auctionClearingPriceLatest == nil {
		slog.Error("auctionClearingPriceLatest is nil")
		return
	}
	auctionClearingPriceLatest.Record(context.WithoutCancel(ctx), price, metric.WithAttributes())
}

// RecordAuctionError increments the error counter
func RecordAuctionError(ctx context.Context) {
	if auctionErrorsCounter == nil {
		slog.Error("auctionErrorsCounter is nil")
		return
	}
	auctionErrorsCounter.Add(context.WithoutCancel(ctx), 1)
}

// RecordAuctionKafkaProduceError increments the Kafka produce error counter.
func RecordAuctionKafkaProduceError(ctx context.Context, topic string) {
	if auctionKafkaProduceErrorsCounter == nil {
		slog.Error("auctionKafkaProduceErrorsCounter is nil")
		return
	}
	auctionKafkaProduceErrorsCounter.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(attribute.String("topic", topic)))
}
