package upstream

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Client wraps an optimized http.Client for low latency proxying
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
}

// NewClient creates an optimized HTTP client with connection pooling
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   6 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   6 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}
}
