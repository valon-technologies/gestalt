package mcpoauth

import (
	"net/http"
	"time"
)

func newHTTPClient(timeout time.Duration) (*http.Client, func()) {
	transport := cloneDefaultTransport()
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, transport.CloseIdleConnections
}

func cloneDefaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}
