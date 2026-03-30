package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/internal/pluginapi"
)

const functionalTestScenarioEnv = "GESTALT_BIGQUERY_SCENARIO"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	provider, err := providerForRun()
	if err != nil {
		return err
	}
	return pluginapi.ServeProvider(ctx, provider)
}
