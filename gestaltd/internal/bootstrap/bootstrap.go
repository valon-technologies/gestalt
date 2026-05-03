package bootstrap

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/oauth"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

// OAuthHandler covers every OAuth method needed by the server (start, exchange,
// refresh) and the broker (refresh). mcpoauth.Handler satisfies this directly;
// use WrapUpstreamHandler to adapt an oauth.UpstreamHandler.
type OAuthHandler interface {
	AuthorizationURL(state string, scopes []string) string
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
	StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error)
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	AuthorizationBaseURL() string
	TokenURL() string
}

type ManualTokenExchanger interface {
	ExchangeCredentials(ctx context.Context, credentialJSON string) (*core.TokenResponse, error)
	ExchangeCredentialsWithURL(ctx context.Context, credentialJSON, tokenURL string) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	TokenURL() string
}

// upstreamHandlerAdapter wraps an oauth.UpstreamHandler to satisfy OAuthHandler.
type upstreamHandlerAdapter struct {
	*oauth.UpstreamHandler
}

// WrapUpstreamHandler adapts an oauth.UpstreamHandler to the OAuthHandler
// interface. The adapter maps StartOAuth to AuthorizationURLWithPKCE and
// ExchangeCodeWithVerifier to ExchangeCode with option injection.
func WrapUpstreamHandler(h *oauth.UpstreamHandler) OAuthHandler {
	return &upstreamHandlerAdapter{UpstreamHandler: h}
}

func (a *upstreamHandlerAdapter) AuthorizationURL(state string, scopes []string) string {
	url, _ := a.AuthorizationURLWithPKCE(state, scopes)
	return url
}

func (a *upstreamHandlerAdapter) StartOAuth(state string, scopes []string) (string, string) {
	return a.AuthorizationURLWithPKCE(state, scopes)
}

func (a *upstreamHandlerAdapter) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return a.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (a *upstreamHandlerAdapter) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return a.UpstreamHandler.ExchangeCode(ctx, code)
}

func (a *upstreamHandlerAdapter) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return a.UpstreamHandler.ExchangeCode(ctx, code, opts...)
}

// ProviderBuildResult carries the constructed provider and an OAuth handler
// for each named connection that uses oauth2 or mcp_oauth auth.
type ProviderBuildResult struct {
	Provider             core.Provider
	ConnectionAuth       map[string]OAuthHandler
	ManualConnectionAuth map[string]ManualTokenExchanger
}

type providerMetadata struct {
	displayName string
	description string
	iconSVG     string
}

func resolveProviderMetadata(entry *config.ProviderEntry) providerMetadata {
	meta := providerMetadata{
		displayName: entry.DisplayName,
		description: entry.Description,
	}
	if entry.IconFile == "" {
		return meta
	}

	svg, err := declarative.ReadIconFile(entry.IconFile)
	if err != nil {
		slog.Warn("could not read icon_file", "path", entry.IconFile, "error", err)
		return meta
	}
	meta.iconSVG = svg
	return meta
}

func (m providerMetadata) displayNameOr(v string) string {
	if m.displayName != "" {
		return m.displayName
	}
	return v
}

func (m providerMetadata) descriptionOr(v string) string {
	if m.description != "" {
		return m.description
	}
	return v
}

type Deps struct {
	// EncryptionKey is the derived 32-byte key from server.encryptionKey, not the
	// raw config value.
	EncryptionKey         []byte
	BaseURL               string
	RuntimeRelayBaseURL   string
	SecretManager         core.SecretManager
	Services              *coredata.Services
	SelectedIndexedDBName string
	IndexedDBs            map[string]indexeddb.IndexedDB
	IndexedDBDefs         map[string]*config.ProviderEntry
	IndexedDBFactory      IndexedDBFactory
	CacheDefs             map[string]*config.ProviderEntry
	CacheFactory          CacheFactory
	S3                    map[string]s3store.Client
	WorkflowRuntime       *workflowRuntime
	AgentRuntime          *agentRuntime
	AgentRunGrants        *agentgrant.Manager
	WorkflowManager       workflowmanager.Service
	AgentManager          agentmanager.Service
	Egress                EgressDeps
	AuthorizationProvider core.AuthorizationProvider
	PluginInvoker         invocation.Invoker
	PluginRuntime         pluginruntime.Provider
	PluginRuntimeRegistry *pluginRuntimeRegistry
	PublicHostServices    *runtimehost.PublicHostServiceRegistry
	HostServiceTLSCAFile  string
	HostServiceTLSCAPEM   string
	Telemetry             core.TelemetryProvider
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthenticationProvider, error)
type AuthorizationFactory func(node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (core.AuthorizationProvider, error)
type ExternalCredentialFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (core.ExternalCredentialProvider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type IndexedDBFactory func(node yaml.Node) (indexeddb.IndexedDB, error)
type CacheFactory func(node yaml.Node) (corecache.Cache, error)
type S3Factory func(node yaml.Node) (s3store.Client, error)
type WorkflowFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreworkflow.Provider, error)
type AgentFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreagent.Provider, error)
type RuntimeFactory func(ctx context.Context, name string, entry *config.RuntimeProviderEntry, deps Deps) (pluginruntime.Provider, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)
type AuditFactory func(ctx context.Context, cfg config.ProviderEntry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error)

type FactoryRegistry struct {
	Auth                AuthFactory
	Authorization       AuthorizationFactory
	ExternalCredentials ExternalCredentialFactory
	Secrets             map[string]SecretManagerFactory
	IndexedDB           IndexedDBFactory
	Cache               CacheFactory
	Runtime             RuntimeFactory
	S3                  S3Factory
	Workflow            WorkflowFactory
	Agent               AgentFactory
	Telemetry           map[string]TelemetryFactory
	Audit               AuditFactory
	Builtins            []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Secrets:   make(map[string]SecretManagerFactory),
		Runtime:   buildExecutablePluginRuntime,
		Telemetry: make(map[string]TelemetryFactory),
	}
}

