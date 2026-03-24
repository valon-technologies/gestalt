package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/internal/graphql"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/internal/openapi"
	"github.com/valon-technologies/gestalt/internal/provider"
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
	logDatastoreWarnings(result.Datastore)

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

func logDatastoreWarnings(ds core.Datastore) {
	type warner interface {
		Warnings() []string
	}
	if w, ok := ds.(warner); ok {
		for _, msg := range w.Warnings() {
			log.Printf("WARNING: %s", msg)
		}
	}
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
				if stopErr := bootstrap.StopRuntimes(env.Ctx, result.Runtimes, started); stopErr != nil {
					log.Printf("stopping runtimes after startup failure: %v", stopErr)
				}
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
				if closeErr := bootstrap.CloseBindings(result.Bindings, started); closeErr != nil {
					log.Printf("closing bindings after startup failure: %v", closeErr)
				}
				if result.Runtimes != nil {
					if stopErr := bootstrap.StopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List()); stopErr != nil {
						log.Printf("stopping runtimes after startup failure: %v", stopErr)
					}
				}
				return fmt.Errorf("getting binding %q: %v", name, err)
			}
			if err := binding.Start(env.Ctx); err != nil {
				if closeErr := bootstrap.CloseBindings(result.Bindings, started); closeErr != nil {
					log.Printf("closing bindings after startup failure: %v", closeErr)
				}
				if result.Runtimes != nil {
					if stopErr := bootstrap.StopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List()); stopErr != nil {
						log.Printf("stopping runtimes after startup failure: %v", stopErr)
					}
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
		if err := bootstrap.CloseBindings(env.Result.Bindings, env.Result.Bindings.List()); err != nil {
			log.Printf("closing bindings: %v", err)
		}
	}
	if env.Result.Runtimes != nil {
		if err := bootstrap.StopRuntimes(ctx, env.Result.Runtimes, env.Result.Runtimes.List()); err != nil {
			log.Printf("stopping runtimes: %v", err)
		}
	}
	if err := bootstrap.CloseProviders(env.Result.Providers); err != nil {
		log.Printf("closing providers: %v", err)
	}
}

func defaultProviderFactory(providerDirs []string) bootstrap.ProviderFactory {
	return func(ctx context.Context, name string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		var apiProv core.Provider
		var mcpUp *mcpupstream.Upstream

		connMode := core.ConnectionModeUser
		switch core.ConnectionMode(intg.ConnectionMode) {
		case "", core.ConnectionModeNone, core.ConnectionModeUser, core.ConnectionModeIdentity, core.ConnectionModeEither:
			if intg.ConnectionMode != "" {
				connMode = core.ConnectionMode(intg.ConnectionMode)
			}
		default:
			return nil, fmt.Errorf("unknown connection_mode %q", intg.ConnectionMode)
		}

		cleanup := func() {
			if mcpUp != nil {
				_ = mcpUp.Close()
			}
		}

		for _, us := range intg.Upstreams {
			switch us.Type {
			case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
				if apiProv != nil {
					cleanup()
					return nil, fmt.Errorf("multiple api upstreams not supported")
				}
				def, err := loadAPIUpstream(ctx, name, us, providerDirs)
				if err != nil {
					cleanup()
					return nil, err
				}
				p, err := provider.Build(def, intg, map[string]string(us.AllowedOperations))
				if err != nil {
					cleanup()
					return nil, err
				}
				apiProv = p
			case config.UpstreamTypeMCP:
				if mcpUp != nil {
					cleanup()
					return nil, fmt.Errorf("multiple mcp upstreams not supported")
				}
				up, err := mcpupstream.New(ctx, name, us.URL, connMode)
				switch {
				case errors.Is(err, transport.ErrUnauthorized):
					log.Printf("INFO: deferring MCP upstream %q: %v", name, err)
					up = mcpupstream.NewDeferred(name, us.URL, connMode)
					if us.AllowedOperations != nil {
						up.SetAllowedOperations(map[string]string(us.AllowedOperations))
					}
				case err != nil:
					return nil, err
				case us.AllowedOperations != nil:
					if err := up.FilterOperations(map[string]string(us.AllowedOperations)); err != nil {
						_ = up.Close()
						return nil, err
					}
				}
				mcpUp = up
			default:
				cleanup()
				return nil, fmt.Errorf("unknown upstream type %q", us.Type)
			}
		}

		switch {
		case apiProv != nil && mcpUp != nil:
			return composite.New(name, apiProv, mcpUp), nil
		case apiProv != nil:
			return apiProv, nil
		case mcpUp != nil:
			return mcpUp, nil
		default:
			return nil, fmt.Errorf("no upstreams configured")
		}
	}
}

func loadAPIUpstream(ctx context.Context, name string, us config.UpstreamDef, providerDirs []string) (*provider.Definition, error) {
	if us.Provider != "" {
		return provider.LoadFile(us.Provider)
	}

	switch us.Type {
	case config.UpstreamTypeREST:
		if us.URL != "" {
			return openapi.LoadDefinition(ctx, name, us.URL, map[string]string(us.AllowedOperations))
		}
	case config.UpstreamTypeGraphQL:
		if us.URL != "" {
			return graphqlupstream.LoadDefinition(ctx, name, us.URL, map[string]string(us.AllowedOperations))
		}
	default:
		return nil, fmt.Errorf("unsupported api upstream type %q", us.Type)
	}

	return provider.LoadFromDir(name, providerDirs)
}
