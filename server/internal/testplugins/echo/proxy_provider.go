package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/valon-technologies/gestalt/server/core"
)

type proxyProvider struct {
	inner core.Provider
}

func newProxyProvider(inner core.Provider) *proxyProvider {
	return &proxyProvider{inner: inner}
}

func (p *proxyProvider) Name() string                        { return p.inner.Name() }
func (p *proxyProvider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *proxyProvider) Description() string                 { return p.inner.Description() }
func (p *proxyProvider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }

func (p *proxyProvider) ListOperations() []core.Operation {
	inner := p.inner.ListOperations()
	ops := make([]core.Operation, len(inner), len(inner)+1)
	copy(ops, inner)
	return append(ops,
		core.Operation{
			Name:   "read_env",
			Method: http.MethodGet,
			Parameters: []core.Parameter{
				{Name: "name", Type: "string", Required: true},
			},
		},
	)
}

func (p *proxyProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case "read_env":
		name, _ := params["name"].(string)
		val, ok := os.LookupEnv(name)
		body, _ := json.Marshal(map[string]any{"name": name, "value": val, "found": ok})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	default:
		return p.inner.Execute(ctx, operation, params, token)
	}
}

func (p *proxyProvider) Close() error {
	return nil
}
