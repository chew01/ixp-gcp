package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

var (
	lokiClient     *LokiHTTPClient
	lokiClientOnce sync.Once
)

// LokiHTTPClient sends logs directly to Loki's HTTP API
type LokiHTTPClient struct {
	endpoint    string
	serviceName string
	httpClient  *http.Client
	logBatch    []LokiLogEntry
	batchMutex  sync.Mutex
	stopCh      chan struct{}
}

// LokiLogEntry represents a single log entry
type LokiLogEntry struct {
	Timestamp time.Time
	Message   string
	Level     string
}

// LokiPushRequest represents the Loki push API request format
type LokiPushRequest struct {
	Streams []LokiStream `json:"streams"`
}

// LokiStream represents a stream in Loki
type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// newLokiLoggerProvider creates a logger provider that sends logs directly to Loki
func newLokiLoggerProvider() (*sdklog.LoggerProvider, error) {
	endpoint := "http://loki.observability.svc.cluster.local:3100/loki/api/v1/push"
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "unknown-service"
	}

	log.Printf("Creating Loki HTTP client with endpoint: %s", endpoint)

	lokiClientOnce.Do(func() {
		lokiClient = &LokiHTTPClient{
			endpoint:    endpoint,
			serviceName: serviceName,
			httpClient: &http.Client{
				Timeout: 5 * time.Second,
			},
			logBatch: make([]LokiLogEntry, 0, 512),
			stopCh:   make(chan struct{}),
		}

		// Start background goroutine to flush logs periodically
		go lokiClient.batchFlushLoop()
	})

	log.Println("Loki HTTP client created successfully")

	// Create logger provider with Loki processor
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(NewLokiProcessor(lokiClient)),
	)

	return loggerProvider, nil
}

// AddLog adds a log entry to the batch
func (c *LokiHTTPClient) AddLog(entry LokiLogEntry) {
	c.batchMutex.Lock()
	defer c.batchMutex.Unlock()

	c.logBatch = append(c.logBatch, entry)

	// Flush if batch is full (match OTLP batch size of 512)
	if len(c.logBatch) >= 512 {
		go c.flush()
	}
}

// batchFlushLoop flushes logs every 100ms (matching OTLP export interval)
func (c *LokiHTTPClient) batchFlushLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.flush()
		case <-c.stopCh:
			// Final flush on shutdown
			c.flush()
			return
		}
	}
}

// flush sends the current batch to Loki
func (c *LokiHTTPClient) flush() {
	c.batchMutex.Lock()
	if len(c.logBatch) == 0 {
		c.batchMutex.Unlock()
		return
	}

	// Take ownership of current batch
	batch := c.logBatch
	c.logBatch = make([]LokiLogEntry, 0, 512)
	c.batchMutex.Unlock()

	// Convert to Loki format
	values := make([][]string, len(batch))
	for i, entry := range batch {
		// Loki expects [timestamp_ns, log_line]
		timestampNs := fmt.Sprintf("%d", entry.Timestamp.UnixNano())
		values[i] = []string{timestampNs, entry.Message}
	}

	pushReq := LokiPushRequest{
		Streams: []LokiStream{
			{
				Stream: map[string]string{
					"service": c.serviceName,
					"job":     c.serviceName,
				},
				Values: values,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(pushReq)
	if err != nil {
		log.Printf("ERROR: Failed to marshal Loki push request: %v", err)
		return
	}

	// Send HTTP POST
	req, err := http.NewRequest("POST", c.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("ERROR: Failed to create Loki HTTP request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to send logs to Loki: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("ERROR: Loki returned error status %d", resp.StatusCode)
	}
}

// Shutdown stops the Loki client
func (c *LokiHTTPClient) Shutdown(ctx context.Context) error {
	close(c.stopCh)
	return nil
}

// LokiProcessor implements sdklog.Processor to send logs to Loki
type LokiProcessor struct {
	client *LokiHTTPClient
}

// NewLokiProcessor creates a new Loki processor
func NewLokiProcessor(client *LokiHTTPClient) *LokiProcessor {
	return &LokiProcessor{
		client: client,
	}
}

// OnEmit processes a log record and sends it to Loki
func (p *LokiProcessor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	if p.client == nil {
		return nil
	}

	// Convert OTel log record to Loki entry
	entry := LokiLogEntry{
		Timestamp: record.Timestamp(),
		Message:   record.Body().AsString(),
		Level:     record.SeverityText(),
	}

	// Add to batch
	p.client.AddLog(entry)

	return nil
}

// Enabled returns whether the processor is enabled
func (p *LokiProcessor) Enabled(ctx context.Context, param sdklog.EnabledParameters) bool {
	return p.client != nil
}

// Shutdown shuts down the processor
func (p *LokiProcessor) Shutdown(ctx context.Context) error {
	if p.client != nil {
		return p.client.Shutdown(ctx)
	}
	return nil
}

// ForceFlush is a no-op for this processor
func (p *LokiProcessor) ForceFlush(ctx context.Context) error {
	return nil
}
