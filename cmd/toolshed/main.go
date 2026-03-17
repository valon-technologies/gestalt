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

	"github.com/valon-technologies/toolshed/core/crypto"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/registry"
	"github.com/valon-technologies/toolshed/internal/server"
	"github.com/valon-technologies/toolshed/plugins/auth/google"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlite"
	"github.com/valon-technologies/toolshed/plugins/integration/slack"
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

	datastore, err := initDatastore(cfg)
	if err != nil {
		return fmt.Errorf("initializing datastore: %v", err)
	}
	defer func() { _ = datastore.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := datastore.Migrate(ctx); err != nil {
		return fmt.Errorf("running datastore migrations: %v", err)
	}

	auth, err := initAuth(cfg)
	if err != nil {
		return fmt.Errorf("initializing auth provider: %v", err)
	}

	reg := registry.New()
	if err := initIntegrations(cfg, reg); err != nil {
		return fmt.Errorf("initializing integrations: %v", err)
	}

	srv := server.New(server.Config{
		Auth:         auth,
		Datastore:    datastore,
		Integrations: &reg.Integrations,
		DevMode:      cfg.Server.DevMode,
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
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

func initDatastore(cfg *config.Config) (*sqlite.Store, error) {
	switch cfg.Datastore.Provider {
	case "sqlite":
		var dsCfg struct {
			Path string `yaml:"path"`
		}
		if err := cfg.Datastore.Config.Decode(&dsCfg); err != nil {
			return nil, fmt.Errorf("decoding sqlite config: %w", err)
		}
		if dsCfg.Path == "" {
			dsCfg.Path = "toolshed.db"
		}

		encKey := crypto.DeriveKey(cfg.Server.EncryptionKey)

		return sqlite.New(dsCfg.Path, encKey)
	default:
		return nil, fmt.Errorf("unknown datastore provider: %q", cfg.Datastore.Provider)
	}
}

func initAuth(cfg *config.Config) (*google.Provider, error) {
	switch cfg.Auth.Provider {
	case "google":
		var authCfg struct {
			ClientID       string   `yaml:"client_id"`
			ClientSecret   string   `yaml:"client_secret"`
			RedirectURL    string   `yaml:"redirect_url"`
			AllowedDomains []string `yaml:"allowed_domains"`
			SessionSecret  string   `yaml:"session_secret"`
		}
		if err := cfg.Auth.Config.Decode(&authCfg); err != nil {
			return nil, fmt.Errorf("decoding google auth config: %w", err)
		}

		return google.New(google.Config{
			ClientID:       authCfg.ClientID,
			ClientSecret:   authCfg.ClientSecret,
			RedirectURL:    authCfg.RedirectURL,
			AllowedDomains: authCfg.AllowedDomains,
			SessionSecret:  []byte(authCfg.SessionSecret),
		})
	default:
		return nil, fmt.Errorf("unknown auth provider: %q", cfg.Auth.Provider)
	}
}

func initIntegrations(cfg *config.Config, reg *registry.Registry) error {
	for _, name := range cfg.Integrations {
		switch name {
		case "slack":
			slackCfg, err := decodeIntegrationConfig[slack.Config](cfg, name)
			if err != nil {
				return err
			}
			integration := slack.New(slackCfg)
			if err := reg.Integrations.Register(name, integration); err != nil {
				return fmt.Errorf("registering integration %q: %w", name, err)
			}
		default:
			return fmt.Errorf("unknown integration: %q", name)
		}
	}
	return nil
}

func decodeIntegrationConfig[T any](cfg *config.Config, name string) (T, error) {
	var result T
	node, ok := cfg.IntegrationConfig[name]
	if !ok {
		return result, nil
	}
	if err := node.Decode(&result); err != nil {
		return result, fmt.Errorf("decoding %s integration config: %w", name, err)
	}
	return result, nil
}
