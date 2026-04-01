package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	gmail "github.com/valon-technologies/gestalt/plugins/gmail"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gmail provider failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return gestalt.ServeProvider(ctx, gmail.NewProvider())
}
