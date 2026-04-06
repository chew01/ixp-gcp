package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

// newKafkaTLSConfig reads KAFKA_TLS_CA_FILE, KAFKA_TLS_CERT_FILE and
// KAFKA_TLS_KEY_FILE from the environment and returns a *tls.Config for
// mutual-TLS connections to a managed Kafka cluster (e.g. Aiven).
// Returns nil when none of the variables are set — plaintext mode.
func newKafkaTLSConfig() (*tls.Config, error) {
	caFile := os.Getenv("KAFKA_TLS_CA_FILE")
	certFile := os.Getenv("KAFKA_TLS_CERT_FILE")
	keyFile := os.Getenv("KAFKA_TLS_KEY_FILE")

	if caFile == "" && certFile == "" && keyFile == "" {
		return nil, nil
	}
	if caFile == "" || certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("KAFKA_TLS_CA_FILE, KAFKA_TLS_CERT_FILE and KAFKA_TLS_KEY_FILE must all be set together")
	}

	keypair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS key pair: %w", err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", caFile)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{keypair},
		RootCAs:      pool,
	}, nil
}

// kafkaDialer wraps tlsCfg in a *kafka.Dialer for use with kafka.ReaderConfig.
// Returns nil when tlsCfg is nil so kafka-go falls back to its default dialer.
func kafkaDialer(tlsCfg *tls.Config) *kafka.Dialer {
	if tlsCfg == nil {
		return nil
	}
	return &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		TLS:       tlsCfg,
	}
}

// kafkaTransport wraps tlsCfg in a kafka.RoundTripper for use with kafka.Writer.
// Returns nil when tlsCfg is nil so kafka-go falls back to its default transport.
func kafkaTransport(tlsCfg *tls.Config) kafka.RoundTripper {
	if tlsCfg == nil {
		return nil
	}
	return &kafka.Transport{TLS: tlsCfg}
}
