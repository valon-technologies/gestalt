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

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
		return serveProxyProvider(ctx, newProxyProvider(&echoProvider{}))
	default:
		return fmt.Errorf("unknown mode %q", os.Args[1])
	}
}

var _ executableProvider = (*echoProvider)(nil)

type echoProvider struct{}

func (p *echoProvider) Configure(context.Context, string, map[string]any) error { return nil }
func (p *echoProvider) Name() string                                            { return "echo" }
func (p *echoProvider) DisplayName() string                                     { return "Echo" }
func (p *echoProvider) Description() string                                     { return "Echoes back the input parameters" }
func (p *echoProvider) ConnectionMode() proto.ConnectionMode {
	return proto.ConnectionMode_CONNECTION_MODE_NONE
}
func (p *echoProvider) AuthTypes() []string { return nil }
func (p *echoProvider) Catalog() *proto.Catalog {
	return &proto.Catalog{
		Name:        p.Name(),
		DisplayName: p.DisplayName(),
		Description: p.Description(),
		Operations: []*proto.CatalogOperation{
			{
				Id:          "echo",
				Description: "Echo back input params as JSON",
				Method:      http.MethodPost,
				Transport:   transportPlugin,
			},
		},
	}
}

func (p *echoProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*gestalt.OperationResult, error) {
	if operation != "echo" {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
}
