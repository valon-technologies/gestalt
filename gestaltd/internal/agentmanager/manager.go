package agentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

var (
	ErrAgentNotConfigured            = errors.New("agent is not configured")
	ErrAgentRunMetadataNotConfigured = errors.New("agent run metadata is not configured")
	ErrAgentSubjectRequired          = errors.New("agent subject is required")
	ErrAgentCallerPluginRequired     = errors.New("agent caller plugin is required for inherited tools")
	ErrAgentInheritedSurfaceTool     = errors.New("agent inherited surface tools are not supported")
	ErrAgentRunCreationInProgress    = errors.New("agent run creation is already in progress for this idempotency key")
)

type AgentControl interface {
	ResolveProvider(name string) (coreagent.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreagent.Provider, err error)
	ProviderNames() []string
}

type Service interface {
	Run(ctx context.Context, p *principal.Principal, req coreagent.ManagerRunRequest) (*coreagent.ManagedRun, error)
	GetRun(ctx context.Context, p *principal.Principal, runID string) (*coreagent.ManagedRun, error)
	ListRuns(ctx context.Context, p *principal.Principal) ([]*coreagent.ManagedRun, error)
	ListRunsByProvider(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.ManagedRun, error)
	CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*coreagent.ManagedRun, error)
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Agent             AgentControl
	RunMetadata       *coredata.AgentRunMetadataService
	Invoker           invocation.Invoker
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]config.PluginInvocationDependency
	Now               func() time.Time
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	agent             AgentControl
	runMetadata       *coredata.AgentRunMetadataService
	invoker           invocation.Invoker
	authorizer        authorization.RuntimeAuthorizer
	defaultConnection map[string]string
	catalogConnection map[string]string
	pluginInvokes     map[string][]config.PluginInvocationDependency
	now               func() time.Time
}

func New(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pluginInvokes := make(map[string][]config.PluginInvocationDependency, len(cfg.PluginInvokes))
	for pluginName, deps := range cfg.PluginInvokes {
		pluginInvokes[pluginName] = append([]config.PluginInvocationDependency(nil), deps...)
	}
	return &Manager{
		providers:         cfg.Providers,
		agent:             cfg.Agent,
		runMetadata:       cfg.RunMetadata,
		invoker:           cfg.Invoker,
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     pluginInvokes,
		now:               now,
	}
}

func (m *Manager) Run(ctx context.Context, p *principal.Principal, req coreagent.ManagerRunRequest) (*coreagent.ManagedRun, error) {
	p = principal.Canonicalized(p)
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentRunMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	providerName, provider, err := m.resolveProviderSelection(req.ProviderName)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)

	tools, err := m.resolveTools(ctx, p, req.CallerPluginName, req.ToolRefs, req.ToolSource)
	if err != nil {
		return nil, err
	}
	runID := uuid.NewString()
	if idempotencyKey != "" {
		for {
			claimedRunID, claimed, err := m.runMetadata.ClaimIdempotency(ctx, subjectID, providerName, idempotencyKey, runID, m.now())
			if err != nil {
				return nil, err
			}
			if claimed {
				break
			}
			existing, err := m.runMetadata.Get(ctx, claimedRunID)
			if err != nil {
				if errors.Is(err, indexeddb.ErrNotFound) {
					return nil, ErrAgentRunCreationInProgress
				}
				return nil, err
			}
			if !executionRefOwnedBy(existing, p) || !m.allowRun(ctx, p, existing) {
				return nil, core.ErrNotFound
			}
			managed, err := m.getManagedRunByMetadata(ctx, p, existing)
			if err == nil {
				return managed, nil
			}
			if !errors.Is(err, core.ErrNotFound) {
				return nil, err
			}
			if deleteErr := m.runMetadata.Delete(ctx, existing.ID); deleteErr != nil {
				return nil, deleteErr
			}
			runID = uuid.NewString()
		}
	}
	ref, err := m.runMetadata.Put(ctx, &coreagent.ExecutionReference{
		ID:                  runID,
		ProviderName:        providerName,
		SubjectID:           subjectID,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		IdempotencyKey:      idempotencyKey,
		Permissions:         principal.PermissionsToAccessPermissions(p.TokenPermissions),
		Tools:               tools,
	})
	if err != nil {
		if idempotencyKey != "" {
			if releaseErr := m.runMetadata.ReleaseIdempotency(ctx, subjectID, providerName, idempotencyKey); releaseErr != nil {
				err = errors.Join(err, releaseErr)
			}
		}
		return nil, err
	}

	run, err := provider.StartRun(ctx, coreagent.StartRunRequest{
		RunID:           runID,
		IdempotencyKey:  idempotencyKey,
		ProviderName:    providerName,
		Model:           strings.TrimSpace(req.Model),
		Messages:        append([]coreagent.Message(nil), req.Messages...),
		Tools:           append([]coreagent.Tool(nil), tools...),
		ResponseSchema:  maps.Clone(req.ResponseSchema),
		SessionRef:      strings.TrimSpace(req.SessionRef),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
		CreatedBy:       agentActorFromPrincipal(p),
		ExecutionRef:    runID,
	})
	if err != nil {
		fallback, getErr := provider.GetRun(ctx, coreagent.GetRunRequest{RunID: runID})
		if getErr == nil {
			run = fallback
		} else {
			_ = m.runMetadata.Delete(ctx, ref.ID)
			return nil, err
		}
	}
	normalized, err := normalizeProviderRun(providerName, runID, run)
	if err != nil {
		_ = m.runMetadata.Delete(ctx, ref.ID)
		return nil, err
	}
	return &coreagent.ManagedRun{
		ProviderName: providerName,
		Run:          normalized,
	}, nil
}

