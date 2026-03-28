package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pluginsdk.ServeProvider(ctx, &slackProvider{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