type Result struct {
	Auth                  core.AuthenticationProvider
	SelectedAuthProvider  string
	AuthProviders         map[string]core.AuthenticationProvider
	AuthorizationProvider core.AuthorizationProvider
	Services              *coredata.Services
	ExtraIndexedDBs       []indexeddb.IndexedDB
	S3                    map[string]s3store.Client
	ExtraS3s              []s3store.Client
	ExtraWorkflows        []coreworkflow.Provider
	ExtraAgents           []coreagent.Provider
	Providers             *registry.ProviderMap[core.Provider]
	WorkflowControl       WorkflowControl
	AgentControl          AgentControl
	AgentManager          agentmanager.Service
	ProvidersReady        <-chan struct{}
	Authorizer            authorization.RuntimeAuthorizer
	ConnectionAuth        func() map[string]map[string]OAuthHandler
	ManualConnectionAuth  func() map[string]map[string]ManualTokenExchanger
	Invoker               invocation.Invoker
	PluginInvoker         invocation.Invoker
	CapabilityLister      invocation.CapabilityLister
	AuditSink             core.AuditSink
	SecretManager         core.SecretManager
	Telemetry             core.TelemetryProvider
	PluginRuntimes        RuntimeInspector
	ProviderDevSessions   *providerdev.Manager
	PublicHostServices    *runtimehost.PublicHostServiceRegistry

	pluginRuntimeRegistry *pluginRuntimeRegistry
	auditClose            func(context.Context) error
	mu                    sync.Mutex
	closed                bool
}

func (r *Result) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("bootstrap result already closed")
	}
	if r.Authorizer != nil {
		started := time.Now()
		if err := r.Authorizer.Start(ctx); err != nil {
			return err
		}
		slog.InfoContext(ctx, "authorization provider state loaded", "duration", time.Since(started).String())
	}
	return nil
}

func (r *Result) StartWorkflowProviders(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("bootstrap result already closed")
	}
	providers := append([]coreworkflow.Provider(nil), r.ExtraWorkflows...)
	r.mu.Unlock()

	var errs []error
	for _, provider := range providers {
		if starter, ok := provider.(startableWorkflowProvider); ok {
			started := time.Now()
			if err := starter.Start(ctx); err != nil {
				errs = append(errs, err)
				continue
			}
			slog.InfoContext(ctx, "workflow provider started", "duration", time.Since(started).String())
		}
	}
	return errors.Join(errs...)
}

func (r *Result) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.ProvidersReady != nil {
		<-r.ProvidersReady
	}
	var errs []error
	authCloseErr := closeAuth(r.Auth)
	if len(r.AuthProviders) != 0 {
		authCloseErr = closeAuthProviders(r.AuthProviders)
	}
	externalCredentialsCloseErr := closeExternalCredentialProviderCandidate(r.Services)
	errs = append(errs,
		closeAuthorizer(r.Authorizer),
		authCloseErr,
		closeAuthorizationProvider(r.AuthorizationProvider),
		externalCredentialsCloseErr,
		closeProviderDevSessions(r.ProviderDevSessions),
		CloseProviders(r.Providers),
		r.Services.Close(),
		closeIndexedDBs(r.ExtraIndexedDBs...),
		closeS3s(r.ExtraS3s...),
		closeWorkflows(r.ExtraWorkflows...),
		closeAgents(r.ExtraAgents...),
		closeSecretManager(r.SecretManager),
		closePluginRuntimeRegistry(r.pluginRuntimeRegistry),
	)
	if r.auditClose != nil {
		errs = append(errs, r.auditClose(ctx))
	}
	if r.Telemetry != nil {
		errs = append(errs, r.Telemetry.Shutdown(ctx))
	}
	r.closed = true
	return errors.Join(errs...)
}

