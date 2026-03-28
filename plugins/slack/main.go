package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

const httpTimeout = 30 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	provider := &slackProvider{
		httpClient: &http.Client{Timeout: httpTimeout},
	}
	if err := pluginsdk.ServeProvider(ctx, provider); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
