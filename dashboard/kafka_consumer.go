package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	pb "github.com/chew01/ixp-gcp/shared/proto/pb"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

// AuctionConsumer subscribes to the auction-results Kafka topic and emits
// "auction" WebSocket events whenever a message arrives.
type AuctionConsumer struct {
	reader      *kafka.Reader
	store       *DashboardStore
	hub         *Hub
	bootstrap   string
	lastMsg     atomic.Int64 // Unix nano of last received message
	brokerCount atomic.Int32
}

func NewAuctionConsumer(bootstrap, topic string, store *DashboardStore, hub *Hub) (*AuctionConsumer, error) {
	dialer, err := buildDialer()
	if err != nil {
		return nil, err
	}
	cfg := kafka.ReaderConfig{
		Brokers: []string{bootstrap},
		Topic:   topic,
		GroupID: "dashboard-service",
	}
	if dialer != nil {
		cfg.Dialer = dialer
	}
	return &AuctionConsumer{
		reader:    kafka.NewReader(cfg),
		store:     store,
		hub:       hub,
		bootstrap: bootstrap,
	}, nil
}

// BrokerCount returns the last known number of Kafka brokers (0 if unknown).
func (c *AuctionConsumer) BrokerCount() int { return int(c.brokerCount.Load()) }

func (c *AuctionConsumer) refreshBrokerCount() {
	conn, err := kafka.Dial("tcp", c.bootstrap)
	if err != nil {
		return
	}
	defer conn.Close()
	brokers, err := conn.Brokers()
	if err != nil {
		return
	}
	c.brokerCount.Store(int32(len(brokers)))
}

func (c *AuctionConsumer) Close() { c.reader.Close() }

// KafkaHealthy returns true when a Kafka message has arrived in the last 60 s.
func (c *AuctionConsumer) KafkaHealthy() bool {
	last := c.lastMsg.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < 60*time.Second
}

// Run blocks and consumes messages. Call in a goroutine.
func (c *AuctionConsumer) Run(ctx context.Context) {
	go func() {
		c.refreshBrokerCount()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refreshBrokerCount()
			}
		}
	}()

	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("auction consumer: shutting down")
				return
			}
			log.Printf("auction consumer: read error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		c.lastMsg.Store(time.Now().UnixNano())
		c.handle(ctx, msg)
	}
}

func (c *AuctionConsumer) handle(ctx context.Context, msg kafka.Message) {
	var result pb.AuctionResult
	if err := proto.Unmarshal(msg.Value, &result); err != nil {
		log.Printf("auction consumer: unmarshal: %v", err)
		return
	}

	flow := result.GetFlowId()
	if flow == nil {
		return
	}

	egressPort := uint64(flow.GetEgressPort())

	// Emit auction-runner → Kafka animation.
	if ev, err := newEvent("auction", "auction-runner", "kafka", map[string]any{
		"bandwidth_kbps": result.GetBandwidthKbps(),
		"egress_port":    egressPort,
	}); err == nil {
		c.hub.Broadcast(ev)
	}

	// Emit auction-runner → Atomix (history write) animation.
	if ev, err := newEvent("atomix_rw", "auction-runner", "atomix",
		AtomixRWPayload{Op: "write", Map: "auction-history"},
	); err == nil {
		c.hub.Broadcast(ev)
	}

	// Fetch the latest full AuctionHistoryRecord for a richer popup.
	rec, err := c.store.LatestAuction(ctx, egressPort)
	if err != nil || rec == nil {
		// Fall back to a minimal record derived from the proto message.
		rec = &shared.AuctionHistoryRecord{
			EgressPort: egressPort,
		}
	}

	payload := AuctionPayload{
		AuctionHistoryRecord: *rec,
		Timestamp:            time.Now(),
	}
	if ev, err := newEvent("auction_detail", "auction-runner", "atomix", payload); err == nil {
		c.hub.Broadcast(ev)
	}
}

// buildDialer returns a TLS-configured Kafka dialer when TLS env vars are set.
func buildDialer() (*kafka.Dialer, error) {
	caFile := os.Getenv("KAFKA_TLS_CA_FILE")
	certFile := os.Getenv("KAFKA_TLS_CERT_FILE")
	keyFile := os.Getenv("KAFKA_TLS_KEY_FILE")

	if caFile == "" && certFile == "" {
		return nil, nil
	}

	tlsCfg := &tls.Config{}

	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caPEM)
		tlsCfg.RootCAs = pool
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return &kafka.Dialer{TLS: tlsCfg}, nil
}
