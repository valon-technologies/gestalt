package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	slack "github.com/valon-technologies/gestalt/plugins/slack"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	if err := run(); err != nil {
		slog.Error("slack provider failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return gestalt.ServeProvider(ctx, slack.NewProvider())
}