func closeIfPossible(values ...any) {
	for _, value := range values {
		if c, ok := value.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

func closeIndexedDBs(stores ...indexeddb.IndexedDB) error {
	var errs []error
	for _, store := range stores {
		if store == nil {
			continue
		}
		if err := store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeS3s(clients ...s3store.Client) error {
	var errs []error
	for _, client := range clients {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeWorkflows(providers ...coreworkflow.Provider) error {
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if err := provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeAgents(providers ...coreagent.Provider) error {
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if err := provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type startableWorkflowProvider interface {
	Start(context.Context) error
}

type workflowProviderWithCleanup struct {
	coreworkflow.Provider
	cleanup func()
}

type workflowProviderWithExecutionReferencesAndCleanup struct {
	coreworkflow.Provider
	coreworkflow.ExecutionReferenceStore
	cleanup func()
}

func (p *workflowProviderWithCleanup) Close() error {
	var errs []error
	if p != nil && p.Provider != nil {
		if err := p.Provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p != nil && p.cleanup != nil {
		p.cleanup()
	}
	return errors.Join(errs...)
}

func (p *workflowProviderWithCleanup) Start(ctx context.Context) error {
	if p == nil || p.Provider == nil {
		return nil
	}
	if starter, ok := p.Provider.(startableWorkflowProvider); ok {
		return starter.Start(ctx)
	}
	return nil
}

func (p *workflowProviderWithExecutionReferencesAndCleanup) Close() error {
	var errs []error
	if p != nil && p.Provider != nil {
		if err := p.Provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p != nil && p.cleanup != nil {
		p.cleanup()
	}
	return errors.Join(errs...)
}

func (p *workflowProviderWithExecutionReferencesAndCleanup) Start(ctx context.Context) error {
	if p == nil || p.Provider == nil {
		return nil
	}
	if starter, ok := p.Provider.(startableWorkflowProvider); ok {
		return starter.Start(ctx)
	}
	return nil
}

type agentProviderWithCleanup struct {
	coreagent.Provider
	cleanup func()
}

func (p *agentProviderWithCleanup) Close() error {
	var errs []error
	if p != nil && p.Provider != nil {
		if err := p.Provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p != nil && p.cleanup != nil {
		p.cleanup()
	}
	return errors.Join(errs...)
}

type agentProviderWithTracking struct {
	delegate     coreagent.Provider
	providerName string
}

func (p *agentProviderWithTracking) CreateSession(ctx context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	session, err := p.delegate.CreateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	requestedID := strings.TrimSpace(req.SessionID)
	if requestedID != "" && session != nil {
		actualID := strings.TrimSpace(session.ID)
		if actualID != "" && actualID != requestedID && strings.TrimSpace(req.IdempotencyKey) == "" {
			return nil, fmt.Errorf("%w: agent provider %q returned session id %q for requested session id %q", invocation.ErrInternal, p.providerName, actualID, requestedID)
		}
	}
	return session, nil
}

func (p *agentProviderWithTracking) GetSession(ctx context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetSession(ctx, req)
}

func (p *agentProviderWithTracking) ListSessions(ctx context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListSessions(ctx, req)
}

func (p *agentProviderWithTracking) UpdateSession(ctx context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.UpdateSession(ctx, req)
}

func (p *agentProviderWithTracking) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	turn, err := p.delegate.CreateTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	requestedID := strings.TrimSpace(req.TurnID)
	if requestedID != "" && turn != nil {
		actualID := strings.TrimSpace(turn.ID)
		if actualID != "" && actualID != requestedID && strings.TrimSpace(req.IdempotencyKey) == "" {
			err := fmt.Errorf("%w: agent provider %q returned turn id %q for requested turn id %q", invocation.ErrInternal, p.providerName, actualID, requestedID)
			cancelErr := p.cancelProviderTurn(actualID, "agent provider returned mismatched turn id")
			if cancelErr != nil {
				return nil, errors.Join(err, cancelErr)
			}
			return nil, err
		}
	}
	return turn, nil
}

func (p *agentProviderWithTracking) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetTurn(ctx, req)
}

func (p *agentProviderWithTracking) ListTurns(ctx context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListTurns(ctx, req)
}

func (p *agentProviderWithTracking) CancelTurn(ctx context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.CancelTurn(ctx, req)
}

func (p *agentProviderWithTracking) ListTurnEvents(ctx context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListTurnEvents(ctx, req)
}

func (p *agentProviderWithTracking) GetInteraction(ctx context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetInteraction(ctx, req)
}

func (p *agentProviderWithTracking) ListInteractions(ctx context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListInteractions(ctx, req)
}

func (p *agentProviderWithTracking) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ResolveInteraction(ctx, req)
}

func (p *agentProviderWithTracking) GetCapabilities(ctx context.Context, req coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetCapabilities(ctx, req)
}

func (p *agentProviderWithTracking) Ping(ctx context.Context) error {
	if p == nil || p.delegate == nil {
		return fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.Ping(ctx)
}

func (p *agentProviderWithTracking) cancelProviderTurn(turnID string, reason string) error {
	if p == nil || p.delegate == nil {
		return nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	_, cancelErr := p.delegate.CancelTurn(context.Background(), coreagent.CancelTurnRequest{
		TurnID: turnID,
		Reason: strings.TrimSpace(reason),
	})
	if cancelErr != nil && !errors.Is(cancelErr, core.ErrNotFound) {
		return cancelErr
	}
	return nil
}

func (p *agentProviderWithTracking) Close() error {
	if p == nil || p.delegate == nil {
		return nil
	}
	return p.delegate.Close()
}

type preparedCore struct {
	Auth                  core.AuthenticationProvider
	SelectedAuthProvider  string
	AuthProviders         map[string]core.AuthenticationProvider
	AuthorizationProvider core.AuthorizationProvider
	Services              *coredata.Services
	ExtraIndexedDBs       []indexeddb.IndexedDB
	ExtraS3s              []s3store.Client
	SecretManager         core.SecretManager
	Telemetry             core.TelemetryProvider
	Deps                  Deps

	pluginRuntimeRegistry *pluginRuntimeRegistry
}

type configSecretManagers struct {
	ctx       context.Context
	cfg       *config.Config
	factories *FactoryRegistry
	managers  map[string]core.SecretManager
}

func newConfigSecretManagers(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*configSecretManagers, error) {
	referenced, err := config.ReferencedConfigSecretProviders(cfg)
	if err != nil {
		return nil, err
	}
	if len(referenced) == 0 {
		return nil, nil
	}
	return &configSecretManagers{
		ctx:       ctx,
		cfg:       cfg,
		factories: factories,
		managers:  make(map[string]core.SecretManager, len(referenced)),
	}, nil
}

func (r *configSecretManagers) resolve(ref config.SecretRef) (string, error) {
	sm, err := r.manager(ref.Provider)
	if err != nil {
		return "", err
	}
	value, err := sm.GetSecret(r.ctx, ref.Name)
	if err != nil {
		return "", fmt.Errorf("provider %q: %w", ref.Provider, err)
	}
	return value, nil
}

func (r *configSecretManagers) manager(name string) (core.SecretManager, error) {
	if sm, ok := r.managers[name]; ok {
		return sm, nil
	}
	entry := r.cfg.Providers.Secrets[name]
	if entry == nil {
		return nil, fmt.Errorf("config validation: secret refs reference unknown secrets provider %q", name)
	}
	sm, err := buildNamedSecretManager(name, entry, r.factories)
	if err != nil {
		return nil, err
	}
	r.managers[name] = sm
	return sm, nil
}

func (r *configSecretManagers) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, sm := range r.managers {
		errs = append(errs, closeSecretManager(sm))
	}
	return errors.Join(errs...)
}

// ResolveConfigSecrets resolves structured config secret refs using their
// referenced secrets providers, then closes the temporary secret managers.
func ResolveConfigSecrets(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) error {
	if err := config.CanonicalizeStructure(cfg); err != nil {
		return err
	}
	resolver, err := newConfigSecretManagers(ctx, cfg, factories)
	if err != nil {
		return err
	}
	if resolver == nil {
		return nil
	}
	defer func() { _ = resolver.Close() }()
	resolveValue := func(val string) (string, error) {
		ref, ok, err := config.ParseSecretRefTransport(val)
		if err != nil {
			return "", err
		}
		if !ok {
			return val, nil
		}
		resolved, err := resolver.resolve(ref)
		if err != nil {
			var secretErr *core.SecretResolutionError
			if errors.As(err, &secretErr) {
				return "", err
			}
			return "", &core.SecretResolutionError{
				Name: ref.Name,
				Err:  err,
			}
		}
		if resolved == "" {
			return "", &core.SecretResolutionError{Name: ref.Name, Err: fmt.Errorf("resolved to empty value")}
		}
		return resolved, nil
	}
	if err := config.TransformConfigStringFields(cfg, resolveValue); err != nil {
		return err
	}
	return config.CanonicalizeStructure(cfg)
}

func prepareCore(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, requireEncryptionKey bool) (*preparedCore, error) {
	if err := ResolveConfigSecrets(ctx, cfg, factories); err != nil {
		return nil, err
	}
	sm, err := buildRuntimeSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	closeSM := true
	defer func() {
		if closeSM {
			_ = closeSecretManager(sm)
		}
	}()

	tp, err := buildTelemetry(cfg, factories)
	if err != nil {
		return nil, err
	}
	shutdownTelemetry := true
	defer func() {
		if shutdownTelemetry {
			_ = tp.Shutdown(context.Background())
		}
	}()

	encKey := crypto.DeriveKey(cfg.Server.EncryptionKey)
	if requireEncryptionKey && encKey == nil {
		return nil, fmt.Errorf("bootstrap: server.encryption_key is required")
	}
	agentRunGrants, err := agentgrant.NewManager(encKey)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: agent run grants: %w", err)
	}
	hostServiceTLSCAFile, hostServiceTLSCAPEM, err := hostServiceTLSCAFromEnv()
	if err != nil {
		return nil, err
	}

	deps := Deps{
		EncryptionKey:        encKey,
		BaseURL:              cfg.Server.BaseURL,
		RuntimeRelayBaseURL:  cfg.Server.Runtime.RelayBaseURL,
		SecretManager:        sm,
		Telemetry:            tp,
		AgentRunGrants:       agentRunGrants,
		HostServiceTLSCAFile: hostServiceTLSCAFile,
		HostServiceTLSCAPEM:  hostServiceTLSCAPEM,
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		return nil, err
	}
	workflowRuntime.InitProviderPlaceholders(cfg.Providers.Workflow)
	deps.WorkflowRuntime = workflowRuntime
	agentRuntime, err := newAgentRuntime(cfg)
	if err != nil {
		return nil, err
	}
	deps.AgentRuntime = agentRuntime
	deps.AgentRuntime.SetRunGrants(agentRunGrants)

	selectedAuthName, authProviders, err := buildAuthProviders(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	auth := authProviders[selectedAuthName]
	var authzProvider core.AuthorizationProvider
	closeAuthorizationOnError := true
	defer func() {
		if closeAuthorizationOnError {
			_ = closeAuthorizationProvider(authzProvider)
		}
	}()

	selectedIndexedDBName, def, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}
	if selectedIndexedDBName == "" || def == nil {
		return nil, fmt.Errorf("bootstrap: datastore resource name is required")
	}
	store, storeErr := buildIndexedDB(def, factories)
	if storeErr != nil {
		return nil, fmt.Errorf("bootstrap: system indexeddb from resource %q: %w", selectedIndexedDBName, storeErr)
	}
	store = metricutil.InstrumentIndexedDB(store, selectedIndexedDBName)
	svc, svcErr := coredata.NewWithContext(ctx, store)
	if svcErr != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: system indexeddb from resource %q: %w", selectedIndexedDBName, svcErr)
	}
	hostIndexedDBs := map[string]indexeddb.IndexedDB{selectedIndexedDBName: store}
	var extraIndexedDBs []indexeddb.IndexedDB
	for name, entry := range cfg.Providers.IndexedDB {
		if name == selectedIndexedDBName || entry == nil {
			continue
		}
		ds, err := buildIndexedDB(entry, factories)
		if err != nil {
			_ = svc.Close()
			_ = closeIndexedDBs(extraIndexedDBs...)
			return nil, fmt.Errorf("bootstrap: indexeddb from resource %q: %w", name, err)
		}
		ds = metricutil.InstrumentIndexedDB(ds, name)
		hostIndexedDBs[name] = ds
		extraIndexedDBs = append(extraIndexedDBs, ds)
	}
	closeSvc := true
	closeExtraStores := true
	defer func() {
		if closeSvc {
			_ = svc.Close()
		}
		if closeExtraStores {
			_ = closeIndexedDBs(extraIndexedDBs...)
		}
	}()

	hostS3s := make(map[string]s3store.Client, len(cfg.Providers.S3))
	var extraS3s []s3store.Client
	for name, entry := range cfg.Providers.S3 {
		if entry == nil {
			continue
		}
		client, err := buildS3(name, entry, factories)
		if err != nil {
			_ = closeS3s(extraS3s...)
			return nil, fmt.Errorf("bootstrap: s3 from resource %q: %w", name, err)
		}
		hostS3s[name] = client
		extraS3s = append(extraS3s, client)
	}
	closeExtraS3s := true
	defer func() {
		if closeExtraS3s {
			_ = closeS3s(extraS3s...)
		}
	}()

	deps.Egress = newEgressDeps(cfg)
	deps.Services = svc
	deps.IndexedDBs = hostIndexedDBs
	deps.SelectedIndexedDBName = selectedIndexedDBName
	deps.IndexedDBDefs = cfg.Providers.IndexedDB
	deps.IndexedDBFactory = factories.IndexedDB
	deps.CacheDefs = cfg.Providers.Cache
	deps.CacheFactory = factories.Cache
	deps.S3 = hostS3s
	closeExternalCredentialsOnError := true
	defer func() {
		if closeExternalCredentialsOnError {
			_ = closeExternalCredentialProviderCandidate(svc)
		}
	}()
	externalCredentials, err := buildExternalCredentialsProvider(ctx, cfg, factories, deps)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	svc.ExternalCredentials = externalCredentials

	authzProvider, err = buildAuthorization(cfg, factories, deps)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	deps.AuthorizationProvider = authzProvider
	runtimeRegistry := newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	deps.PluginRuntimeRegistry = runtimeRegistry

	closeSM = false
	shutdownTelemetry = false
	closeSvc = false
	closeExtraStores = false
	closeExtraS3s = false
	closeExternalCredentialsOnError = false
	closeAuthorizationOnError = false
	return &preparedCore{
		Auth:                  auth,
		SelectedAuthProvider:  selectedAuthName,
		AuthProviders:         authProviders,
		AuthorizationProvider: authzProvider,
		Services:              svc,
		ExtraIndexedDBs:       extraIndexedDBs,
		ExtraS3s:              extraS3s,
		SecretManager:         sm,
		Telemetry:             tp,
		Deps:                  deps,
		pluginRuntimeRegistry: runtimeRegistry,
	}, nil
}

func hostServiceTLSCAFromEnv() (caFile string, caPEM string, err error) {
	if pemValue := strings.TrimSpace(os.Getenv(hostServiceTLSCAPEMEnv)); pemValue != "" {
		return "", pemValue, nil
	}
	caFile = strings.TrimSpace(os.Getenv(hostServiceTLSCAFileEnv))
	if caFile == "" {
		return "", "", nil
	}
	data, err := os.ReadFile(caFile)
	if err != nil {
		return "", "", fmt.Errorf("bootstrap: read %s %q: %w", hostServiceTLSCAFileEnv, caFile, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", "", fmt.Errorf("bootstrap: %s %q is empty", hostServiceTLSCAFileEnv, caFile)
	}
	return "", strings.TrimSpace(string(data)), nil
}

func (p *preparedCore) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}

	var errs []error
	authCloseErr := closeAuth(p.Auth)
	if len(p.AuthProviders) != 0 {
		authCloseErr = closeAuthProviders(p.AuthProviders)
	}
	externalCredentialsCloseErr := closeExternalCredentialProviderCandidate(p.Services)
	errs = append(errs,
		authCloseErr,
		closeAuthorizationProvider(p.AuthorizationProvider),
		externalCredentialsCloseErr,
		p.Services.Close(),
		closeIndexedDBs(p.ExtraIndexedDBs...),
		closeS3s(p.ExtraS3s...),
		closeSecretManager(p.SecretManager),
		closePluginRuntimeRegistry(p.pluginRuntimeRegistry),
	)
	if p.Telemetry != nil {
		errs = append(errs, p.Telemetry.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func Bootstrap(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*Result, error) {
	prepared, err := prepareCore(ctx, cfg, factories, true)
	if err != nil {
		return nil, err
	}
	closeCore := true
	defer func() {
		if closeCore {
			_ = prepared.Close(context.Background())
		}
	}()

	pluginInvoker := newLazyInvoker()
	workflowManager := newLazyWorkflowManager()
	agentManager := newLazyAgentManager()
	workflowTools := newWorkflowSystemTools(workflowManager, prepared.Deps.WorkflowRuntime)
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	prepared.Deps.PluginInvoker = pluginInvoker
	prepared.Deps.WorkflowManager = workflowManager
	prepared.Deps.AgentManager = agentManager
	prepared.Deps.PublicHostServices = publicHostServices
	prepared.Deps.WorkflowRuntime.SetAgentManager(agentManager)

	providers, providersReady, connAuthResolver, manualConnAuthResolver, err := buildProviders(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return nil, err
	}
	closeProviders := true
	defer func() {
		if closeProviders {
			<-providersReady
			_ = CloseProviders(providers)
		}
	}()

	connMaps, err := BuildConnectionMaps(cfg)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	connRuntime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	baseAuthz, err := authorization.New(config.AuthorizationStaticConfig(cfg.Authorization, cfg.Plugins))
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	closeAuthz := true
	defer func() {
		if closeAuthz {
			_ = baseAuthz.Close()
		}
	}()
	var authz authorization.RuntimeAuthorizer = baseAuthz
	if prepared.AuthorizationProvider != nil {
		authz, err = authorization.NewProviderBacked(baseAuthz, prepared.AuthorizationProvider)
		if err != nil {
			prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
			return nil, err
		}
	}
	providerDevSessions, err := buildProviderDevManager(cfg, providers, prepared.Deps)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	sharedInvoker := invocation.NewBroker(providers, prepared.Services.Users, prepared.Services.ExternalCredentials,
		invocation.WithAuthorizer(authz),
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
		invocation.WithManualConnectionAuth(lazyManualRefreshers(providersReady, manualConnAuthResolver)),
		invocation.WithConnectionRuntime(connRuntime.Resolve),
		invocation.WithProviderOverrides(providerDevSessions),
	)
	prepared.Deps.WorkflowRuntime.SetInvoker(sharedInvoker)
	prepared.Deps.AgentRuntime.SetInvoker(sharedInvoker)
	prepared.Deps.AgentRuntime.SetSystemToolExecutor(workflowTools)
	workflowManager.SetTarget(workflowmanager.New(workflowmanager.Config{
		Providers:         providers,
		Workflow:          prepared.Deps.WorkflowRuntime,
		Agent:             prepared.Deps.AgentRuntime,
		AgentManager:      agentManager,
		Invoker:           sharedInvoker,
		Authorizer:        authz,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.APIConnection,
		PluginInvokes:     agentPluginInvokes(cfg),
	}))
	agentManager.SetTarget(agentmanager.New(agentmanager.Config{
		Providers:         providers,
		Agent:             prepared.Deps.AgentRuntime,
		WorkflowTools:     workflowTools,
		RunGrants:         prepared.Deps.AgentRunGrants,
		Invoker:           sharedInvoker,
		Authorizer:        authz,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.APIConnection,
		PluginInvokes:     agentPluginInvokes(cfg),
		AgentConnections:  agentConnectionBindings(cfg),
	}))
	prepared.Deps.AgentRuntime.SetToolSearcher(agentManager)
	extraWorkflows, err := buildWorkflows(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return nil, err
	}
	closeWorkflowsOnError := true
	defer func() {
		if closeWorkflowsOnError {
			_ = closeWorkflows(extraWorkflows...)
		}
	}()
	extraAgents, err := buildAgents(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return nil, err
	}
	closeAgentsOnError := true
	defer func() {
		if closeAgentsOnError {
			_ = closeAgents(extraAgents...)
		}
	}()
	audit, auditClose, err := buildAuditSink(ctx, cfg, factories, prepared.Telemetry)
	if err != nil {
		return nil, err
	}
	closeAudit := true
	defer func() {
		if closeAudit && auditClose != nil {
			_ = auditClose(context.Background())
		}
	}()
	pluginInvoker.SetTarget(invocation.NewGuarded(sharedInvoker, nil, "plugin", audit, invocation.WithoutRateLimit()))
	if err := reconcileWorkflowConfigSchedules(ctx, cfg, prepared.Deps.WorkflowRuntime); err != nil {
		return nil, err
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, prepared.Deps.WorkflowRuntime); err != nil {
		return nil, err
	}

	closeProviders = false
	closeCore = false
	closeAudit = false
	closeAuthz = false
	closeWorkflowsOnError = false
	closeAgentsOnError = false
	return &Result{
		Auth:                  prepared.Auth,
		SelectedAuthProvider:  prepared.SelectedAuthProvider,
		AuthProviders:         prepared.AuthProviders,
		AuthorizationProvider: prepared.AuthorizationProvider,
		Services:              prepared.Services,
		ExtraIndexedDBs:       prepared.ExtraIndexedDBs,
		S3:                    prepared.Deps.S3,
		ExtraS3s:              prepared.ExtraS3s,
		ExtraWorkflows:        extraWorkflows,
		ExtraAgents:           extraAgents,
		Providers:             providers,
		WorkflowControl:       prepared.Deps.WorkflowRuntime,
		AgentControl:          prepared.Deps.AgentRuntime,
		AgentManager:          prepared.Deps.AgentManager,
		ProvidersReady:        providersReady,
		Authorizer:            authz,
		ConnectionAuth:        connAuthResolver,
		ManualConnectionAuth:  manualConnAuthResolver,
		Invoker:               sharedInvoker,
		PluginInvoker:         pluginInvoker,
		CapabilityLister:      sharedInvoker,
		AuditSink:             audit,
		SecretManager:         prepared.SecretManager,
		Telemetry:             prepared.Telemetry,
		PluginRuntimes:        prepared.pluginRuntimeRegistry,
		ProviderDevSessions:   providerDevSessions,
		PublicHostServices:    publicHostServices,
		pluginRuntimeRegistry: prepared.pluginRuntimeRegistry,
		auditClose:            auditClose,
	}, nil
}

func buildWorkflows(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreworkflow.Provider, error) {
	var extraWorkflows []coreworkflow.Provider
	for name, entry := range cfg.Providers.Workflow {
		if entry == nil {
			continue
		}
		value, err := buildWorkflow(ctx, name, entry, factories, deps)
		if err != nil {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.FailProvider(name, err)
				deps.WorkflowRuntime.FailPendingProviders(err)
			}
			_ = closeWorkflows(extraWorkflows...)
			return nil, fmt.Errorf("bootstrap: workflow from resource %q: %w", name, err)
		}
		if deps.WorkflowRuntime != nil {
			deps.WorkflowRuntime.PublishProvider(name, value)
		}
		extraWorkflows = append(extraWorkflows, value)
	}
	return extraWorkflows, nil
}

func buildAgents(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreagent.Provider, error) {
	var extraAgents []coreagent.Provider
	for name, entry := range cfg.Providers.Agent {
		if entry == nil {
			continue
		}
		value, err := buildAgent(ctx, name, entry, factories, deps)
		if err != nil {
			if deps.AgentRuntime != nil {
				deps.AgentRuntime.FailProvider(name)
			}
			_ = closeAgents(extraAgents...)
			return nil, fmt.Errorf("bootstrap: agent from resource %q: %w", name, err)
		}
		if deps.AgentRuntime != nil {
			deps.AgentRuntime.PublishProvider(name, value)
		}
		extraAgents = append(extraAgents, value)
	}
	return extraAgents, nil
}

func agentPluginInvokes(cfg *config.Config) map[string][]invocation.PluginInvocationDependency {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	out := make(map[string][]invocation.PluginInvocationDependency, len(cfg.Plugins))
	for pluginName, entry := range cfg.Plugins {
		if entry == nil || len(entry.Invokes) == 0 {
			continue
		}
		out[pluginName] = pluginInvocationDependencies(entry.Invokes)
	}
	return out
}

func pluginInvocationDependencies(deps []config.PluginInvocationDependency) []invocation.PluginInvocationDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]invocation.PluginInvocationDependency, 0, len(deps))
	for _, dep := range deps {
		out = append(out, invocation.PluginInvocationDependency{
			Plugin:         dep.Plugin,
			Operation:      dep.Operation,
			Surface:        dep.Surface,
			CredentialMode: core.ConnectionMode(dep.CredentialMode),
		})
	}
	return out
}

func buildTelemetry(cfg *config.Config, factories *FactoryRegistry) (core.TelemetryProvider, error) {
	_, tel, err := cfg.SelectedTelemetryProvider()
	if err != nil {
		return nil, err
	}
	if tel != nil && !tel.Source.IsBuiltin() {
		return nil, fmt.Errorf("bootstrap: provider-based telemetry providers are not yet supported")
	}
	builtin := ""
	var configNode yaml.Node
	if tel != nil {
		builtin = tel.Source.Builtin
		configNode = tel.Config
	}
	factory, ok := factories.Telemetry[builtin]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown telemetry provider %q", builtin)
	}
	tp, err := factory(configNode)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: telemetry provider %q: %w", builtin, err)
	}
	return tp, nil
}

func buildAuditSink(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
	_, audit, err := cfg.SelectedAuditProvider()
	if err != nil {
		return nil, nil, err
	}
	if audit != nil && !audit.Source.IsBuiltin() {
		return nil, nil, fmt.Errorf("bootstrap: provider-based audit providers are not yet supported")
	}
	builtin := ""
	if audit != nil {
		builtin = audit.Source.Builtin
	}
	if factories.Audit == nil {
		switch builtin {
		case "", "inherit":
			return invocation.NewLoggerAuditSink(telemetry.Logger()), nil, nil
		default:
			return nil, nil, fmt.Errorf("bootstrap: unknown audit provider %q", builtin)
		}
	}
	if audit == nil {
		audit = &config.ProviderEntry{}
	}
	sink, closeFn, err := factories.Audit(ctx, *audit, telemetry)
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap: audit provider %q: %w", builtin, err)
	}
	return sink, closeFn, nil
}

func buildRuntimeSecretManager(cfg *config.Config, factories *FactoryRegistry) (core.SecretManager, error) {
	name, secrets, err := cfg.SelectedSecretsProvider()
	if err != nil {
		return nil, err
	}
	return buildNamedSecretManager(name, secrets, factories)
}

func buildNamedSecretManager(name string, secrets *config.ProviderEntry, factories *FactoryRegistry) (core.SecretManager, error) {
	logicalName := name
	if logicalName == "" {
		logicalName = "secrets"
	}

	if secrets != nil && (secrets.HasRemoteSource() || secrets.HasLocalSource() || secrets.HasLocalReleaseSource()) {
		factory, ok := factories.Secrets["provider"]
		if !ok {
			return nil, fmt.Errorf("bootstrap: secrets provider factory is not registered")
		}
		node := secrets.Config
		if !config.IsComponentRuntimeConfigNode(node) {
			var err error
			node, err = config.BuildComponentRuntimeConfigNode(logicalName, "secrets", secrets, secrets.Config)
			if err != nil {
				return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
			}
		}
		sm, err := factory(node)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
		}
		return sm, nil
	}

	builtinName := ""
	var configNode yaml.Node
	if secrets != nil {
		builtinName = secrets.Source.Builtin
		configNode = secrets.Config
		if builtinName == "" {
			return nil, fmt.Errorf("bootstrap: secrets provider %q has no source", logicalName)
		}
	}
	if builtinName == "" {
		builtinName = "env"
	}
	factory, ok := factories.Secrets[builtinName]
	if !ok {
		if secrets != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q references unknown builtin %q", logicalName, builtinName)
		}
		return nil, fmt.Errorf("bootstrap: unknown secrets provider %q", builtinName)
	}
	sm, err := factory(configNode)
	if err != nil {
		if secrets != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
		}
		return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", builtinName, err)
	}
	return sm, nil
}

