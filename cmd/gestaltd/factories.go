package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"database/sql"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/provider"
	providercompiler "github.com/valon-technologies/gestalt/internal/provider/compiler"
	"github.com/valon-technologies/gestalt/plugins/auth/google"
	"github.com/valon-technologies/gestalt/plugins/auth/local"
	"github.com/valon-technologies/gestalt/plugins/auth/oidc"
	"github.com/valon-technologies/gestalt/plugins/bindings/proxy"
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

func setupBootstrap(configFlag string, locked bool) (*bootstrapEnv, error) {
	_, cfg, preparedProviders, err := loadConfigForExecution(configFlag, locked)
	if err != nil {
		return nil, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	factories := buildFactories(preparedProviders, cfg.Server.DevMode)

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
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	_ = e.Result.Close(ctx)
}

func buildFactories(preparedProviders map[string]string, devMode bool) *bootstrap.FactoryRegistry {
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["google"] = google.Factory
	factories.Auth["local"] = local.Factory
	factories.Auth["oidc"] = oidc.Factory
	factories.Datastores["sqlite"] = sqlite.Factory
	factories.Datastores["postgres"] = postgres.Factory
	factories.Datastores["mysql"] = mysql.Factory
	factories.Datastores["dynamodb"] = dynamodbstore.Factory
	factories.Datastores["mongodb"] = mongodb.Factory
	factories.Datastores["oracle"] = oracle.Factory
	factories.Datastores["firestore"] = firestore.Factory
	factories.Datastores["sqlserver"] = sqlserver.Factory
	registerProviders(factories)
	factories.DefaultProvider = defaultProviderFactory(preparedProviders)
	if devMode {
		factories.Builtins = append(factories.Builtins, echo.New())
		factories.Runtimes["echo"] = echoruntime.Factory
	}
	factories.Bindings["webhook"] = webhook.Factory
	factories.Bindings["proxy"] = proxy.Factory
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
	if p := defaultLocalConfigPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
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

func defaultProviderFactory(preparedProviders map[string]string) bootstrap.ProviderFactory {
	var regStoreOnce sync.Once
	var regStore mcpoauth.RegistrationStore

	return func(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (core.Provider, error) {
		regStoreOnce.Do(func() { regStore = buildRegistrationStore(deps) })
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

		var mcpUpDef *config.UpstreamDef

		for i := range intg.Upstreams {
			us := &intg.Upstreams[i]
			switch us.Type {
			case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
				if apiProv != nil {
					cleanup()
					return nil, fmt.Errorf("multiple api upstreams not supported")
				}
				var buildOpts []provider.BuildOption
				buildOpts = append(buildOpts, provider.WithEgressResolver(deps.Egress.Resolver))
				if us.Auth.Type == "mcp_oauth" {
					handler := buildMCPOAuthHandlerFromUpstream(*us, regStore, deps)
					buildOpts = append(buildOpts, provider.WithAuthHandler(handler))
				}
				p, err := providercompiler.BuildProvider(ctx, name, intg, *us, preparedProviders, buildOpts...)
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
				up, err := mcpupstream.New(ctx, name, us.URL, connMode, deps.Egress.Resolver)
				if err != nil {
					return nil, err
				}
				if us.AllowedOperations != nil {
					if err := up.FilterOperations(map[string]string(us.AllowedOperations)); err != nil {
						_ = up.Close()
						return nil, err
					}
				}
				mcpUp = up
				mcpUpDef = us
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
			if mcpUpDef != nil && mcpUpDef.Auth.Type == "mcp_oauth" {
				return buildMCPOAuthProvider(name, intg, *mcpUpDef, mcpUp, regStore, deps)
			}
			return mcpUp, nil
		default:
			return nil, fmt.Errorf("no upstreams configured")
		}
	}
}

func buildRegistrationStore(deps bootstrap.Deps) mcpoauth.RegistrationStore {
	db, ok := deps.SQLDB.(*sql.DB)
	if !ok || db == nil {
		return nil
	}
	dialect, ok := deps.SQLDialect.(mcpoauth.SQLDialect)
	if !ok || dialect == nil {
		return nil
	}
	enc, err := crypto.NewAESGCM(deps.EncryptionKey)
	if err != nil {
		log.Printf("WARNING: mcpoauth: cannot create encryptor for registration store: %v", err)
		return nil
	}
	store := mcpoauth.NewSQLStore(db, enc, dialect)
	if err := store.Migrate(context.Background()); err != nil {
		log.Printf("WARNING: mcpoauth: migrating registration store: %v", err)
	}
	return store
}

func buildMCPOAuthHandlerFromUpstream(us config.UpstreamDef, store mcpoauth.RegistrationStore, deps bootstrap.Deps) *mcpoauth.Handler {
	redirectURL := us.RedirectURL
	if redirectURL == "" {
		redirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}

	mcpURL := us.URL
	if us.MCPURL != "" {
		mcpURL = us.MCPURL
	}

	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:       mcpURL,
		Store:        store,
		RedirectURL:  redirectURL,
		ClientID:     us.ClientID,
		ClientSecret: us.ClientSecret,
	})
}

func buildMCPOAuthProvider(name string, intg config.IntegrationDef, us config.UpstreamDef, mcpUp *mcpupstream.Upstream, store mcpoauth.RegistrationStore, deps bootstrap.Deps) (core.Provider, error) {
	handler := buildMCPOAuthHandlerFromUpstream(us, store, deps)

	connMode := core.ConnectionModeUser
	if intg.ConnectionMode != "" {
		connMode = core.ConnectionMode(intg.ConnectionMode)
	}

	baseProv := &mcpOAuthMetadataProvider{
		name:        name,
		displayName: intg.DisplayName,
		description: intg.Description,
		connMode:    connMode,
		auth:        handler,
	}

	return composite.New(name, baseProv, mcpUp), nil
}

type mcpOAuthMetadataProvider struct {
	name        string
	displayName string
	description string
	connMode    core.ConnectionMode
	auth        *mcpoauth.Handler
}

func (p *mcpOAuthMetadataProvider) Name() string        { return p.name }
func (p *mcpOAuthMetadataProvider) DisplayName() string { return p.displayName }
func (p *mcpOAuthMetadataProvider) Description() string { return p.description }
func (p *mcpOAuthMetadataProvider) ListOperations() []core.Operation {
	return nil
}

func (p *mcpOAuthMetadataProvider) ConnectionMode() core.ConnectionMode {
	if p.connMode == "" {
		return core.ConnectionModeUser
	}
	return p.connMode
}

func (p *mcpOAuthMetadataProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, fmt.Errorf("integration %q does not expose executable api operations", p.name)
}

func (p *mcpOAuthMetadataProvider) SupportsManualAuth() bool { return true }

func (p *mcpOAuthMetadataProvider) AuthTypes() []string {
	return []string{"oauth", "manual"}
}

func (p *mcpOAuthMetadataProvider) AuthorizationURL(state string, scopes []string) string {
	return p.auth.AuthorizationURL(state, scopes)
}

func (p *mcpOAuthMetadataProvider) StartOAuth(state string, scopes []string) (string, string) {
	return p.auth.StartOAuth(state, scopes)
}

func (p *mcpOAuthMetadataProvider) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return p.auth.StartOAuthWithOverride(authBaseURL, state, scopes)
}

func (p *mcpOAuthMetadataProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.auth.ExchangeCode(ctx, code)
}

func (p *mcpOAuthMetadataProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	return p.auth.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
}

func (p *mcpOAuthMetadataProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.auth.RefreshToken(ctx, refreshToken)
}

func (p *mcpOAuthMetadataProvider) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	return p.auth.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (p *mcpOAuthMetadataProvider) TokenURL() string {
	return p.auth.TokenURL()
}

func (p *mcpOAuthMetadataProvider) AuthorizationBaseURL() string {
	return p.auth.AuthorizationBaseURL()
}
