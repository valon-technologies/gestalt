package mcpoauth

import (
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

func newHTTPClient(timeout time.Duration) (*http.Client, func()) {
	transport := egress.CloneDefaultTransport()
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, transport.CloseIdleConnections
}
