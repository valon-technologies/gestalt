package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := appservice.ServeProvider(ctx, &coretesting.StubIntegration{N: "fakebackend"}); err != nil {
		log.Fatal(err)
	}
}
