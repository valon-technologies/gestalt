package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	bigquery "github.com/valon-technologies/gestalt/plugins/bigquery"
	"github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return gestalt.ServeProvider(ctx, bigquery.NewProvider())
}
