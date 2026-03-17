package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/server"
	"github.com/valon-technologies/toolshed/plugins/auth/google"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlite"
	"github.com/valon-technologies/toolshed/plugins/integration/slack"
)

func main() {
	configPath := flag.String("config", "toolshed.yaml", "path to config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Integrations["slack"] = slack.Factory

	result, err := bootstrap.Bootstrap(cfg, factories)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	defer func() { _ = result.Datastore.Close() }()

	ctx := context.Background()
	if err := result.Datastore.Migrate(ctx); err != nil {
		return fmt.Errorf("database migration: %w", err)
	}

	srv := server.New(server.Config{
		Auth:         result.Auth,
		Datastore:    result.Datastore,
		Integrations: result.Integrations,
		DevMode:      result.DevMode,
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpServer := &http.Server{Addr: addr, Handler: srv}

	shutdown := make(chan struct{})
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Println("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		close(shutdown)
	}()

	log.Printf("listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	<-shutdown
	return nil
}