func buildExternalCredentialsProvider(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.ExternalCredentialProvider, error) {
	name, entry, err := cfg.SelectedExternalCredentialsProvider()
	if err != nil {
		return nil, err
	}
	if entry == nil {
		name = config.DefaultProviderInstance
		entry = defaultExternalCredentialsProviderEntry()
	}
	return buildNamedExternalCredentialsProvider(ctx, name, entry, factories, deps)
}

func buildNamedExternalCredentialsProvider(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (core.ExternalCredentialProvider, error) {
	logicalName := strings.TrimSpace(name)
	if logicalName == "" {
		logicalName = "external-credentials"
	}
	if entry == nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q is not configured", logicalName)
	}
	if factories.ExternalCredentials == nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider factory is not registered")
	}
	node, err := buildExternalCredentialsRuntimeConfigNode(logicalName, entry, deps.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
	}
	if !config.IsComponentRuntimeConfigNode(node) {
		node, err = config.BuildComponentRuntimeConfigNode(logicalName, providermanifestv1.KindExternalCredentials, entry, node)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
		}
	}
	hostServices, err := buildExternalCredentialsHostServices(logicalName, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
	}
	provider, err := factories.ExternalCredentials(ctx, logicalName, node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
	}
	if core.ExternalCredentialProviderMissing(provider) {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q returned nil", logicalName)
	}
	return observability.InstrumentExternalCredentialProvider(logicalName, provider), nil
}

