package network

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// LoadServerTLS loads certificate/key pair for REST or WebSocket servers.
func LoadServerTLS(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("tls cert and key required")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls cert: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// LoadPeerTLS returns TLS config for outbound wss:// peer connections.
// When caFile is empty, uses system roots with InsecureSkipVerify disabled.
func LoadPeerTLS(caFile string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if caFile == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read peer tls ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("invalid peer tls ca pem")
	}
	cfg.RootCAs = pool
	return cfg, nil
}

// TLSEnabled reports whether cert/key env or flags are set.
func TLSEnabled(certFile, keyFile string) bool {
	return certFile != "" && keyFile != ""
}
