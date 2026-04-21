package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

var (
	ErrWorkflowNotConfigured      = errors.New("workflow is not configured")
	ErrExecutionRefsNotConfigured = errors.New("workflow execution refs are not configured")
	ErrWorkflowSubjectRequired    = errors.New("workflow subject is required")
	ErrWorkflowScheduleSubject    = ErrWorkflowSubjectRequired
	ErrDuplicateExecutionRefs     = errors.New("workflow schedule matched multiple execution references")
)

const workflowScheduleExecutionRefBasePrefix = "workflow_schedule:"

type WorkflowControl interface {
	ResolveProvider(name string) (coreworkflow.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreworkflow.Provider, err error)
}

type Service interface {
	ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error)
	CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (*ManagedSchedule, error)
	GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
	UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (*ManagedSchedule, error)
	DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) error
	PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
	ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
}

type Config struct {
	Providers             *registry.ProviderMap[core.Provider]
	Workflow              WorkflowControl
	WorkflowExecutionRefs *coredata.WorkflowExecutionRefService
	Invoker               invocation.Invoker
	Authorizer            authorization.RuntimeAuthorizer
	DefaultConnection     map[string]string
	CatalogConnection     map[string]string
	Now                   func() time.Time
}

type Manager struct {
	providers             *registry.ProviderMap[core.Provider]
	workflow              WorkflowControl
	workflowExecutionRefs *coredata.WorkflowExecutionRefService
	invoker               invocation.Invoker
	authorizer            authorization.RuntimeAuthorizer
	defaultConnection     map[string]string
	catalogConnection     map[string]string
	now                   func() time.Time
}

type ScheduleUpsert struct {
	ProviderName string
	Cron         string
	Timezone     string
	Target       coreworkflow.Target
	Paused       bool
}

type ManagedSchedule struct {
	ProviderName string
	Schedule     *coreworkflow.Schedule
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type ManagedRun struct {
	ProviderName string
	Run          *coreworkflow.Run
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

func New(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		providers:             cfg.Providers,
		workflow:              cfg.Workflow,
		workflowExecutionRefs: cfg.WorkflowExecutionRefs,
		invoker:               cfg.Invoker,
		authorizer:            cfg.Authorizer,
		defaultConnection:     maps.Clone(cfg.DefaultConnection),
		catalogConnection:     maps.Clone(cfg.CatalogConnection),
		now:                   now,
	}
}

func (m *Manager) ListRuns(ctx context.Context, p *principal.Principal) ([]*ManagedRun, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, false)
	if err != nil {
		return nil, err
	}
	refsByProvider := executionRefsByProvider(refs)
	out := make([]*ManagedRun, 0, len(refs))
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		runs, err := provider.ListRuns(ctx, coreworkflow.ListRunsRequest{})
		if err != nil {
			return nil, err
		}
		refIndex := executionRefsByID(providerRefs)
		for _, run := range runs {
			if run == nil {
				continue
			}
			ref := refIndex[strings.TrimSpace(run.ExecutionRef)]
			if ref == nil || !m.allowTarget(ctx, p, ref.Target) || !runMatchesExecutionRef(providerName, run, ref) {
				continue
			}
			out = append(out, &ManagedRun{
				ProviderName: providerName,
				Run:          run,
				ExecutionRef: ref,
				provider:     provider,
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

func (m *Manager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, core.ErrNotFound
	}
	refs, err := m.listOwnedExecutionRefs(ctx, p, false)
	if err != nil {
		return nil, err
	}
	refsByProvider := executionRefsByProvider(refs)
	var firstErr error
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{RunID: runID})
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ref := executionRefsByID(providerRefs)[strings.TrimSpace(run.ExecutionRef)]
		if ref == nil || !m.allowTarget(ctx, p, ref.Target) || !runMatchesExecutionRef(providerName, run, ref) {
			continue
		}
		return &ManagedRun{
			ProviderName: providerName,
			Run:          run,
			ExecutionRef: ref,
			provider:     provider,
		}, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, core.ErrNotFound
}

func (m *Manager) ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedSchedule, 0, len(refs))
	for _, ref := range refs {
		if !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		scheduleID := scheduleIDFromExecutionRefID(ref.ID)
		if scheduleID == "" {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
			continue
		}
		out = append(out, &ManagedSchedule{
			ProviderName: strings.TrimSpace(ref.ProviderName),
			Schedule:     schedule,
			ExecutionRef: ref,
			provider:     provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Schedule != nil && right.Schedule != nil && left.Schedule.CreatedAt != nil && right.Schedule.CreatedAt != nil && !left.Schedule.CreatedAt.Equal(*right.Schedule.CreatedAt) {
			return left.Schedule.CreatedAt.Before(*right.Schedule.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Schedule != nil {
			leftID = left.Schedule.ID
		}
		if right.Schedule != nil {
			rightID = right.Schedule.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (*ManagedSchedule, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target)
	if err != nil {
		return nil, err
	}

	scheduleID := uuid.NewString()
	executionRefID := scheduleExecutionRefID(scheduleID)
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, target, p)
	if err != nil {
		return nil, err
	}
	schedule, err := provider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   scheduleID,
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	return &ManagedSchedule{
		ProviderName: providerName,
		Schedule:     schedule,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	return m.requireOwnedSchedule(ctx, scheduleID, p)
}

func (m *Manager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (*ManagedSchedule, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	nextProviderName, nextProvider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target)
	if err != nil {
		return nil, err
	}

	executionRefID := scheduleExecutionRefID(strings.TrimSpace(existing.Schedule.ID))
	nextRef, err := m.putExecutionRef(ctx, executionRefID, nextProviderName, target, p)
	if err != nil {
		return nil, err
	}
	schedule, err := nextProvider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   strings.TrimSpace(existing.Schedule.ID),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, nextRef)
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingProvider(existing).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
			ScheduleID: strings.TrimSpace(existing.Schedule.ID),
		}); err != nil {
			_ = nextProvider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
				ScheduleID: strings.TrimSpace(existing.Schedule.ID),
			})
			m.revokeExecutionRef(ctx, nextRef)
			return nil, err
		}
	}
	if existing.ExecutionRef != nil && existing.ExecutionRef.ID != "" && existing.ExecutionRef.ID != executionRefID {
		m.revokeExecutionRef(ctx, existing.ExecutionRef)
	}
	return &ManagedSchedule{
		ProviderName: nextProviderName,
		Schedule:     schedule,
		ExecutionRef: nextRef,
		provider:     nextProvider,
	}, nil
}

