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
	mcpDef  *config.UpstreamDef
}

func (a *providerAssembly) build() (_ core.Provider, err error) {
	connMode, err := connectionModeForIntegration(a.intg)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			a.cleanup()
		}
	}()

	for i := range a.intg.Upstreams {
		if err = a.addUpstream(&a.intg.Upstreams[i], connMode); err != nil {
			return nil, err
		}
	}
	return a.compose(connMode)
}

func (a *providerAssembly) addUpstream(us *config.UpstreamDef, connMode core.ConnectionMode) error {
	switch us.Type {
	case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
		return a.addAPIUpstream(*us)
	case config.UpstreamTypeMCP:
		return a.addMCPUpstream(us, connMode)
	default:
		return fmt.Errorf("unknown upstream type %q", us.Type)
	}
}

func (a *providerAssembly) addAPIUpstream(us config.UpstreamDef) error {
	if a.apiProv != nil {
		return fmt.Errorf("multiple api upstreams not supported")
	}

	buildOpts := []provider.BuildOption{
		provider.WithEgressResolver(a.deps.Egress.Resolver),
	}
	if us.Auth.Type == "mcp_oauth" {
		buildOpts = append(buildOpts, provider.WithAuthHandler(buildMCPOAuthHandlerFromUpstream(us, a.regStore, a.deps)))
	}

	p, err := providercompiler.BuildProvider(a.ctx, a.name, a.intg, us, a.preparedProviders, buildOpts...)
	if err != nil {
		return err
	}
	a.apiProv = p
	return nil
}

func (a *providerAssembly) addMCPUpstream(us *config.UpstreamDef, connMode core.ConnectionMode) error {
	if a.mcpUp != nil {
		return fmt.Errorf("multiple mcp upstreams not supported")
	}

	up, err := mcpupstream.New(a.ctx, a.name, us.URL, connMode, a.deps.Egress.Resolver)
	if err != nil {
		return err
	}
	if us.AllowedOperations != nil {
		if err := up.FilterOperations(map[string]string(us.AllowedOperations)); err != nil {
			_ = up.Close()
			return err
		}
	}

	a.mcpUp = up
	a.mcpDef = us
	return nil
}

func (a *providerAssembly) compose(connMode core.ConnectionMode) (core.Provider, error) {
	switch {
	case a.apiProv != nil && a.mcpUp != nil:
		return composite.New(a.name, a.apiProv, a.mcpUp), nil
	case a.apiProv != nil:
		return a.apiProv, nil
	case a.mcpUp != nil:
		if a.mcpDef != nil && a.mcpDef.Auth.Type == "mcp_oauth" {
			return buildMCPOAuthProvider(a.name, a.intg, *a.mcpDef, a.mcpUp, a.regStore, a.deps, connMode)
		}
		return a.mcpUp, nil
	default:
		return nil, fmt.Errorf("no upstreams configured")
	}
}

func (a *providerAssembly) cleanup() {
	if a.mcpUp != nil {
		_ = a.mcpUp.Close()
	}
}

func connectionModeForIntegration(intg config.IntegrationDef) (core.ConnectionMode, error) {
	connMode := core.ConnectionModeUser
	switch core.ConnectionMode(intg.ConnectionMode) {
	case "", core.ConnectionModeNone, core.ConnectionModeUser, core.ConnectionModeIdentity, core.ConnectionModeEither:
		if intg.ConnectionMode != "" {
			connMode = core.ConnectionMode(intg.ConnectionMode)
		}
	default:
		return "", fmt.Errorf("unknown connection_mode %q", intg.ConnectionMode)
	}
	return connMode, nil
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

func buildMCPOAuthProvider(name string, intg config.IntegrationDef, us config.UpstreamDef, mcpUp *mcpupstream.Upstream, store mcpoauth.RegistrationStore, deps bootstrap.Deps, connMode core.ConnectionMode) (core.Provider, error) {
	handler := buildMCPOAuthHandlerFromUpstream(us, store, deps)

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
