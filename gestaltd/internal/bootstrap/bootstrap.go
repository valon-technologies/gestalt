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

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/agents/agenttoolid"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"gopkg.in/yaml.v3"
)

// OAuthHandler covers every OAuth method needed by the server (start, exchange,
// refresh) and the broker (refresh). mcpoauth.Handler satisfies this directly;
// use WrapUpstreamHandler to adapt an oauth.UpstreamHandler.
type OAuthHandler interface {
	AuthorizationURL(state string, scopes []string) string
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
	StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	ExchangeCode(ctx context.Context, code string) (*core.OAuthTokenResponse, error)
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.OAuthTokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.OAuthTokenResponse, error)
	AuthorizationBaseURL() string
	TokenURL() string
}

type ManualTokenExchanger interface {
	ExchangeCredentials(ctx context.Context, credentialJSON string) (*core.OAuthTokenResponse, error)
	ExchangeCredentialsWithURL(ctx context.Context, credentialJSON, tokenURL string) (*core.OAuthTokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.OAuthTokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.OAuthTokenResponse, error)
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

func (a *upstreamHandlerAdapter) ExchangeCode(ctx context.Context, code string) (*core.OAuthTokenResponse, error) {
	return a.UpstreamHandler.ExchangeCode(ctx, code)
}

func (a *upstreamHandlerAdapter) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.OAuthTokenResponse, error) {
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
	Caches                map[string]corecache.Cache
	CacheDefs             map[string]*config.ProviderEntry
	CacheFactory          CacheFactory
	S3                    map[string]s3sdk.S3
	Authentication        core.IdentityProvider
	Authorization         core.AuthorizationProvider
	WorkflowRuntime       *workflowRuntime
	AgentRuntime          *agentRuntime
	AgentTurnScopes       *agentturnscope.Store
	AgentToolIDs          *agenttoolid.Codec
	WorkflowManager       workflowmanager.Service
	AgentManager          agentmanager.Service
	Egress                EgressDeps
	AppInvocation         invocation.Invoker
	Runtime               runtimeprovider.Provider
	RuntimeRegistry       *runtimeRegistry
	PublicHostServices    *runtimehost.PublicHostServiceRegistry
	HostServiceTLSCAFile  string
	HostServiceTLSCAPEM   string
	Telemetry             core.TelemetryProvider
	ProviderTransport     providergateway.Transport
	CallerTokenPublicKey  string

	hostedAgentPoolClock hostedAgentPoolClock
}

type AuthFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (core.IdentityProvider, error)
type AuthorizationFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (providerdrivers.AuthorizationBuildResult, error)
type ExternalCredentialFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (core.ExternalCredentialProvider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type IndexedDBFactory func(node yaml.Node) (indexeddb.IndexedDB, error)
type CacheFactory func(node yaml.Node) (corecache.Cache, error)
type S3Factory func(node yaml.Node) (s3sdk.S3, error)
type WorkflowFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreworkflow.Provider, error)
type AgentFactory func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreagent.Provider, error)
type RuntimeFactory func(ctx context.Context, name string, entry *config.RuntimeProviderEntry, deps Deps) (runtimeprovider.Provider, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)
type AuditFactory func(ctx context.Context, cfg config.ProviderEntry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error)

const (
	callerTokenPrivateKeySecretName = "gestaltd-caller-token-ed25519-private-key"
	callerTokenPublicKeySecretName  = "gestaltd-caller-token-ed25519-public-key"
)

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
		Runtime:   buildExecutableRuntime,
		Telemetry: make(map[string]TelemetryFactory),
	}
}

type Result struct {
	Auth                 core.IdentityProvider
	SelectedAuthProvider string
	AuthProviders        map[string]core.IdentityProvider
	Authorization        map[string]core.AuthorizationProvider
	Services             *coredata.Services
	ExtraIndexedDBs      []indexeddb.IndexedDB
	ExtraCaches          []corecache.Cache
	S3                   map[string]s3sdk.S3
	ExtraS3s             []s3sdk.S3
	ExtraWorkflows       []coreworkflow.Provider
	ExtraAgents          []coreagent.Provider
	Providers            *registry.ProviderMap[core.Provider]
	WorkflowControl      WorkflowControl
	AgentControl         AgentControl
	AgentManager         agentmanager.Service
	ProvidersReady       <-chan struct{}
	ConnectionAuth       func() map[string]map[string]OAuthHandler
	ManualConnectionAuth func() map[string]map[string]ManualTokenExchanger
	Invoker              invocation.Invoker
	AppInvocation        invocation.Invoker
	CapabilityLister     invocation.CapabilityLister
	AuditSink            core.AuditSink
	SecretManager        core.SecretManager
	Telemetry            core.TelemetryProvider
	Runtimes             RuntimeInspector
	PublicHostServices   *runtimehost.PublicHostServiceRegistry
	CallerTokenIssuer    *providergateway.CallerTokenIssuer

	runtimeRegistry                     *runtimeRegistry
	workflowConfigReconcileTasks        []workflowConfigReconcileTask
	workflowConfigReconcileTasksStarted bool
	auditClose                          func(context.Context) error
	mu                                  sync.Mutex
	closed                              bool
}

