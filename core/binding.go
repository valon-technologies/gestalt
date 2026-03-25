package core

import (
	"context"
	"net/http"
)

// Deprecated: BindingKind is no longer used by the core Binding contract.
type BindingKind int

const (
	BindingTrigger BindingKind = 1 << iota
	BindingSurface
)

type Binding interface {
	Name() string
	Start(ctx context.Context) error
	Routes() []Route
	Close() error
}

type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
	Public  bool
}
