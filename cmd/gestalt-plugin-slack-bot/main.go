package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/internal/pluginapi"
	"github.com/valon-technologies/gestalt/plugins/providers/slack"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return pluginapi.ServeProvider(ctx, slack.NewOverlayProvider())
}