func defaultExternalCredentialsProviderEntry() *config.ProviderEntry {
	return &config.ProviderEntry{
		Default: true,
		Source:  config.DefaultProviderSource(config.DefaultExternalCredentialsProvider, config.DefaultExternalCredentialsVersion),
	}
}

func buildExternalCredentialsRuntimeConfigNode(name string, entry *config.ProviderEntry, encryptionKey []byte) (yaml.Node, error) {
	if entry == nil {
		return yaml.Node{}, fmt.Errorf("external credentials provider %q is required", name)
	}
	cfg, err := config.NodeToMap(entry.Config)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("decode config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	if _, ok := cfg["encryptionKey"]; !ok {
		if len(encryptionKey) == 0 {
			return yaml.Node{}, fmt.Errorf("config.encryptionKey is required")
		}
		cfg["encryptionKey"] = hex.EncodeToString(encryptionKey)
	}
	return mapToYAMLNode(cfg)
}

func buildExternalCredentialsHostServices(name string, deps Deps) ([]runtimehost.HostService, error) {
	if len(deps.IndexedDBs) == 0 || deps.SelectedIndexedDBName == "" {
		return nil, fmt.Errorf("indexeddb host services are not available")
	}
	hostServices := make([]runtimehost.HostService, 0, len(deps.IndexedDBs)+1)
	if ds := deps.IndexedDBs[deps.SelectedIndexedDBName]; ds != nil {
		hostServices = append(hostServices, externalCredentialsIndexedDBHostService(indexeddbservice.DefaultSocketEnv, name, ds))
	}
	for _, indexedDBName := range slices.Sorted(maps.Keys(deps.IndexedDBs)) {
		ds := deps.IndexedDBs[indexedDBName]
		if ds == nil {
			continue
		}
		hostServices = append(hostServices, externalCredentialsIndexedDBHostService(indexeddbservice.SocketEnv(indexedDBName), name, ds))
	}
	if len(hostServices) == 0 {
		return nil, fmt.Errorf("indexeddb %q is not available", deps.SelectedIndexedDBName)
	}
	return hostServices, nil
}

