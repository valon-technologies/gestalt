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

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/openapi"
	"github.com/valon-technologies/toolshed/internal/provider"
	"github.com/valon-technologies/toolshed/internal/server"
	"github.com/valon-technologies/toolshed/plugins/auth/google"
	"github.com/valon-technologies/toolshed/plugins/auth/oidc"
	"github.com/valon-technologies/toolshed/plugins/datastore/mysql"
	"github.com/valon-technologies/toolshed/plugins/datastore/postgres"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlite"

	_ "github.com/valon-technologies/toolshed/plugins/integrations/datadog"
	_ "github.com/valon-technologies/toolshed/plugins/integrations/hex"
	_ "github.com/valon-technologies/toolshed/plugins/integrations/slack"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	path := resolveConfigPath(*configPath)

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Auth["oidc"] = oidc.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Datastores["postgres"] = postgres.Factory
	factories.Datastores["mysql"] = mysql.Factory
	factories.DefaultProvider = defaultProviderFactory(cfg.ProviderDirs)

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		return fmt.Errorf("bootstrap: %v", err)
	}
	defer func() { _ = result.Datastore.Close() }()

	if err := result.Datastore.Migrate(ctx); err != nil {
		return fmt.Errorf("running datastore migrations: %v", err)
	}

	srv := server.New(server.Config{
		Auth:      result.Auth,
		Datastore: result.Datastore,
		Providers: result.Providers,
		DevMode:   result.DevMode,
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.Server.BaseURL != "" {
		log.Printf("toolshed base URL: %s", cfg.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", cfg.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", cfg.Server.BaseURL, config.IntegrationCallbackPath)
	}

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("toolshed listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			listenErr <- err
		}
	}()

	select {
	case err := <-listenErr:
		return fmt.Errorf("http server: %v", err)
	case <-ctx.Done():
	}
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %v", err)
	}
	log.Println("shutdown complete")
	return nil
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envPath := os.Getenv("TOOLSHED_CONFIG"); envPath != "" {
		return envPath
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	return "/etc/toolshed/config.yaml"
}

func defaultProviderFactory(providerDirs []string) bootstrap.ProviderFactory {
	return func(ctx context.Context, name string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		def, err := loadDefinition(ctx, name, intg, providerDirs)
		if err != nil {
			return nil, err
		}
		return provider.Build(def, intg)
	}
}

func loadDefinition(ctx context.Context, name string, intgDef config.IntegrationDef, providerDirs []string) (*provider.Definition, error) {
	switch {
	case intgDef.OpenAPI != "":
		return openapi.LoadDefinition(ctx, name, intgDef.OpenAPI, intgDef.AllowedOperations)

	case intgDef.Provider != "":
		return provider.LoadFile(intgDef.Provider)

	default:
		return provider.LoadFromDir(name, providerDirs)
	}
}
