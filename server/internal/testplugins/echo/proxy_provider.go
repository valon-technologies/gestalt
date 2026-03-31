package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
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
	ops := make([]core.Operation, len(inner), len(inner)+2)
	copy(ops, inner)
	return append(ops,
		core.Operation{
			Name:   "read_env",
			Method: http.MethodGet,
			Parameters: []core.Parameter{
				{Name: "name", Type: "string", Required: true},
			},
		},
		core.Operation{
			Name:   "proxy_http",
			Method: http.MethodPost,
			Parameters: []core.Parameter{
				{Name: "url", Type: "string", Required: true},
				{Name: "method", Type: "string", Required: true},
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

	case "proxy_http":
		return p.executeProxyHTTP(ctx, params)

	default:
		return p.inner.Execute(ctx, operation, params, token)
	}
}

func (p *proxyProvider) executeProxyHTTP(ctx context.Context, params map[string]any) (*core.OperationResult, error) {
	targetURL, _ := params["url"].(string)
	method, _ := params["method"].(string)

	socket := os.Getenv(proto.EnvPluginHostSocket)
	if socket == "" {
		return nil, fmt.Errorf("%s is required", proto.EnvPluginHostSocket)
	}
	conn, err := pluginhost.DialPluginHost(ctx, socket)
	if err != nil {
		return nil, fmt.Errorf("dial plugin host: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := proto.NewPluginHostClient(conn)
	resp, err := client.ProxyHTTP(ctx, &proto.ProxyHTTPRequest{
		Method: method,
		Url:    targetURL,
	})
	if err != nil {
		body, _ := json.Marshal(map[string]any{"error": err.Error()})
		return &core.OperationResult{Status: http.StatusBadGateway, Body: string(body)}, nil
	}

	body, _ := json.Marshal(map[string]any{
		"status_code": resp.GetStatusCode(),
		"body":        string(resp.GetBody()),
	})
	return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
}

func (p *proxyProvider) Close() error {
	return nil
}