func externalCredentialsIndexedDBHostService(envVar, providerName string, ds indexeddb.IndexedDB) runtimehost.HostService {
	return runtimehost.HostService{
		Name:   "indexeddb",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, providerName, indexeddbservice.ServerOptions{
				AllowedStores: []string{"external_credentials", "external_credentials_v2"},
			}))
		},
	}
}

func closeAuth(provider core.AuthenticationProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeAuthProviders(providers map[string]core.AuthenticationProvider) error {
	if len(providers) == 0 {
		return nil
	}
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		errs = append(errs, closeAuth(provider))
	}
	return errors.Join(errs...)
}

func closeAuthorizer(authorizer authorization.RuntimeAuthorizer) error {
	if authorizer == nil {
		return nil
	}
	return authorizer.Close()
}

func closeAuthorizationProvider(provider core.AuthorizationProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeExternalCredentialProviderCandidate(services *coredata.Services) error {
	if services == nil || core.ExternalCredentialProviderMissing(services.ExternalCredentials) {
		return nil
	}
	return closeExternalCredentialProvider(services.ExternalCredentials)
}

func closeExternalCredentialProvider(provider core.ExternalCredentialProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeSecretManager(sm core.SecretManager) error {
	closer, ok := sm.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func buildAuthProviders(cfg *config.Config, factories *FactoryRegistry, deps Deps) (string, map[string]core.AuthenticationProvider, error) {
	selectedName, _, err := cfg.SelectedAuthenticationProvider()
	if err != nil {
		return "", nil, err
	}
	if len(cfg.Providers.Authentication) == 0 {
		return selectedName, nil, nil
	}
	if factories.Auth == nil {
		return "", nil, fmt.Errorf("bootstrap: authentication factory is not registered")
	}
	providers := make(map[string]core.AuthenticationProvider, len(cfg.Providers.Authentication))
	for name, authEntry := range cfg.Providers.Authentication {
		if authEntry == nil {
			continue
		}
		auth, err := buildNamedAuthProvider(name, authEntry, factories, deps)
		if err != nil {
			_ = closeAuthProviders(providers)
			return "", nil, err
		}
		providers[name] = auth
	}
	return selectedName, providers, nil
}

func buildNamedAuthProvider(name string, authEntry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (core.AuthenticationProvider, error) {
	if authEntry == nil {
		return nil, nil
	}
	node := authEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "authentication", authEntry, authEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: authentication provider %q: %w", name, err)
		}
	}
	auth, err := factories.Auth(node, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: authentication provider %q: %w", name, err)
	}
	return auth, nil
}

func buildAuthorization(cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.AuthorizationProvider, error) {
	authzName, authzEntry, err := cfg.SelectedAuthorizationProvider()
	if err != nil {
		return nil, err
	}
	if authzEntry == nil {
		return nil, nil
	}
	if factories.Authorization == nil {
		return nil, fmt.Errorf("bootstrap: authorization factory is not registered")
	}
	node := authzEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("authorization", "authorization", authzEntry, authzEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: authorization provider: %w", err)
		}
	}
	hostServices := buildHostIndexedDBHostServices(deps.SelectedIndexedDBName, deps.IndexedDBs)
	provider, err := factories.Authorization(node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: authorization provider: %w", err)
	}
	return observability.InstrumentAuthorizationProvider(authzName, provider), nil
}

func buildIndexedDB(entry *config.ProviderEntry, factories *FactoryRegistry) (indexeddb.IndexedDB, error) {
	if entry == nil {
		return nil, fmt.Errorf("datastore provider is required")
	}
	if factories.IndexedDB == nil {
		return nil, fmt.Errorf("datastore factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("indexeddb", "indexeddb", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("datastore provider: %w", err)
		}
	}
	ds, err := factories.IndexedDB(node)
	if err != nil {
		return nil, fmt.Errorf("datastore provider: %w", err)
	}
	return ds, nil
}

func buildHostIndexedDBHostServices(selectedName string, indexeddbs map[string]indexeddb.IndexedDB) []runtimehost.HostService {
	if len(indexeddbs) == 0 {
		return nil
	}

	hostServices := make([]runtimehost.HostService, 0, len(indexeddbs)+1)
	if selected := indexeddbs[selectedName]; strings.TrimSpace(selectedName) != "" && selected != nil {
		hostServices = append(hostServices, indexedDBHostService(indexeddbservice.DefaultSocketEnv, selectedName, selected))
	}

	for _, name := range slices.Sorted(maps.Keys(indexeddbs)) {
		ds := indexeddbs[name]
		if ds == nil {
			continue
		}
		hostServices = append(hostServices, indexedDBHostService(indexeddbservice.SocketEnv(name), name, ds))
	}
	return hostServices
}

func indexedDBHostService(envVar, name string, ds indexeddb.IndexedDB) runtimehost.HostService {
	return runtimehost.HostService{
		Name:   "indexeddb",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, name, indexeddbservice.ServerOptions{}))
		},
	}
}