type workflowConfigReconcileTask struct {
	name      string
	reconcile func(context.Context) error
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

func (r *Result) StartWorkflowConfigReconciliation(ctx context.Context) {
	if r == nil {
		return
	}

	r.mu.Lock()
	if r.closed || r.workflowConfigReconcileTasksStarted || len(r.workflowConfigReconcileTasks) == 0 {
		r.mu.Unlock()
		return
	}
	r.workflowConfigReconcileTasksStarted = true
	tasks := append([]workflowConfigReconcileTask(nil), r.workflowConfigReconcileTasks...)
	r.mu.Unlock()

	for _, task := range tasks {
		task := task
		go runWorkflowConfigReconcileTask(ctx, task)
	}
}

func runWorkflowConfigReconcileTask(ctx context.Context, task workflowConfigReconcileTask) {
	if task.reconcile == nil {
		return
	}
	taskName := strings.TrimSpace(task.name)
	if taskName == "" {
		taskName = "workflow config"
	}
	interval := 5 * time.Second
	for {
		if err := task.reconcile(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "workflow config reconciliation failed; will retry", "task", taskName, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				continue
			}
		}
		slog.InfoContext(ctx, "workflow config reconciled", "task", taskName)
		return
	}
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
	authorizationCloseErr := closeAuthorizationProviders(r.Authorization)
	externalCredentialsCloseErr := closeExternalCredentialProviderCandidate(r.Services)
	errs = append(errs,
		authCloseErr,
		authorizationCloseErr,
		externalCredentialsCloseErr,
		CloseProviders(r.Providers),
		r.Services.Close(),
		closeIndexedDBs(r.ExtraIndexedDBs...),
		closeCaches(r.ExtraCaches...),
		closeS3s(r.ExtraS3s...),
		closeWorkflows(r.ExtraWorkflows...),
		closeAgents(r.ExtraAgents...),
		closeSecretManager(r.SecretManager),
		closeRuntimeRegistry(r.runtimeRegistry),
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

func closeS3s(clients ...s3sdk.S3) error {
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

func (p *workflowProviderWithCleanup) WaitRuntimeWorkersReady(ctx context.Context) error {
	if p == nil || p.Provider == nil {
		return nil
	}
	if workerProvider, ok := p.Provider.(runtimeWorkerWorkflowProvider); ok {
		return workerProvider.WaitRuntimeWorkersReady(ctx)
	}
	return nil
}

type agentProviderWithTracking struct {
	delegate     coreagent.Provider
	providerName string
}

func (p *agentProviderWithTracking) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.CreateSession(ctx, req)
}

func (p *agentProviderWithTracking) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetSession(ctx, req)
}

func (p *agentProviderWithTracking) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListSessions(ctx, req)
}

func (p *agentProviderWithTracking) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.UpdateSession(ctx, req)
}

func (p *agentProviderWithTracking) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	turn, err := p.delegate.CreateTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	requestedID := strings.TrimSpace(req.GetTurnId())
	if requestedID != "" && turn != nil {
		actualID := strings.TrimSpace(turn.ID)
		if actualID != "" && actualID != requestedID && strings.TrimSpace(req.GetIdempotencyKey()) == "" {
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

func (p *agentProviderWithTracking) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetTurn(ctx, req)
}

func (p *agentProviderWithTracking) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListTurns(ctx, req)
}

func (p *agentProviderWithTracking) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.CancelTurn(ctx, req)
}

func (p *agentProviderWithTracking) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListTurnEvents(ctx, req)
}

func (p *agentProviderWithTracking) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.GetInteraction(ctx, req)
}

func (p *agentProviderWithTracking) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ListInteractions(ctx, req)
}

func (p *agentProviderWithTracking) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	if p == nil || p.delegate == nil {
		return nil, fmt.Errorf("agent provider is not configured")
	}
	return p.delegate.ResolveInteraction(ctx, req)
}

