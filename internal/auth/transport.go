package auth

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// BuildTLSConfig loads a client certificate and key and returns a tls.Config.
func BuildTLSConfig(certPath, keyPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load key pair: %w", err)
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
	}, nil
}

// BuildMTLSTransport creates an http.Transport configured for mutual TLS.
func BuildMTLSTransport(certPath, keyPath string) (*http.Transport, error) {
	tlsCfg, err := BuildTLSConfig(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &http.Transport{
		TLSClientConfig: tlsCfg,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
	}, nil
}
