package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

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
)

type providerFactoryState struct {
	preparedProviders map[string]string
	regStoreOnce      sync.Once
	regStore          mcpoauth.RegistrationStore
}

func defaultProviderFactory(preparedProviders map[string]string) bootstrap.ProviderFactory {
	state := &providerFactoryState{preparedProviders: preparedProviders}
	return state.build
}

func (s *providerFactoryState) build(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (core.Provider, error) {
	assembly := providerAssembly{
		ctx:               ctx,
		name:              name,
		intg:              intg,
		deps:              deps,
		preparedProviders: s.preparedProviders,
		regStore:          s.registrationStore(deps),
	}
	return assembly.build()
}

func (s *providerFactoryState) registrationStore(deps bootstrap.Deps) mcpoauth.RegistrationStore {
	s.regStoreOnce.Do(func() {
		s.regStore = buildRegistrationStore(deps)
	})
	return s.regStore
}

type providerAssembly struct {
	ctx               context.Context
	name              string
	intg              config.IntegrationDef
	deps              bootstrap.Deps
	preparedProviders map[string]string
	regStore          mcpoauth.RegistrationStore

	apiProv core.Provider
	mcpUp   *mcpupstream.Upstream
	mcpConn *config.ConnectionDef
}

func (a *providerAssembly) build() (_ core.Provider, err error) {
	connMode := connectionModeForIntegration(a.intg)

	defer func() {
		if err != nil {
			a.cleanup()
		}
	}()

	if a.intg.API != nil {
		conn := a.intg.Connections[a.intg.API.Connection]
		buildOpts := []provider.BuildOption{
			provider.WithEgressResolver(a.deps.Egress.Resolver),
		}
		if conn.Auth.Type == "mcp_oauth" {
			mcpURL := ""
			if a.intg.MCP != nil {
				mcpURL = a.intg.MCP.URL
			}
			buildOpts = append(buildOpts, provider.WithAuthHandler(buildMCPOAuthHandler(conn, mcpURL, a.regStore, a.deps)))
		}

		p, err := providercompiler.BuildProvider(a.ctx, a.name, a.intg, *a.intg.API, conn, a.preparedProviders, buildOpts...)
		if err != nil {
			return nil, err
		}
		a.apiProv = p
	}

	if a.intg.MCP != nil {
		conn := a.intg.Connections[a.intg.MCP.Connection]
		up, err := mcpupstream.New(a.ctx, a.name, a.intg.MCP.URL, connMode, a.deps.Egress.Resolver)
		if err != nil {
			return nil, err
		}
		a.mcpUp = up
		a.mcpConn = &conn
	}

	switch {
	case a.apiProv != nil && a.mcpUp != nil:
		return composite.New(a.name, a.apiProv, a.mcpUp), nil
	case a.apiProv != nil:
		return a.apiProv, nil
	case a.mcpUp != nil:
		if a.mcpConn != nil && a.mcpConn.Auth.Type == "mcp_oauth" {
			return buildMCPOAuthProvider(a.name, a.intg, *a.mcpConn, a.mcpUp, a.regStore, a.deps, connMode)
		}
		return a.mcpUp, nil
	default:
		return nil, fmt.Errorf("no surfaces configured")
	}
}

func (a *providerAssembly) cleanup() {
	if a.mcpUp != nil {
		_ = a.mcpUp.Close()
	}
}

func connectionModeForIntegration(intg config.IntegrationDef) core.ConnectionMode {
	return core.ConnectionMode(config.ConnectionMode(intg.Connections))
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

func buildMCPOAuthHandler(conn config.ConnectionDef, mcpURL string, store mcpoauth.RegistrationStore, deps bootstrap.Deps) *mcpoauth.Handler {
	redirectURL := conn.Auth.RedirectURL
	if redirectURL == "" {
		redirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}

	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:       mcpURL,
		Store:        store,
		RedirectURL:  redirectURL,
		ClientID:     conn.Auth.ClientID,
		ClientSecret: conn.Auth.ClientSecret,
	})
}

func buildMCPOAuthProvider(name string, intg config.IntegrationDef, conn config.ConnectionDef, mcpUp *mcpupstream.Upstream, store mcpoauth.RegistrationStore, deps bootstrap.Deps, connMode core.ConnectionMode) (core.Provider, error) {
	mcpURL := ""
	if intg.MCP != nil {
		mcpURL = intg.MCP.URL
	}
	handler := buildMCPOAuthHandler(conn, mcpURL, store, deps)

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
