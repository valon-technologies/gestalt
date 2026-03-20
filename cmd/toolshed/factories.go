package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/openapi"
	"github.com/valon-technologies/toolshed/internal/provider"
	"github.com/valon-technologies/toolshed/plugins/auth/google"
	"github.com/valon-technologies/toolshed/plugins/auth/oidc"
	"github.com/valon-technologies/toolshed/plugins/bindings/webhook"
	"github.com/valon-technologies/toolshed/plugins/datastore/mongodb"
	"github.com/valon-technologies/toolshed/plugins/datastore/mysql"
	"github.com/valon-technologies/toolshed/plugins/datastore/postgres"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlite"
	"github.com/valon-technologies/toolshed/plugins/providers/echo"
	echoruntime "github.com/valon-technologies/toolshed/plugins/runtimes/echo"
	secretsenv "github.com/valon-technologies/toolshed/plugins/secrets/env"
	secretsfile "github.com/valon-technologies/toolshed/plugins/secrets/file"
	secretsgcp "github.com/valon-technologies/toolshed/plugins/secrets/gcp"
)

type bootstrapEnv struct {
	Ctx    context.Context
	Stop   context.CancelFunc
	Config *config.Config
	Result *bootstrap.Result
}

func setupBootstrap(cmdName string, args []string) (*bootstrapEnv, error) {
	fs := flag.NewFlagSet(cmdName, flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	path := resolveConfigPath(*configPath)

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	factories := buildFactories(cfg.ProviderDirs)

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		stop()
		return nil, fmt.Errorf("bootstrap: %v", err)
	}

	if err := result.Datastore.Migrate(ctx); err != nil {
		_ = result.Datastore.Close()
		if closer, ok := result.SecretManager.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		stop()
		return nil, fmt.Errorf("running datastore migrations: %v", err)
	}

	return &bootstrapEnv{
		Ctx:    ctx,
		Stop:   stop,
		Config: cfg,
		Result: result,
	}, nil
}

func (e *bootstrapEnv) Close() {
	e.Stop()
	_ = e.Result.Datastore.Close()
	if closer, ok := e.Result.SecretManager.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func buildFactories(providerDirs []string) *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Auth["oidc"] = oidc.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Datastores["postgres"] = postgres.Factory
	factories.Datastores["mysql"] = mysql.Factory
	factories.Datastores["mongodb"] = mongodb.Factory
	factories.DefaultProvider = defaultProviderFactory(providerDirs)
	factories.Builtins = append(factories.Builtins, echo.New())
	factories.Runtimes["echo"] = echoruntime.Factory
	factories.Bindings["webhook"] = webhook.Factory
	factories.Secrets["env"] = secretsenv.Factory
	factories.Secrets["file"] = secretsfile.Factory
	factories.Secrets["gcp_secret_manager"] = secretsgcp.Factory
	return factories
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
