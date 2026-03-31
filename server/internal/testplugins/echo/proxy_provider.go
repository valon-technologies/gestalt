package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"google.golang.org/grpc"
)

type proxyProvider struct {
	inner    core.Provider
	hostMu   sync.Mutex
	hostConn *grpc.ClientConn
	host     pluginapiv1.ProviderHostClient
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
			Name:   "proxy_fetch",
			Method: http.MethodGet,
			Parameters: []core.Parameter{
				{Name: "url", Type: "string", Required: true},
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

	case "proxy_fetch":
		if err := p.ensureHost(ctx); err != nil {
			return nil, err
		}
		url, _ := params["url"].(string)
		invocationID := pluginsdk.InvocationID(ctx)
		resp, err := p.host.ProxyHTTP(ctx, &pluginapiv1.ProxyHTTPRequest{
			InvocationId: invocationID,
			Method:       http.MethodGet,
			Url:          url,
		})
		if err != nil {
			return nil, fmt.Errorf("proxy_fetch: %w", err)
		}
		body, _ := json.Marshal(map[string]any{
			"status_code": resp.GetStatusCode(),
			"headers":     resp.GetHeaders(),
			"body":        string(resp.GetBody()),
		})
		return &core.OperationResult{Status: int(resp.GetStatusCode()), Body: string(body)}, nil

	default:
		return p.inner.Execute(ctx, operation, params, token)
	}
}

func (p *proxyProvider) ensureHost(ctx context.Context) error {
	p.hostMu.Lock()
	defer p.hostMu.Unlock()
	if p.host != nil {
		return nil
	}
	conn, host, err := pluginhost.DialProviderHost(ctx)
	if err != nil {
		return err
	}
	p.hostConn = conn
	p.host = host
	return nil
}

func (p *proxyProvider) Close() error {
	if p.hostConn != nil {
		return p.hostConn.Close()
	}
	return nil
}
