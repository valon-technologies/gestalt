package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	providerpkg "github.com/valon-technologies/gestalt/testdata/provider-go-indexeddb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeIndexedDBProvider(ctx, providerpkg.New()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
