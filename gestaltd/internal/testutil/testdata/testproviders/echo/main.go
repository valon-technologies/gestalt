package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

func main() {
	if len(os.Args) < 2 {
		slog.Error("usage", "command", "gestalt-plugin-echo provider")
		os.Exit(2)
	}
	if err := run(); err != nil {
		slog.Error("echo plugin failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "provider":
		return pluginservice.ServeProvider(ctx, newProxyProvider(&echoProvider{}))
	default:
		return fmt.Errorf("unknown mode %q", os.Args[1])
	}
}

var _ core.Provider = (*echoProvider)(nil)

type echoProvider struct{}

func (p *echoProvider) Name() string                        { return "echo" }
func (p *echoProvider) DisplayName() string                 { return "Echo" }
func (p *echoProvider) Description() string                 { return "Echoes back the input parameters" }
func (p *echoProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeNone }
func (p *echoProvider) AuthTypes() []string                 { return nil }
func (p *echoProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *echoProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *echoProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *echoProvider) ConnectionForOperation(string) string        { return "" }
func (p *echoProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        p.Name(),
		DisplayName: p.DisplayName(),
		Description: p.Description(),
		Operations: []catalog.CatalogOperation{
			{
				ID:          "echo",
				Description: "Echo back input params as JSON",
				Method:      http.MethodPost,
				Transport:   catalog.TransportPlugin,
			},
		},
	}
}

func (p *echoProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	if operation != "echo" {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
}
