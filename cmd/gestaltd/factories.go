package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/internal/openapi"
	"github.com/valon-technologies/gestalt/internal/provider"
	"github.com/valon-technologies/gestalt/internal/registry"
	"github.com/valon-technologies/gestalt/plugins/auth/google"
	"github.com/valon-technologies/gestalt/plugins/auth/oidc"
	"github.com/valon-technologies/gestalt/plugins/bindings/webhook"
	dynamodbstore "github.com/valon-technologies/gestalt/plugins/datastore/dynamodb"
	"github.com/valon-technologies/gestalt/plugins/datastore/firestore"
	"github.com/valon-technologies/gestalt/plugins/datastore/mongodb"
	"github.com/valon-technologies/gestalt/plugins/datastore/mysql"
	"github.com/valon-technologies/gestalt/plugins/datastore/oracle"
	"github.com/valon-technologies/gestalt/plugins/datastore/postgres"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlite"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlserver"
	"github.com/valon-technologies/gestalt/plugins/providers/echo"
	echoruntime "github.com/valon-technologies/gestalt/plugins/runtimes/echo"
	secretsenv "github.com/valon-technologies/gestalt/plugins/secrets/env"
	secretsfile "github.com/valon-technologies/gestalt/plugins/secrets/file"
	secretsgcp "github.com/valon-technologies/gestalt/plugins/secrets/gcp"
)

type bootstrapEnv struct {
	Ctx    context.Context
	Stop   context.CancelFunc
	Config *config.Config
	Result *bootstrap.Result
}

func setupBootstrap(configFlag string) (*bootstrapEnv, error) {
	path := resolveConfigPath(configFlag)

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	factories := buildFactories(cfg.ProviderDirs, cfg.Server.DevMode)

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

func buildFactories(providerDirs []string, devMode bool) *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Auth["oidc"] = oidc.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Datastores["postgres"] = postgres.Factory
	factories.Datastores["mysql"] = mysql.Factory
	factories.Datastores["dynamodb"] = dynamodbstore.Factory
	factories.Datastores["mongodb"] = mongodb.Factory
	factories.Datastores["oracle"] = oracle.Factory
	factories.Datastores["firestore"] = firestore.Factory
	factories.Datastores["sqlserver"] = sqlserver.Factory
	factories.DefaultProvider = defaultProviderFactory(providerDirs)
	if devMode {
		factories.Builtins = append(factories.Builtins, echo.New())
		factories.Runtimes["echo"] = echoruntime.Factory
	}
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
	if envPath := os.Getenv("GESTALT_CONFIG"); envPath != "" {
		return envPath
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	return "/etc/gestalt/config.yaml"
}

const gracefulShutdownTimeout = 15 * time.Second

func startPlugins(env *bootstrapEnv) error {
	result := env.Result
	if result.Runtimes != nil {
		var started []string
		for _, name := range result.Runtimes.List() {
			rt, err := result.Runtimes.Get(name)
			if err != nil {
				return fmt.Errorf("getting runtime %q: %v", name, err)
			}
			if err := rt.Start(env.Ctx); err != nil {
				stopRuntimes(env.Ctx, result.Runtimes, started)
				return fmt.Errorf("starting runtime %q: %v", name, err)
			}
			started = append(started, name)
		}
	}
	if result.Bindings != nil {
		var started []string
		for _, name := range result.Bindings.List() {
			binding, err := result.Bindings.Get(name)
			if err != nil {
				closeBindings(result.Bindings, started)
				if result.Runtimes != nil {
					stopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List())
				}
				return fmt.Errorf("getting binding %q: %v", name, err)
			}
			if err := binding.Start(env.Ctx); err != nil {
				closeBindings(result.Bindings, started)
				if result.Runtimes != nil {
					stopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List())
				}
				return fmt.Errorf("starting binding %q: %v", name, err)
			}
			started = append(started, name)
		}
	}
	return nil
}

func shutdownPlugins(ctx context.Context, env *bootstrapEnv) {
	if env.Result.Bindings != nil {
		closeBindings(env.Result.Bindings, env.Result.Bindings.List())
	}
	if env.Result.Runtimes != nil {
		stopRuntimes(ctx, env.Result.Runtimes, env.Result.Runtimes.List())
	}
	closeProviders(env.Result.Providers)
}

func closeProviders(providers *registry.PluginMap[core.Provider]) {
	if providers == nil {
		return
	}
	for _, name := range providers.List() {
		prov, err := providers.Get(name)
		if err != nil {
			continue
		}
		if c, ok := prov.(io.Closer); ok {
			if err := c.Close(); err != nil {
				log.Printf("closing provider %q: %v", name, err)
			}
		}
	}
}

func defaultProviderFactory(providerDirs []string) bootstrap.ProviderFactory {
	return func(ctx context.Context, name string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		if intg.MCP != nil {
			return mcpupstream.New(ctx, name, intg)
		}
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
