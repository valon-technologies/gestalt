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
)

func main() {
	if len(os.Args) < 2 {
		slog.Error("usage", "command", "gestalt-app-echo provider")
		os.Exit(2)
	}
	if err := run(); err != nil {
		slog.Error("echo app failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "provider":
		return gestalt.ServeProvider(ctx, newProxyProvider(&echoProvider{}), proxyRouter)
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

func (p *echoProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*gestalt.OperationResult, error) {
	if operation != "echo" {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil
}