func (m *Manager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*coreagent.ManagedRun, error) {
	ref, err := m.requireOwnedRunMetadata(ctx, runID, p)
	if err != nil {
		return nil, err
	}
	return m.getManagedRunByMetadata(ctx, p, ref)
}

func (m *Manager) ListRuns(ctx context.Context, p *principal.Principal) ([]*coreagent.ManagedRun, error) {
	return m.listRuns(ctx, p, "")
}

func (m *Manager) ListRunsByProvider(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.ManagedRun, error) {
	return m.listRuns(ctx, p, providerName)
}

func (m *Manager) listRuns(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.ManagedRun, error) {
	refs, err := m.listOwnedRunMetadata(ctx, p, false)
	if err != nil {
		return nil, err
	}
	refsByProvider := executionRefsByProvider(refs)
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		if providerRefs, ok := refsByProvider[providerName]; ok {
			refsByProvider = map[string][]*coreagent.ExecutionReference{providerName: providerRefs}
		} else {
			refsByProvider = map[string][]*coreagent.ExecutionReference{}
		}
	}
	out := make([]*coreagent.ManagedRun, 0, len(refs))
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		runs, err := provider.ListRuns(ctx, coreagent.ListRunsRequest{})
		if err != nil {
			return nil, err
		}
		refIndex := executionRefsByID(providerRefs)
		for _, run := range runs {
			if run == nil {
				continue
			}
			ref := refIndex[strings.TrimSpace(run.ID)]
			if ref == nil || !m.allowRun(ctx, p, ref) {
				continue
			}
			normalized, err := normalizeProviderRun(providerName, ref.ID, run)
			if err != nil {
				return nil, err
			}
			out = append(out, &coreagent.ManagedRun{
				ProviderName: providerName,
				Run:          normalized,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Run != nil && right.Run != nil && left.Run.CreatedAt != nil && right.Run.CreatedAt != nil && !left.Run.CreatedAt.Equal(*right.Run.CreatedAt) {
			return left.Run.CreatedAt.After(*right.Run.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Run != nil {
			leftID = left.Run.ID
		}
		if right.Run != nil {
			rightID = right.Run.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*coreagent.ManagedRun, error) {
	ref, err := m.requireOwnedRunMetadata(ctx, runID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	run, err := provider.CancelRun(ctx, coreagent.CancelRunRequest{
		RunID:  strings.TrimSpace(runID),
		Reason: strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderRun(ref.ProviderName, strings.TrimSpace(runID), run)
	if err != nil {
		return nil, err
	}
	return &coreagent.ManagedRun{
		ProviderName: ref.ProviderName,
		Run:          normalized,
	}, nil
}

func (m *Manager) getManagedRunByMetadata(ctx context.Context, p *principal.Principal, ref *coreagent.ExecutionReference) (*coreagent.ManagedRun, error) {
	if ref == nil {
		return nil, core.ErrNotFound
	}
	if !m.allowRun(ctx, p, ref) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	run, err := provider.GetRun(ctx, coreagent.GetRunRequest{RunID: strings.TrimSpace(ref.ID)})
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderRun(ref.ProviderName, ref.ID, run)
	if err != nil {
		return nil, err
	}
	return &coreagent.ManagedRun{
		ProviderName: ref.ProviderName,
		Run:          normalized,
	}, nil
}

func (m *Manager) resolveProviderSelection(providerName string) (string, coreagent.Provider, error) {
	if m == nil || m.agent == nil {
		return "", nil, ErrAgentNotConfigured
	}
	return m.agent.ResolveProviderSelection(strings.TrimSpace(providerName))
}

func (m *Manager) resolveProviderByName(providerName string) (coreagent.Provider, error) {
	if m == nil || m.agent == nil {
		return nil, ErrAgentNotConfigured
	}
	return m.agent.ResolveProvider(strings.TrimSpace(providerName))
}

func (m *Manager) listOwnedRunMetadata(ctx context.Context, p *principal.Principal, activeOnly bool) ([]*coreagent.ExecutionReference, error) {
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentRunMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	refs, err := m.runMetadata.ListBySubject(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]*coreagent.ExecutionReference, 0, len(refs))
	for _, ref := range refs {
		if !executionRefOwnedBy(ref, p) || (activeOnly && !executionRefActive(ref)) {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) requireOwnedRunMetadata(ctx context.Context, runID string, p *principal.Principal) (*coreagent.ExecutionReference, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, core.ErrNotFound
	}
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentRunMetadataNotConfigured
	}
	ref, err := m.runMetadata.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, err
	}
	if !executionRefOwnedBy(ref, p) || !m.allowRun(ctx, p, ref) {
		return nil, core.ErrNotFound
	}
	return ref, nil
}

func (m *Manager) resolveTools(ctx context.Context, p *principal.Principal, callerPluginName string, explicit []coreagent.ToolRef, source coreagent.ToolSourceMode) ([]coreagent.Tool, error) {
	refs := make([]coreagent.ToolRef, 0, len(explicit)+4)
	refs = append(refs, explicit...)
	if source == coreagent.ToolSourceModeInheritInvokes {
		callerPluginName = strings.TrimSpace(callerPluginName)
		if callerPluginName == "" {
			return nil, ErrAgentCallerPluginRequired
		}
		for _, invoke := range m.pluginInvokes[callerPluginName] {
			if strings.TrimSpace(invoke.Surface) != "" {
				return nil, fmt.Errorf("%w: %s.%s", ErrAgentInheritedSurfaceTool, callerPluginName, invoke.Surface)
			}
			refs = append(refs, coreagent.ToolRef{
				PluginName: invoke.Plugin,
				Operation:  invoke.Operation,
			})
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]coreagent.Tool, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		tool, err := m.resolveTool(ctx, p, ref)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[tool.ID]; ok {
			continue
		}
		seen[tool.ID] = struct{}{}
		out = append(out, tool)
	}
	return out, nil
}

func (m *Manager) resolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if m == nil || m.providers == nil {
		return coreagent.Tool{}, fmt.Errorf("%w: agent providers are not configured", invocation.ErrInternal)
	}
	pluginName := strings.TrimSpace(ref.PluginName)
	if pluginName == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool plugin is required", invocation.ErrProviderNotFound)
	}
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreagent.Tool{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return coreagent.Tool{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	operation := strings.TrimSpace(ref.Operation)
	if operation == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool operation is required", invocation.ErrOperationNotFound)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return coreagent.Tool{}, invocation.ErrAuthorizationDenied
	}

	connection := strings.TrimSpace(ref.Connection)
	if connection != "" && !config.SafeConnectionValue(connection) {
		return coreagent.Tool{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = config.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(ref.Instance)
	if instance != "" && !config.SafeInstanceValue(instance) {
		return coreagent.Tool{}, fmt.Errorf("instance name contains invalid characters")
	}
	if m.authorizer != nil && principal.IsWorkloadPrincipal(p) && (connection != "" || instance != "") {
		return coreagent.Tool{}, fmt.Errorf("%w: workloads may not override connection or instance bindings", invocation.ErrAuthorizationDenied)
	}

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, pluginName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	boundCredential := invocation.CredentialBindingResolution{}
	if bindingResolver, ok := m.invoker.(invocation.EffectiveCredentialBindingResolver); ok {
		boundCredential, err = bindingResolver.ResolveEffectiveCredentialBinding(p, pluginName, connection, instance)
		if err != nil {
			return coreagent.Tool{}, err
		}
	}
	boundConnections, sessionInstance := m.catalogSelectorConfig().BoundSessionCatalogConnections(pluginName, p, connection, instance)
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, pluginName, resolver, p, operation, boundConnections, sessionInstance)
	if err != nil {
		return coreagent.Tool{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := invocation.ResolveTokenForBinding(ctx, resolver, p, pluginName, connection, sessionInstance, boundCredential)
		if err != nil {
			return coreagent.Tool{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}

	parametersSchema, err := operationInputSchema(opMeta)
	if err != nil {
		return coreagent.Tool{}, err
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = strings.TrimSpace(opMeta.Title)
	}
	if name == "" {
		name = pluginName + "." + opMeta.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(opMeta.Description)
	}
	return coreagent.Tool{
		ID:               agentToolID(pluginName, opMeta.ID, connection, sessionInstance),
		Name:             name,
		Description:      description,
		ParametersSchema: parametersSchema,
		Target: coreagent.ToolTarget{
			PluginName: pluginName,
			Operation:  opMeta.ID,
			Connection: connection,
			Instance:   sessionInstance,
		},
	}, nil
}

func (m *Manager) allowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowProvider(ctx, p, provider)
}

func (m *Manager) allowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowOperation(ctx, p, provider, operation)
}

func (m *Manager) providerAccessContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	if m == nil || m.authorizer == nil {
		return invocation.AccessContext{}
	}
	access, _ := m.authorizer.ResolveAccess(ctx, p, provider)
	return access
}

func (m *Manager) allowRun(ctx context.Context, p *principal.Principal, ref *coreagent.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	if !executionRefOwnedBy(ref, p) {
		return false
	}
	if len(ref.Tools) == 0 {
		return true
	}
	for _, tool := range ref.Tools {
		if !m.allowTool(ctx, p, tool) {
			return false
		}
	}
	return true
}

func (m *Manager) allowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool {
	pluginName := strings.TrimSpace(tool.Target.PluginName)
	operation := strings.TrimSpace(tool.Target.Operation)
	if pluginName == "" || operation == "" {
		return false
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return false
	}
	return principal.AllowsOperationPermission(p, pluginName, operation)
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Authorizer:        m.authorizer,
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func executionRefOwnedBy(ref *coreagent.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func executionRefActive(ref *coreagent.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

func executionRefsByProvider(refs []*coreagent.ExecutionReference) map[string][]*coreagent.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string][]*coreagent.ExecutionReference)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		providerName := strings.TrimSpace(ref.ProviderName)
		if providerName == "" {
			continue
		}
		out[providerName] = append(out[providerName], ref)
	}
	return out
}

func executionRefsByID(refs []*coreagent.ExecutionReference) map[string]*coreagent.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreagent.ExecutionReference, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		id := strings.TrimSpace(ref.ID)
		if id == "" {
			continue
		}
		out[id] = ref
	}
	return out
}

func normalizeProviderRun(providerName, runID string, run *coreagent.Run) (*coreagent.Run, error) {
	if run == nil {
		return nil, core.ErrNotFound
	}
	cloned := *run
	if strings.TrimSpace(cloned.ID) == "" {
		cloned.ID = strings.TrimSpace(runID)
	}
	if strings.TrimSpace(cloned.ID) != strings.TrimSpace(runID) {
		return nil, fmt.Errorf("agent provider returned run id %q, want %q", cloned.ID, runID)
	}
	if strings.TrimSpace(cloned.ProviderName) == "" {
		cloned.ProviderName = strings.TrimSpace(providerName)
	}
	return &cloned, nil
}

func operationInputSchema(op catalog.CatalogOperation) (map[string]any, error) {
	if len(op.InputSchema) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(op.InputSchema, &out); err != nil {
		return nil, fmt.Errorf("decode %s input schema: %w", op.ID, err)
	}
	return out, nil
}

func agentToolID(pluginName, operation, connection, instance string) string {
	id := strings.TrimSpace(pluginName) + "/" + strings.TrimSpace(operation)
	if strings.TrimSpace(connection) != "" {
		id += "?connection=" + strings.TrimSpace(connection)
	}
	if strings.TrimSpace(instance) != "" {
		sep := "?"
		if strings.Contains(id, "?") {
			sep = "&"
		}
		id += sep + "instance=" + strings.TrimSpace(instance)
	}
	return id
}

func agentActorFromPrincipal(p *principal.Principal) coreagent.Actor {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreagent.Actor{}
	}
	return coreagent.Actor{
		SubjectID:   strings.TrimSpace(p.SubjectID),
		SubjectKind: string(p.Kind),
		DisplayName: agentActorDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func agentActorDisplayName(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if value := strings.TrimSpace(p.DisplayName); value != "" {
		return value
	}
	if p.Identity != nil {
		return strings.TrimSpace(p.Identity.DisplayName)
	}
	return ""
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

var _ Service = (*Manager)(nil)
