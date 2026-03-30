package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
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

func (s *providerFactoryState) build(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (*bootstrap.ProviderBuildResult, error) {
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

func (a *providerAssembly) build() (_ *bootstrap.ProviderBuildResult, err error) {
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

		p, err := providercompiler.BuildProvider(a.ctx, a.name, a.intg, *a.intg.API, conn, a.preparedProviders, a.intg.API.AllowedOperations, buildOpts...)
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
		if a.intg.MCP.AllowedOperations != nil {
			if err := up.FilterOperations(a.intg.MCP.AllowedOperations); err != nil {
				return nil, fmt.Errorf("integration %q mcp: %w", a.name, err)
			}
		}
		a.mcpUp = up
		a.mcpConn = &conn
	}

	var prov core.Provider
	switch {
	case a.apiProv != nil && a.mcpUp != nil:
		prov = composite.New(a.name, a.apiProv, a.mcpUp)
	case a.apiProv != nil:
		prov = a.apiProv
	case a.mcpUp != nil:
		if a.mcpConn != nil && a.mcpConn.Auth.Type == "mcp_oauth" {
			p, buildErr := buildMCPOAuthProvider(a.name, a.intg, a.mcpUp, connMode)
			if buildErr != nil {
				return nil, buildErr
			}
			prov = p
		} else {
			prov = a.mcpUp
		}
	default:
		return nil, fmt.Errorf("no surfaces configured")
	}

	connAuth := a.buildConnectionAuth()

	return &bootstrap.ProviderBuildResult{
		Provider:       prov,
		ConnectionAuth: connAuth,
	}, nil
}

func (a *providerAssembly) buildConnectionAuth() map[string]bootstrap.OAuthHandler {
	authMap := make(map[string]bootstrap.OAuthHandler)

	for connName := range a.intg.Connections {
		conn := a.intg.Connections[connName]
		switch conn.Auth.Type {
		case "oauth2", "":
			if conn.Auth.AuthorizationURL == "" && conn.Auth.ClientID == "" {
				continue
			}
			handler := a.buildOAuth2Handler(connName, conn)
			if handler != nil {
				authMap[connName] = handler
			}
		case "mcp_oauth":
			mcpURL := ""
			if a.intg.MCP != nil {
				mcpURL = a.intg.MCP.URL
			}
			authMap[connName] = buildMCPOAuthHandler(conn, mcpURL, a.regStore, a.deps)
		}
	}

	if len(authMap) == 0 {
		return nil
	}
	return authMap
}

func (a *providerAssembly) buildOAuth2Handler(connName string, conn config.ConnectionDef) bootstrap.OAuthHandler {
	if a.intg.API == nil {
		return nil
	}
	def, err := providercompiler.LoadDefinition(a.ctx, a.name, *a.intg.API, a.preparedProviders)
	if err != nil {
		slog.Warn("cannot load definition for connection oauth handler", "provider", a.name, "connection", connName, "error", err)
		return nil
	}

	defCopy := *def
	provider.ApplyConnectionAuth(&defCopy, conn)

	upstream, err := provider.BuildOAuthUpstream(&defCopy, conn, defCopy.BaseURL, nil)
	if err != nil {
		slog.Warn("cannot build oauth handler for connection", "provider", a.name, "connection", connName, "error", err)
		return nil
	}
	return bootstrap.WrapUpstreamHandler(upstream)
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
		slog.Warn("cannot create encryptor for registration store", "component", "mcpoauth", "error", err)
		return nil
	}
	store := mcpoauth.NewSQLStore(db, enc, dialect)
	if err := store.Migrate(context.Background()); err != nil {
		slog.Warn("registration store migration failed", "component", "mcpoauth", "error", err)
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

func buildMCPOAuthProvider(name string, intg config.IntegrationDef, mcpUp *mcpupstream.Upstream, connMode core.ConnectionMode) (core.Provider, error) {
	baseProv := &mcpOAuthMetadataProvider{
		name:        name,
		displayName: intg.DisplayName,
		description: intg.Description,
		connMode:    connMode,
	}

	return composite.New(name, baseProv, mcpUp), nil
}

type mcpOAuthMetadataProvider struct {
	name        string
	displayName string
	description string
	connMode    core.ConnectionMode
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