func buildCache(entry *config.ProviderEntry, factories *FactoryRegistry) (corecache.Cache, error) {
	if entry == nil {
		return nil, fmt.Errorf("cache provider is required")
	}
	if factories.Cache == nil {
		return nil, fmt.Errorf("cache factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("cache", "cache", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("cache provider: %w", err)
		}
	}
	value, err := factories.Cache(node)
	if err != nil {
		return nil, fmt.Errorf("cache provider: %w", err)
	}
	return value, nil
}

func buildS3(name string, entry *config.ProviderEntry, factories *FactoryRegistry) (s3store.Client, error) {
	if entry == nil {
		return nil, fmt.Errorf("s3 provider is required")
	}
	if factories.S3 == nil {
		return nil, fmt.Errorf("s3 factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "s3", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("s3 provider: %w", err)
		}
	}
	client, err := factories.S3(node)
	if err != nil {
		return nil, fmt.Errorf("s3 provider: %w", err)
	}
	return client, nil
}

func buildWorkflow(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (coreworkflow.Provider, error) {
	if entry == nil {
		return nil, fmt.Errorf("workflow provider is required")
	}
	if factories.Workflow == nil {
		return nil, fmt.Errorf("workflow factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "workflow", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
	}
	hostServices := []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowHostServer(srv, workflowservice.NewHostServer(name, deps.WorkflowRuntime.Invoke))
		},
	}}
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	effectiveIndexedDB, err := config.ResolveEffectiveWorkflowIndexedDB(name, entry, deps.IndexedDBDefs)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	if effectiveIndexedDB.Enabled {
		indexedDBHostServices, indexedDBCleanup, err := buildWorkflowIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
		hostServices = append(hostServices, indexedDBHostServices...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	provider, err := factories.Workflow(ctx, name, node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	if cleanup != nil {
		if executionRefs, ok := provider.(coreworkflow.ExecutionReferenceStore); ok {
			provider = &workflowProviderWithExecutionReferencesAndCleanup{
				Provider:                provider,
				ExecutionReferenceStore: executionRefs,
				cleanup:                 cleanup,
			}
		} else {
			provider = &workflowProviderWithCleanup{
				Provider: provider,
				cleanup:  cleanup,
			}
		}
		cleanup = nil
	}
	return provider, nil
}

func buildAgent(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (coreagent.Provider, error) {
	if entry == nil {
		return nil, fmt.Errorf("agent provider is required")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "agent", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("agent provider: %w", err)
		}
	}
	hostServices := []runtimehost.HostService{{
		Name:   "agent_host",
		EnvVar: agentservice.DefaultHostSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAgentHostServer(srv, agentservice.NewHostServerWithConnections(name, deps.AgentRuntime.ListTools, deps.AgentRuntime.ExecuteTool, deps.AgentRuntime.ResolveConnection))
		},
	}}
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	effectiveIndexedDB, err := config.ResolveEffectiveAgentIndexedDB(name, entry, deps.IndexedDBDefs)
	if err != nil {
		return nil, fmt.Errorf("agent provider: %w", err)
	}
	if effectiveIndexedDB.Enabled {
		indexedDBHostServices, indexedDBCleanup, err := buildAgentIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return nil, fmt.Errorf("agent provider: %w", err)
		}
		hostServices = append(hostServices, indexedDBHostServices...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	var (
		provider    coreagent.Provider
		providerErr error
	)
	if entry.UsesHostedExecution() {
		provider, providerErr = buildHostedAgentProvider(ctx, name, entry, node, hostServices, deps)
	} else {
		if factories.Agent == nil {
			return nil, fmt.Errorf("agent factory is not registered")
		}
		provider, providerErr = factories.Agent(ctx, name, node, hostServices, deps)
	}
	if providerErr != nil {
		return nil, fmt.Errorf("agent provider: %w", providerErr)
	}
	provider = observability.InstrumentAgentProvider(name, provider)
	tracked := &agentProviderWithTracking{
		delegate:     provider,
		providerName: name,
	}
	if cleanup != nil {
		provider := &agentProviderWithCleanup{
			Provider: tracked,
			cleanup:  cleanup,
		}
		cleanup = nil
		return provider, nil
	}
	return tracked, nil
}
