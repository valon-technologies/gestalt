package core

import (
	"context"
	"net/http"
)

type BindingKind int

const (
	BindingTrigger BindingKind = 1 << iota
	BindingSurface
)

type Binding interface {
	Name() string
	Kind() BindingKind
	Start(ctx context.Context) error
	Routes() []Route
	Close() error
}

type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}