func (p *agentProviderWithTracking) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
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
	_, cancelErr := p.delegate.CancelTurn(context.Background(), &proto.CancelAgentProviderTurnRequest{
		ProviderName: strings.TrimSpace(p.providerName),
		TurnId:       turnID,
		Reason:       strings.TrimSpace(reason),
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

func (p *agentProviderWithTracking) SupportsWorkspaceRequests() bool {
	if p == nil || p.delegate == nil {
		return false
	}
	workspaceProvider, ok := p.delegate.(coreagent.WorkspaceProvider)
	return ok && workspaceProvider.SupportsWorkspaceRequests()
}

type preparedCore struct {
	Auth                 core.IdentityProvider
	SelectedAuthProvider string
	AuthProviders        map[string]core.IdentityProvider
	Authorization        map[string]core.AuthorizationProvider
	Services             *coredata.Services
	ExtraIndexedDBs      []indexeddb.IndexedDB
	ExtraCaches          []corecache.Cache
	ExtraS3s             []s3sdk.S3
	SecretManager        core.SecretManager
	Telemetry            core.TelemetryProvider
	Deps                 Deps
	AppInvocation        *lazyInvoker
	WorkflowManager      *lazyWorkflowManager
	AgentManager         *lazyAgentManager
	PublicHostServices   *runtimehost.PublicHostServiceRegistry
	CallerTokenIssuer    *providergateway.CallerTokenIssuer

	runtimeRegistry *runtimeRegistry
}

type configSecretManagers struct {
	ctx       context.Context
	cfg       *config.Config
	factories *FactoryRegistry
	managers  map[string]core.SecretManager
}

func newConfigSecretManagersForReferences(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, referenced map[string]struct{}) *configSecretManagers {
	if len(referenced) == 0 {
		return nil
	}
	return &configSecretManagers{
		ctx:       ctx,
		cfg:       cfg,
		factories: factories,
		managers:  make(map[string]core.SecretManager, len(referenced)),
	}
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
	return resolveConfigSecrets(ctx, cfg, factories, config.ReferencedConfigSecretProviders, config.TransformConfigStringFields)
}

// ResolveSourceAuthSecrets resolves only provider source.auth.token structured
// secret refs. It is used by build-time artifact preparation, where source
// credentials may be needed to fetch provider packages but runtime secrets are
// intentionally left unresolved.
func ResolveSourceAuthSecrets(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) error {
	return resolveConfigSecrets(ctx, cfg, factories, config.ReferencedSourceAuthSecretProviders, config.TransformSourceAuthTokens)
}

func resolveConfigSecrets(
	ctx context.Context,
	cfg *config.Config,
	factories *FactoryRegistry,
	collectReferences func(*config.Config) (map[string]struct{}, error),
	transformFields func(*config.Config, config.ConfigStringTransformer) error,
) error {
	if err := config.CanonicalizeStructure(cfg); err != nil {
		return err
	}
	referenced, err := collectReferences(cfg)
	if err != nil {
		return err
	}
	resolver := newConfigSecretManagersForReferences(ctx, cfg, factories, referenced)
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
	if err := transformFields(cfg, resolveValue); err != nil {
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
	agentToolIDs, err := agenttoolid.NewCodec(encKey)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: agent tool ids: %w", err)
	}
	agentTurnScopes := agentturnscope.NewStore()
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
		AgentTurnScopes:      agentTurnScopes,
		AgentToolIDs:         agentToolIDs,
		HostServiceTLSCAFile: hostServiceTLSCAFile,
		HostServiceTLSCAPEM:  hostServiceTLSCAPEM,
	}
	pluginInvoker := newLazyInvoker()
	workflowManager := newLazyWorkflowManager()
	agentManager := newLazyAgentManager()
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	deps.AppInvocation = pluginInvoker
	deps.WorkflowManager = workflowManager
	deps.AgentManager = agentManager
	deps.PublicHostServices = publicHostServices

	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		return nil, err
	}
	workflowRuntime.InitProviderPlaceholders(cfg.Providers.Workflow)
	deps.WorkflowRuntime = workflowRuntime
	agentRuntime, err := newAgentRuntime(cfg, workflowRuntime.StartupWaitTracker())
	if err != nil {
		return nil, err
	}
	deps.AgentRuntime = agentRuntime

	selectedIndexedDBName, def, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}
	if selectedIndexedDBName == "" || def == nil {
		return nil, fmt.Errorf("bootstrap: indexeddb resource name is required")
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
	indexedDBs := map[string]indexeddb.IndexedDB{selectedIndexedDBName: store}
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
		indexedDBs[name] = ds
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

	hostCaches := make(map[string]corecache.Cache, len(cfg.Providers.Cache))
	var extraCaches []corecache.Cache
	for name, entry := range cfg.Providers.Cache {
		if entry == nil {
			continue
		}
		value, err := buildCache(entry, factories)
		if err != nil {
			_ = closeCaches(extraCaches...)
			return nil, fmt.Errorf("bootstrap: cache from resource %q: %w", name, err)
		}
		hostCaches[name] = value
		extraCaches = append(extraCaches, value)
	}
	closeExtraCaches := true
	defer func() {
		if closeExtraCaches {
			_ = closeCaches(extraCaches...)
		}
	}()

	hostS3s := make(map[string]s3sdk.S3, len(cfg.Providers.S3))
	var extraS3s []s3sdk.S3
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
	deps.IndexedDBs = indexedDBs
	deps.SelectedIndexedDBName = selectedIndexedDBName
	deps.Caches = hostCaches
	deps.S3 = hostS3s

	selectedAuthName, authProviders, err := buildAuthProviders(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	auth := authProviders[selectedAuthName]
	deps.Authentication = auth

	callerTokenPrivateKey, err := resolveCallerTokenPrivateKey(ctx, sm)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	callerTokenIssuer, err := providergateway.NewCallerTokenIssuer(callerTokenPrivateKey)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, fmt.Errorf("bootstrap: caller token private key: %w", err)
	}
	providerTransport := providergateway.DirectTransport{}
	deps.ProviderTransport = providerTransport
	callerTokenPublicKey, err := resolveCallerTokenPublicKey(ctx, sm)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	deps.CallerTokenPublicKey = callerTokenPublicKey
	authorizationProviders, err := buildAuthorizationProviders(ctx, cfg, factories, deps)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	closeAuthorizationOnError := true
	defer func() {
		if closeAuthorizationOnError {
			_ = closeAuthorizationProviders(authorizationProviders.Guarded)
		}
	}()
	if err := bootstrapAuthorizationProviderState(ctx, cfg, authorizationProviders.Raw); err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	_, authorizationProvider, err := selectedAuthorizationProviderInstance(cfg, authorizationProviders.Guarded)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	if authorizationProvider != nil {
		deps.Authorization = authorizationProvider
	}
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

	runtimeRegistry := newRuntimeRegistry(cfg, factories.Runtime, deps)
	deps.RuntimeRegistry = runtimeRegistry

	closeSM = false
	shutdownTelemetry = false
	closeSvc = false
	closeExtraStores = false
	closeExtraCaches = false
	closeExtraS3s = false
	closeAuthorizationOnError = false
	closeExternalCredentialsOnError = false
	return &preparedCore{
		Auth:                 auth,
		SelectedAuthProvider: selectedAuthName,
		AuthProviders:        authProviders,
		Authorization:        authorizationProviders.Guarded,
		Services:             svc,
		ExtraIndexedDBs:      extraIndexedDBs,
		ExtraCaches:          extraCaches,
		ExtraS3s:             extraS3s,
		SecretManager:        sm,
		Telemetry:            tp,
		Deps:                 deps,
		AppInvocation:        pluginInvoker,
		WorkflowManager:      workflowManager,
		AgentManager:         agentManager,
		PublicHostServices:   publicHostServices,
		CallerTokenIssuer:    callerTokenIssuer,
		runtimeRegistry:      runtimeRegistry,
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
	authorizationCloseErr := closeAuthorizationProviders(p.Authorization)
	externalCredentialsCloseErr := closeExternalCredentialProviderCandidate(p.Services)
	errs = append(errs,
		authCloseErr,
		authorizationCloseErr,
		externalCredentialsCloseErr,
		p.Services.Close(),
		closeIndexedDBs(p.ExtraIndexedDBs...),
		closeCaches(p.ExtraCaches...),
		closeS3s(p.ExtraS3s...),
		closeSecretManager(p.SecretManager),
		closeRuntimeRegistry(p.runtimeRegistry),
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

	pluginInvoker := prepared.AppInvocation
	workflowManager := prepared.WorkflowManager
	agentManager := prepared.AgentManager
	workflowTools := newWorkflowSystemTools(workflowManager, prepared.Deps.WorkflowRuntime)
	publicHostServices := prepared.PublicHostServices

	providerBuilds, err := prepareProviderBuilds(cfg, factories, prepared.Deps)
	if err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	providers := providerBuilds.providers
	var (
		providersReady         <-chan struct{}
		connAuthResolver       func() map[string]map[string]OAuthHandler
		manualConnAuthResolver func() map[string]map[string]ManualTokenExchanger
	)
	closeProviders := true
	defer func() {
		if closeProviders {
			if providersReady != nil {
				<-providersReady
			}
			_ = CloseProviders(providers)
		}
	}()

	connMaps, err := BuildConnectionMaps(cfg)
	if err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	connRuntime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	if err := attachMCPOAuthRuntimeAuth(cfg, connRuntime, prepared.Deps); err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	if err := ValidateConnectionRuntimeCredentials(ctx, prepared.Services.ExternalCredentials, connRuntime); err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	_, authorizationProvider, err := selectedAuthorizationProviderInstance(cfg, prepared.Authorization)
	if err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	sharedInvoker := invocation.NewBroker(providers, prepared.Services.Users, prepared.Services.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionRuntime(connRuntime.Resolve),
		invocation.WithAuthorizationProvider(authorizationProvider),
	)
	audit, auditClose, err := buildAuditSink(ctx, cfg, factories, prepared.Telemetry)
	if err != nil {
		failPendingStartupProviders(prepared.Deps, err)
		return nil, err
	}
	closeAudit := true
	defer func() {
		if closeAudit && auditClose != nil {
			_ = auditClose(context.Background())
		}
	}()
	workflowManager.SetTarget(workflowmanager.New(workflowmanager.Config{
		Providers:         providers,
		Workflow:          prepared.Deps.WorkflowRuntime,
		Agent:             prepared.Deps.AgentRuntime,
		AgentManager:      agentManager,
		Invoker:           sharedInvoker,
		Audit:             audit,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.APIConnection,
		MCPConnection:     connMaps.MCPConnection,
	}))
	agentManager.SetTarget(agentmanager.New(agentmanager.Config{
		Providers:         providers,
		Agent:             prepared.Deps.AgentRuntime,
		WorkflowTools:     workflowTools,
		TurnScopes:        prepared.Deps.AgentTurnScopes,
		ToolIDs:           prepared.Deps.AgentToolIDs,
		Invoker:           sharedInvoker,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.APIConnection,
		MCPConnection:     connMaps.MCPConnection,
		AgentConnections:  agentConnectionBindings(cfg),
		SessionStart:      agentSessionStartConfigs(cfg),
	}))
	pluginInvoker.SetTarget(invocation.NewGuarded(sharedInvoker, nil, "app", audit, invocation.WithoutRateLimit()))
	// Build workflow/agent providers before app providers: they establish their
	// backend connection during build, which must not race concurrent app startup.
	extraWorkflows, extraAgents, err := buildWorkflowsAndAgents(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		_ = closeWorkflows(extraWorkflows...)
		_ = closeAgents(extraAgents...)
		return nil, err
	}
	closeWorkflowsOnError := true
	defer func() {
		if closeWorkflowsOnError {
			_ = closeWorkflows(extraWorkflows...)
		}
	}()
	closeAgentsOnError := true
	defer func() {
		if closeAgentsOnError {
			_ = closeAgents(extraAgents...)
		}
	}()
	noopBuilds, updateBuilds := providerBuilds.partition(appStartupCategory)

	// NOOP apps: block until all are ready before /ready is returned.
	noopReady, noopConnAuth, noopManualConnAuth, _ := noopBuilds.Start(ctx, prepared.Deps, buildProvider)
	select {
	case <-noopReady:
	case <-ctx.Done():
		return nil, fmt.Errorf("app provider startup interrupted: %w", ctx.Err())
	}
	slog.InfoContext(ctx, "all ready-blocking app providers loaded", "count", len(noopBuilds.pending))

	// UPDATE apps: start in background and complete after /ready.
	var updateConnAuth func() map[string]map[string]OAuthHandler
	var updateManualConnAuth func() map[string]map[string]ManualTokenExchanger
	providersReady, updateConnAuth, updateManualConnAuth, _ = updateBuilds.Start(ctx, prepared.Deps, buildProvider)

	connAuthResolver = func() map[string]map[string]OAuthHandler {
		a, b := noopConnAuth(), updateConnAuth()
		if len(b) == 0 {
			return a
		}
		merged := make(map[string]map[string]OAuthHandler, len(a)+len(b))
		for k, v := range a {
			merged[k] = v
		}
		for k, v := range b {
			merged[k] = v
		}
		return merged
	}
	manualConnAuthResolver = func() map[string]map[string]ManualTokenExchanger {
		a, b := noopManualConnAuth(), updateManualConnAuth()
		if len(b) == 0 {
			return a
		}
		merged := make(map[string]map[string]ManualTokenExchanger, len(a)+len(b))
		for k, v := range a {
			merged[k] = v
		}
		for k, v := range b {
			merged[k] = v
		}
		return merged
	}

	reconcileWorkflowConfig := func(ctx context.Context, includeProvider workflowConfigProviderFilter) error {
		if err := reconcileWorkflowConfigDefinitions(ctx, cfg, prepared.Deps.WorkflowRuntime, includeProvider); err != nil {
			return err
		}
		return nil
	}
	var deferredWorkflowConfigReconcileTasks []workflowConfigReconcileTask
	runtimePlacedWorkflowProviders := runtimePlacedWorkflowProviderNames(cfg)
	if len(runtimePlacedWorkflowProviders) > 0 {
		localWorkflowProviders := func(providerName string) bool {
			_, runtimePlaced := runtimePlacedWorkflowProviders[strings.TrimSpace(providerName)]
			return !runtimePlaced
		}
		if err := reconcileWorkflowConfig(ctx, localWorkflowProviders); err != nil {
			return nil, err
		}
		deferredWorkflowConfigReconcileTasks = runtimeWorkflowConfigReconcileTasks(prepared.Deps.WorkflowRuntime, runtimePlacedWorkflowProviders, reconcileWorkflowConfig)
	} else if err := reconcileWorkflowConfig(ctx, nil); err != nil {
		return nil, err
	}

	closeProviders = false
	closeCore = false
	closeAudit = false
	closeWorkflowsOnError = false
	closeAgentsOnError = false
	return &Result{
		Auth:                         prepared.Auth,
		SelectedAuthProvider:         prepared.SelectedAuthProvider,
		AuthProviders:                prepared.AuthProviders,
		Authorization:                prepared.Authorization,
		Services:                     prepared.Services,
		ExtraIndexedDBs:              prepared.ExtraIndexedDBs,
		ExtraCaches:                  prepared.ExtraCaches,
		S3:                           prepared.Deps.S3,
		ExtraS3s:                     prepared.ExtraS3s,
		ExtraWorkflows:               extraWorkflows,
		ExtraAgents:                  extraAgents,
		Providers:                    providers,
		WorkflowControl:              prepared.Deps.WorkflowRuntime,
		AgentControl:                 prepared.Deps.AgentRuntime,
		AgentManager:                 prepared.Deps.AgentManager,
		ProvidersReady:               providersReady,
		ConnectionAuth:               connAuthResolver,
		ManualConnectionAuth:         manualConnAuthResolver,
		Invoker:                      sharedInvoker,
		AppInvocation:                pluginInvoker,
		CapabilityLister:             sharedInvoker,
		AuditSink:                    audit,
		SecretManager:                prepared.SecretManager,
		Telemetry:                    prepared.Telemetry,
		Runtimes:                     prepared.runtimeRegistry,
		PublicHostServices:           publicHostServices,
		CallerTokenIssuer:            prepared.CallerTokenIssuer,
		runtimeRegistry:              prepared.runtimeRegistry,
		workflowConfigReconcileTasks: deferredWorkflowConfigReconcileTasks,
		auditClose:                   auditClose,
	}, nil
}

type runtimeWorkerWorkflowProvider interface {
	WaitRuntimeWorkersReady(context.Context) error
}

func runtimePlacedWorkflowProviderNames(cfg *config.Config) map[string]struct{} {
	providerNames := map[string]struct{}{}
	if cfg == nil {
		return providerNames
	}
	for name, entry := range cfg.Providers.Workflow {
		if entry != nil && entry.UsesRuntimePlacement() {
			providerNames[strings.TrimSpace(name)] = struct{}{}
		}
	}
	return providerNames
}

func runtimeWorkflowConfigReconcileTasks(runtime *workflowRuntime, providerNames map[string]struct{}, reconcile func(context.Context, workflowConfigProviderFilter) error) []workflowConfigReconcileTask {
	tasks := make([]workflowConfigReconcileTask, 0, len(providerNames))
	for _, providerName := range slices.Sorted(maps.Keys(providerNames)) {
		providerName := providerName
		tasks = append(tasks, workflowConfigReconcileTask{
			name: "workflow provider " + providerName,
			reconcile: func(ctx context.Context) error {
				if err := waitRuntimeWorkflowProviderReady(ctx, runtime, providerName); err != nil {
					return err
				}
				return reconcile(ctx, workflowConfigOnlyProvider(providerName))
			},
		})
	}
	return tasks
}

func workflowConfigOnlyProvider(providerName string) workflowConfigProviderFilter {
	return func(candidateName string) bool {
		return strings.TrimSpace(candidateName) == strings.TrimSpace(providerName)
	}
}

func waitRuntimeWorkflowProviderReady(ctx context.Context, runtime *workflowRuntime, providerName string) error {
	_, provider, err := runtime.ResolveProvider(ctx, providerName)
	if err != nil {
		return err
	}
	workerProvider, ok := provider.(runtimeWorkerWorkflowProvider)
	if !ok {
		return nil
	}
	if err := workerProvider.WaitRuntimeWorkersReady(ctx); err != nil {
		return err
	}
	return nil
}

type configuredProviderBuilds[T any] struct {
	providers      []T
	publishedNames []string
	err            error
}

func buildWorkflowsAndAgents(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreworkflow.Provider, []coreagent.Provider, error) {
	workflowCh := make(chan configuredProviderBuilds[coreworkflow.Provider], 1)
	agentCh := make(chan configuredProviderBuilds[coreagent.Provider], 1)
	go func() {
		providers, publishedNames, err := buildWorkflows(ctx, cfg, factories, deps)
		workflowCh <- configuredProviderBuilds[coreworkflow.Provider]{providers: providers, publishedNames: publishedNames, err: err}
	}()
	go func() {
		providers, publishedNames, err := buildAgents(ctx, cfg, factories, deps)
		agentCh <- configuredProviderBuilds[coreagent.Provider]{providers: providers, publishedNames: publishedNames, err: err}
	}()

	workflowResult := <-workflowCh
	agentResult := <-agentCh
	if err := errors.Join(workflowResult.err, agentResult.err); err != nil {
		if agentResult.err != nil && deps.WorkflowRuntime != nil {
			for _, name := range workflowResult.publishedNames {
				deps.WorkflowRuntime.UnpublishProvider(name)
			}
			err = errors.Join(err, closeWorkflows(workflowResult.providers...))
			workflowResult.providers = nil
		}
		if workflowResult.err != nil && deps.AgentRuntime != nil {
			for _, name := range agentResult.publishedNames {
				deps.AgentRuntime.UnpublishProvider(name)
			}
			err = errors.Join(err, closeAgents(agentResult.providers...))
			agentResult.providers = nil
		}
		return workflowResult.providers, agentResult.providers, err
	}
	return workflowResult.providers, agentResult.providers, nil
}

func failPendingStartupProviders(deps Deps, err error) {
	if deps.WorkflowRuntime != nil {
		deps.WorkflowRuntime.FailPendingProviders(err)
	}
	if deps.AgentRuntime != nil {
		deps.AgentRuntime.FailPendingProviders(err)
	}
}

func buildConfiguredProviders[T any](
	ctx context.Context,
	entries map[string]*config.ProviderEntry,
	build func(context.Context, string, *config.ProviderEntry) (T, error),
	publish func(string, T),
	failStartupProvider func(string, error),
	unpublishProvider func(string),
	failPending func(error),
	closeProviders func(...T) error,
	wrapErr func(string, error) error,
) ([]T, []string, error) {
	var pending []struct {
		name  string
		entry *config.ProviderEntry
	}
	for name, entry := range entries {
		if entry == nil {
			continue
		}
		pending = append(pending, struct {
			name  string
			entry *config.ProviderEntry
		}{name: name, entry: entry})
	}
	if len(pending) == 0 {
		return nil, nil, nil
	}
	type buildResult struct {
		name     string
		provider T
		err      error
	}
	results := make(chan buildResult, len(pending))
	for _, item := range pending {
		go func(name string, entry *config.ProviderEntry) {
			provider, err := build(ctx, name, entry)
			results <- buildResult{name: name, provider: provider, err: err}
		}(item.name, item.entry)
	}

	var providers []T
	var published []string
	var errs []error
	buildFailed := false
	for range pending {
		result := <-results
		if result.err != nil {
			if failStartupProvider != nil {
				failStartupProvider(result.name, result.err)
			}
			if !buildFailed {
				buildFailed = true
				if failPending != nil {
					failPending(result.err)
				}
				if unpublishProvider != nil {
					for _, name := range published {
						unpublishProvider(name)
					}
				}
			}
			if wrapErr != nil {
				errs = append(errs, wrapErr(result.name, result.err))
			} else {
				errs = append(errs, result.err)
			}
			continue
		}
		if buildFailed {
			if closeProviders != nil {
				_ = closeProviders(result.provider)
			}
			continue
		}
		providers = append(providers, result.provider)
		if publish != nil {
			publish(result.name, result.provider)
		}
		published = append(published, result.name)
	}
	if len(errs) > 0 {
		err := errors.Join(errs...)
		if closeProviders != nil {
			// Published providers were removed from the runtime on failure, but
			// this builder still owns their local process/session resources.
			_ = closeProviders(providers...)
		}
		return nil, nil, err
	}
	return providers, published, nil
}

func buildWorkflows(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreworkflow.Provider, []string, error) {
	return buildConfiguredProviders(ctx, cfg.Providers.Workflow,
		func(ctx context.Context, name string, entry *config.ProviderEntry) (coreworkflow.Provider, error) {
			return buildWorkflow(ctx, name, entry, factories, deps)
		},
		func(name string, provider coreworkflow.Provider) {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.PublishProvider(name, provider)
			}
		},
		func(name string, err error) {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.FailStartupProvider(name, err)
			}
		},
		func(name string) {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.UnpublishProvider(name)
			}
		},
		func(err error) {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.FailPendingProviders(err)
			}
		},
		closeWorkflows,
		func(name string, err error) error {
			return fmt.Errorf("bootstrap: workflow from resource %q: %w", name, err)
		},
	)
}

