package core

import (
	"context"
	"net/http"
)

type Binding interface {
	Name() string
	Start(ctx context.Context) error
	Routes() []Route
	Close() error
}

type Route struct {
	Method    string
	Pattern   string
	Handler   http.Handler
	Public    bool
	ProxyAuth bool // use proxy auth semantics (Proxy-Authorization, 407 responses)
	Connect   bool // handle CONNECT method (dispatched outside path-based routing)
}
