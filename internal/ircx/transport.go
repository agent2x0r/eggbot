package ircx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"eggbot/internal/config"
)

type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

func defaultDialer(cfg *config.Config) DialFunc {
	timeout := time.Duration(cfg.Server.DialTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	d := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		raw, err := d.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if !cfg.Server.TLS {
			return raw, nil
		}
		tlsCfg, err := tlsConfig(cfg)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		c := tls.Client(raw, tlsCfg)
		if err := c.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return c, nil
	}
}

func tlsConfig(cfg *config.Config) (*tls.Config, error) {
	tc := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.Server.InsecureSkipVerify, //nolint:gosec
		ServerName:         cfg.Server.TLSServerName,
	}
	if tc.ServerName == "" {
		host := cfg.Server.Host
		if h, _, err := net.SplitHostPort(cfg.ServerAddr()); err == nil {
			host = h
		}
		tc.ServerName = host
	}
	if cfg.Server.CAFile != "" {
		pem, err := os.ReadFile(cfg.Server.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca file contained no certificates")
		}
		tc.RootCAs = pool
	}
	if cfg.Server.ClientCert != "" && cfg.Server.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Server.ClientCert, cfg.Server.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		tc.Certificates = []tls.Certificate{cert}
	}
	return tc, nil
}
