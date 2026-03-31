package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type exampleRuntime struct{}

func (r *exampleRuntime) Start(ctx context.Context, name string, config map[string]any, capabilities []gestalt.Capability, host gestalt.RuntimeHost) error {
	slog.Info("example runtime started", "runtime", name, "capability_count", len(capabilities))
	return nil
}

func (r *exampleRuntime) Stop(ctx context.Context) error {
	slog.Info("example runtime stopped")
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := gestalt.ServeRuntime(ctx, &exampleRuntime{}); err != nil {
		slog.Error("example runtime failed", "error", err)
		os.Exit(1)
	}
}