func buildAgents(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreagent.Provider, []string, error) {
	return buildConfiguredProviders(ctx, cfg.Providers.Agent,
		func(ctx context.Context, name string, entry *config.ProviderEntry) (coreagent.Provider, error) {
			return buildAgent(ctx, name, entry, factories, deps)
		},
		func(name string, provider coreagent.Provider) {
			if deps.AgentRuntime != nil {
				deps.AgentRuntime.PublishProvider(name, provider)
			}
		},
		func(name string, err error) {
			if deps.AgentRuntime != nil {
				deps.AgentRuntime.FailStartupProvider(name, err)
			}
		},
		func(name string) {
			if deps.AgentRuntime != nil {
				deps.AgentRuntime.UnpublishProvider(name)
			}
		},
		func(err error) {
			if deps.AgentRuntime != nil {
				deps.AgentRuntime.FailPendingProviders(err)
			}
		},
		closeAgents,
		func(name string, err error) error {
			return fmt.Errorf("bootstrap: agent from resource %q: %w", name, err)
		},
	)
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
	return buildNamedExternalCredentialsProvider(ctx, cfg, name, entry, factories, deps)
}

func buildNamedExternalCredentialsProvider(ctx context.Context, cfg *config.Config, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (core.ExternalCredentialProvider, error) {
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
	node, err := buildExternalCredentialsRuntimeConfigNode(logicalName, entry, deps.EncryptionKey, cfg)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
	}
	if !config.IsComponentRuntimeConfigNode(node) {
		node, err = config.BuildComponentRuntimeConfigNode(logicalName, providermanifestv1.KindExternalCredentials, entry, node)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: external credentials provider %q: %w", logicalName, err)
		}
	}
	hostServices, err := buildProviderHostServices(logicalName, deps)
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

