package mcpoauth

import (
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/egress"
)

func newHTTPClient(timeout time.Duration) (*http.Client, func()) {
	transport := egress.CloneDefaultTransport()
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, transport.CloseIdleConnections
}