func (m *Manager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) error {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return err
	}
	if err := existingProvider(value).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
	return nil
}

func (m *Manager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	schedule, err := existingProvider(value).PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
	return value, nil
}

func (m *Manager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	schedule, err := existingProvider(value).ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
	return value, nil
}

func (m *Manager) resolveProviderSelection(providerName string) (string, coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return "", nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProviderSelection(strings.TrimSpace(providerName))
}

func (m *Manager) resolveProviderByName(providerName string) (coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProvider(strings.TrimSpace(providerName))
}

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) (coreworkflow.Target, error) {
	if m == nil || m.providers == nil {
		return coreworkflow.Target{}, fmt.Errorf("%w: workflow providers are not configured", invocation.ErrInternal)
	}
	pluginName := strings.TrimSpace(target.PluginName)
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreworkflow.Target{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return coreworkflow.Target{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.Target{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrOperationNotFound)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return coreworkflow.Target{}, invocation.ErrAuthorizationDenied
	}

	connection := strings.TrimSpace(target.Connection)
	if connection != "" && !config.SafeConnectionValue(connection) {
		return coreworkflow.Target{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = config.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(target.Instance)
	if instance != "" && !config.SafeInstanceValue(instance) {
		return coreworkflow.Target{}, fmt.Errorf("instance name contains invalid characters")
	}

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, pluginName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	boundConnections, sessionInstance := m.catalogSelectorConfig().BoundSessionCatalogConnections(pluginName, p, connection, instance)
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, pluginName, resolver, p, operation, boundConnections, sessionInstance)
	if err != nil {
		return coreworkflow.Target{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, pluginName, connection, sessionInstance)
		if err != nil {
			return coreworkflow.Target{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}
	return coreworkflow.Target{
		PluginName: pluginName,
		Operation:  opMeta.ID,
		Connection: connection,
		Instance:   sessionInstance,
		Input:      maps.Clone(target.Input),
	}, nil
}

func (m *Manager) requireOwnedSchedule(ctx context.Context, scheduleID string, p *principal.Principal) (*ManagedSchedule, error) {
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" {
		return nil, core.ErrNotFound
	}
	ref, err := m.findOwnedExecutionRef(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	if !m.allowTarget(ctx, p, ref.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
	if err != nil {
		return nil, err
	}
	schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
	if err != nil {
		return nil, err
	}
	if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedSchedule{
		ProviderName: strings.TrimSpace(ref.ProviderName),
		Schedule:     schedule,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) listOwnedExecutionRefs(ctx context.Context, p *principal.Principal, activeOnly bool) ([]*coreworkflow.ExecutionReference, error) {
	if m == nil || m.workflowExecutionRefs == nil {
		return nil, ErrExecutionRefsNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	refs, err := m.workflowExecutionRefs.ListBySubject(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]*coreworkflow.ExecutionReference, 0, len(refs))
	for _, ref := range refs {
		if !executionRefOwnedBy(ref, p) || (activeOnly && !executionRefActive(ref)) {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) findOwnedExecutionRef(ctx context.Context, scheduleID string, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	prefix := scheduleExecutionRefPrefix(scheduleID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, scheduleID)
		}
		match = ref
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	return match, nil
}

func (m *Manager) putExecutionRef(ctx context.Context, executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	if m == nil || m.workflowExecutionRefs == nil {
		return nil, ErrExecutionRefsNotConfigured
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	return m.workflowExecutionRefs.Put(ctx, &coreworkflow.ExecutionReference{
		ID:           executionRefID,
		ProviderName: strings.TrimSpace(providerName),
		Target:       target,
		SubjectID:    subjectID,
		Permissions:  principal.PermissionsToAccessPermissions(p.TokenPermissions),
	})
}

func (m *Manager) revokeExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	if m == nil || m.workflowExecutionRefs == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return
	}
	cloned := *ref
	now := m.now().UTC().Truncate(time.Second)
	cloned.RevokedAt = &now
	_, _ = m.workflowExecutionRefs.Put(ctx, &cloned)
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

func (m *Manager) allowTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	pluginName := strings.TrimSpace(target.PluginName)
	operation := strings.TrimSpace(target.Operation)
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

func executionRefOwnedBy(ref *coreworkflow.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func executionRefActive(ref *coreworkflow.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

func executionRefsByProvider(refs []*coreworkflow.ExecutionReference) map[string][]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string][]*coreworkflow.ExecutionReference)
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

func executionRefsByID(refs []*coreworkflow.ExecutionReference) map[string]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreworkflow.ExecutionReference, len(refs))
	for _, ref := range refs {
		if ref == nil || strings.TrimSpace(ref.ID) == "" {
			continue
		}
		out[strings.TrimSpace(ref.ID)] = ref
	}
	return out
}

func scheduleMatchesExecutionRef(providerName string, schedule *coreworkflow.Schedule, ref *coreworkflow.ExecutionReference) bool {
	if schedule == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return strings.TrimSpace(schedule.Target.PluginName) == strings.TrimSpace(ref.Target.PluginName) &&
		strings.TrimSpace(schedule.Target.Operation) == strings.TrimSpace(ref.Target.Operation) &&
		strings.TrimSpace(schedule.Target.Connection) == strings.TrimSpace(ref.Target.Connection) &&
		strings.TrimSpace(schedule.Target.Instance) == strings.TrimSpace(ref.Target.Instance)
}

func runMatchesExecutionRef(providerName string, run *coreworkflow.Run, ref *coreworkflow.ExecutionReference) bool {
	if run == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return strings.TrimSpace(run.Target.PluginName) == strings.TrimSpace(ref.Target.PluginName) &&
		strings.TrimSpace(run.Target.Operation) == strings.TrimSpace(ref.Target.Operation) &&
		strings.TrimSpace(run.Target.Connection) == strings.TrimSpace(ref.Target.Connection) &&
		strings.TrimSpace(run.Target.Instance) == strings.TrimSpace(ref.Target.Instance)
}

func workflowActorFromPrincipal(p *principal.Principal) coreworkflow.Actor {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   strings.TrimSpace(p.SubjectID),
		SubjectKind: string(p.Kind),
		DisplayName: workflowActorDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func workflowActorDisplayName(p *principal.Principal) string {
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

func scheduleExecutionRefID(scheduleID string) string {
	return scheduleExecutionRefPrefix(scheduleID) + uuid.NewString()
}

func scheduleExecutionRefPrefix(scheduleID string) string {
	return workflowScheduleExecutionRefBasePrefix + strings.TrimSpace(scheduleID) + ":"
}

func scheduleIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowScheduleExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowScheduleExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func existingProvider(value *ManagedSchedule) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}