func buildExternalCredentialsRuntimeConfigNode(name string, entry *config.ProviderEntry, encryptionKey []byte, appCfg *config.Config) (yaml.Node, error) {
	if entry == nil {
		return yaml.Node{}, fmt.Errorf("external credentials provider %q is required", name)
	}
	providerCfg, err := config.NodeToMap(entry.Config)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("decode config: %w", err)
	}
	if providerCfg == nil {
		providerCfg = map[string]any{}
	}
	if _, ok := providerCfg["encryptionKey"]; !ok {
		if len(encryptionKey) == 0 {
			return yaml.Node{}, fmt.Errorf("config.encryptionKey is required")
		}
		providerCfg["encryptionKey"] = hex.EncodeToString(encryptionKey)
	}
	resolvedConnections, err := buildExternalCredentialsResolvedConnections(appCfg)
	if err != nil {
		return yaml.Node{}, err
	}
	if len(resolvedConnections) > 0 {
		if _, exists := providerCfg["resolvedConnections"]; exists {
			return yaml.Node{}, fmt.Errorf("config.resolvedConnections is managed by gestaltd")
		}
		providerCfg["resolvedConnections"] = resolvedConnections
	}
	return mapToYAMLNode(providerCfg)
}

func closeAuth(provider core.IdentityProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeAuthProviders(providers map[string]core.IdentityProvider) error {
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

func closeAuthorizationProviders(providers map[string]core.AuthorizationProvider) error {
	if len(providers) == 0 {
		return nil
	}
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		errs = append(errs, provider.Close())
	}
	return errors.Join(errs...)
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

func buildAuthProviders(cfg *config.Config, factories *FactoryRegistry, deps Deps) (string, map[string]core.IdentityProvider, error) {
	selectedName, _, err := cfg.SelectedIdentityProvider()
	if err != nil {
		return "", nil, err
	}
	if len(cfg.Providers.Identity) == 0 {
		return selectedName, nil, nil
	}
	if factories.Auth == nil {
		return "", nil, fmt.Errorf("bootstrap: authentication factory is not registered")
	}
	providers := make(map[string]core.IdentityProvider, len(cfg.Providers.Identity))
	for name, authEntry := range cfg.Providers.Identity {
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

func buildNamedAuthProvider(name string, authEntry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (core.IdentityProvider, error) {
	if authEntry == nil {
		return nil, nil
	}
	node := authEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "identity", authEntry, authEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: identity provider %q: %w", name, err)
		}
	}
	hostServices, err := buildProviderHostServices(name, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: identity provider %q: %w", name, err)
	}
	auth, err := factories.Auth(context.Background(), name, node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: identity provider %q: %w", name, err)
	}
	return auth, nil
}

type authorizationProviderSets struct {
	Raw     map[string]core.AuthorizationProvider
	Guarded map[string]core.AuthorizationProvider
}

func buildAuthorizationProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (authorizationProviderSets, error) {
	if len(cfg.Providers.Authorization) == 0 {
		return authorizationProviderSets{}, nil
	}
	if factories.Authorization == nil {
		return authorizationProviderSets{}, fmt.Errorf("bootstrap: authorization factory is not registered")
	}
	name, entry, err := cfg.SelectedAuthorizationProvider()
	if err != nil {
		return authorizationProviderSets{}, err
	}
	if entry == nil {
		return authorizationProviderSets{}, nil
	}
	providers, err := buildNamedAuthorizationProvider(ctx, name, entry, factories, deps)
	if err != nil {
		return authorizationProviderSets{}, err
	}
	return authorizationProviderSets{
		Raw:     map[string]core.AuthorizationProvider{name: providers.Raw},
		Guarded: map[string]core.AuthorizationProvider{name: providers.Guarded},
	}, nil
}

func buildNamedAuthorizationProvider(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (providerdrivers.AuthorizationBuildResult, error) {
	logicalName := strings.TrimSpace(name)
	if logicalName == "" {
		logicalName = "authorization"
	}
	if entry == nil {
		return providerdrivers.AuthorizationBuildResult{}, fmt.Errorf("bootstrap: authorization provider %q is not configured", logicalName)
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(logicalName, providermanifestv1.KindAuthorization, entry, entry.Config)
		if err != nil {
			return providerdrivers.AuthorizationBuildResult{}, fmt.Errorf("bootstrap: authorization provider %q: %w", logicalName, err)
		}
	}
	hostServices, err := buildProviderHostServices(logicalName, deps)
	if err != nil {
		return providerdrivers.AuthorizationBuildResult{}, fmt.Errorf("bootstrap: authorization provider %q: %w", logicalName, err)
	}
	provider, err := factories.Authorization(ctx, logicalName, node, hostServices, deps)
	if err != nil {
		return providerdrivers.AuthorizationBuildResult{}, fmt.Errorf("bootstrap: authorization provider %q: %w", logicalName, err)
	}
	return provider, nil
}

func resolveCallerTokenPrivateKey(ctx context.Context, sm core.SecretManager) (string, error) {
	return resolveCallerTokenKey(ctx, sm, callerTokenPrivateKeySecretName)
}

func resolveCallerTokenPublicKey(ctx context.Context, sm core.SecretManager) (string, error) {
	return resolveCallerTokenKey(ctx, sm, callerTokenPublicKeySecretName)
}

func resolveCallerTokenKey(ctx context.Context, sm core.SecretManager, name string) (string, error) {
	if sm == nil {
		return "", nil
	}
	value, err := sm.GetSecret(ctx, name)
	if err != nil {
		if errors.Is(err, core.ErrSecretNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("bootstrap: resolve %s: %w", name, err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("bootstrap: secret %q resolved to empty value", name)
	}
	return value, nil
}

func buildIndexedDB(entry *config.ProviderEntry, factories *FactoryRegistry) (indexeddb.IndexedDB, error) {
	if entry == nil {
		return nil, fmt.Errorf("indexeddb provider is required")
	}
	if factories.IndexedDB == nil {
		return nil, fmt.Errorf("indexeddb factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("indexeddb", "indexeddb", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("indexeddb provider: %w", err)
		}
	}
	ds, err := factories.IndexedDB(node)
	if err != nil {
		return nil, fmt.Errorf("indexeddb provider: %w", err)
	}
	return ds, nil
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

func buildS3(name string, entry *config.ProviderEntry, factories *FactoryRegistry) (s3sdk.S3, error) {
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
	ctx = invocation.WithCallerProvider(ctx, invocation.ProviderKindWorkflow, name)
	if entry == nil {
		return nil, fmt.Errorf("workflow provider is required")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "workflow", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
	}
	hostServices, err := buildProviderHostServices(name, deps)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if !entry.UsesRuntimePlacement() {
		publicWorkflowProviderHostServicesCleanup, err := registerPublicWorkflowProviderHostServices(name, hostServices, deps)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
		cleanup = chainCleanup(cleanup, publicWorkflowProviderHostServicesCleanup)
	}
	if factories.Workflow == nil {
		return nil, fmt.Errorf("workflow factory is not registered")
	}
	provider, err := factories.Workflow(ctx, name, node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	if entry.UsesRuntimePlacement() {
		workerPool, err := buildHostedWorkflowWorkerPool(ctx, name, entry, node, hostServices, deps)
		if err != nil {
			_ = provider.Close()
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
		provider = wrapWorkflowProviderWithRuntimeWorkers(provider, workerPool)
	}
	if cleanup != nil {
		provider = &workflowProviderWithCleanup{
			Provider: provider,
			cleanup:  cleanup,
		}
		cleanup = nil
	}
	return provider, nil
}

func buildAgent(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (coreagent.Provider, error) {
	ctx = invocation.WithCallerProvider(ctx, invocation.ProviderKindAgent, name)
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
	hostServices, err := buildProviderHostServices(name, deps)
	if err != nil {
		return nil, fmt.Errorf("agent provider: %w", err)
	}
	var (
		provider    coreagent.Provider
		providerErr error
	)
	if entry.UsesRuntimePlacement() {
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
	return tracked, nil
}
