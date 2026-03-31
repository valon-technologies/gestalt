package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type exampleRuntime struct{}

func (r *exampleRuntime) Start(ctx context.Context, name string, config map[string]any, capabilities []gestalt.Capability, host gestalt.RuntimeHost) error {
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

	if err := gestalt.ServeRuntime(ctx, &exampleRuntime{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
