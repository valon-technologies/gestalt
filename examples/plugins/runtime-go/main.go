package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

type exampleRuntime struct{}

func (r *exampleRuntime) Start(ctx context.Context, name string, config map[string]any, capabilities []pluginsdk.Capability, host pluginsdk.RuntimeHost) error {
	log.Printf("example runtime %q started with %d initial capabilities", name, len(capabilities))
	return nil
}

func (r *exampleRuntime) Stop(ctx context.Context) error {
	log.Println("example runtime stopped")
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pluginsdk.ServeRuntime(ctx, &exampleRuntime{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
