package workflowmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (m *Manager) StartRun(ctx context.Context, p *principal.Principal, req RunStart) (out *ManagedRun, err error) {
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunStart)
	audit.setCallerApp(req.CallerAppName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.Run != nil {
				audit.setWorkflowTarget(out.Run.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)

	run, err := provider.StartRun(ctx, coreworkflow.StartRunRequest{
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:    strings.TrimSpace(req.WorkflowKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		DefinitionID:   strings.TrimSpace(req.DefinitionID),
	})
	if err != nil {
		return nil, err
	}
	return &ManagedRun{ProviderName: providerName, Run: run, provider: provider}, nil
}

func (m *Manager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error) {
	return m.requireOwnedRun(ctx, p, runID, "")
}

func (m *Manager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (out *ManagedRun, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunCancel)
	audit.setObjectTarget(workflowAuditTargetRun, runID, "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.Run != nil {
				audit.setWorkflowTarget(out.Run.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedRun(ctx, p, runID, "")
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.Run != nil {
		audit.setWorkflowTarget(value.Run.Target)
	}
	run, err := value.provider.CancelRun(ctx, coreworkflow.CancelRunRequest{
		RunID:  strings.TrimSpace(runID),
		Reason: strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, err
	}
	value.Run = run
	return value, nil
}

func (m *Manager) SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (out *ManagedRunSignal, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunSignal)
	audit.setObjectTarget(workflowAuditTargetRun, req.RunID, "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			audit.setWorkflowKey(out.WorkflowKey)
			if out.Run != nil {
				audit.setWorkflowTarget(out.Run.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedRun(ctx, p, req.RunID, "")
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.Run != nil {
		audit.setWorkflowTarget(value.Run.Target)
	}
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}
	resp, err := value.provider.SignalRun(ctx, coreworkflow.SignalRunRequest{
		RunID:  strings.TrimSpace(req.RunID),
		Signal: signal,
	})
	if err != nil {
		return nil, err
	}
	return managedSignalResponse(value.ProviderName, value.provider, resp)
}

func (m *Manager) SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (out *ManagedRunSignal, err error) {
	phase := "validate_subject"
	providerName := ""
	var target coreworkflow.Target
	var targetAuthFailure *targetAuthorizationFailure
	p = principal.Canonicalized(p)
	ctx = WithCallerAppName(ctx, req.CallerAppName)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunSignalOrStart)
	audit.setCallerApp(req.CallerAppName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			audit.setWorkflowKey(out.WorkflowKey)
			if out.Run != nil {
				audit.setWorkflowTarget(out.Run.Target)
			}
		}
		audit.finish(ctx, err)
		if err != nil {
			logWorkflowSignalOrStartFailure(ctx, req, phase, targetAuthFailure, err)
		}
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	phase = "validate_workflow_key"
	workflowKey := strings.TrimSpace(req.WorkflowKey)
	if workflowKey == "" {
		return nil, ErrWorkflowKeyRequired
	}
	phase = "resolve_provider_target"
	var provider coreworkflow.Provider
	providerName, provider, target, err = m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerAppName)
	if err != nil {
		if failure, ok := workflowTargetAuthorizationFailure(err); ok {
			phase = "authorize_target"
			targetAuthFailure = failure
			audit.setProvider(providerName)
			audit.setWorkflowTargetAuthorizationFailure(req.Target, *failure)
		}
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	phase = "normalize_signal"
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}

	phase = "provider_signal_or_start"
	resp, err := provider.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{
		WorkflowKey:    workflowKey,
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		DefinitionID:   strings.TrimSpace(req.DefinitionID),
		Signal:         signal,
	})
	if err != nil {
		return nil, err
	}
	phase = "bind_response"
	return managedSignalResponse(providerName, provider, resp)
}

func logWorkflowSignalOrStartFailure(ctx context.Context, req RunSignalOrStart, phase string, targetAuthFailure *targetAuthorizationFailure, err error) {
	if err == nil {
		return
	}
	attrs := []any{
		"phase", strings.TrimSpace(phase),
		"workflow_key_sha256", workflowManagerSHA256(req.WorkflowKey),
		"error_type", workflowManagerErrorType(err),
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil && strings.TrimSpace(meta.RequestID) != "" {
		attrs = append(attrs, "request_id", strings.TrimSpace(meta.RequestID))
	}
	if errorCode := workflowManagerErrorCode(err); errorCode != "" {
		attrs = append(attrs, "error_code", errorCode)
	}
	if targetAuthFailure != nil {
		attrs = appendTargetAuthorizationFailureAttrs(attrs, *targetAuthFailure)
	}
	slog.WarnContext(ctx, "workflow manager signal-or-start failed", attrs...)
}

func appendTargetAuthorizationFailureAttrs(attrs []any, failure targetAuthorizationFailure) []any {
	if component := strings.TrimSpace(failure.component); component != "" {
		attrs = append(attrs, "workflow_target_component", component)
	}
	if decision := workflowAuditTargetAuthorizationDecision(failure); decision != "" {
		attrs = append(attrs, "authorization_decision", decision)
	}
	return attrs
}

func workflowManagerSHA256(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func workflowManagerErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return observability.WorkflowGRPCCodeName(st.Code())
	}
	return ""
}

func workflowManagerErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, ErrWorkflowNotConfigured):
		return "workflow_not_configured"
	case errors.Is(err, ErrWorkflowSubjectRequired):
		return "workflow_subject_required"
	case errors.Is(err, ErrDuplicateWorkflowObjects):
		return "duplicate_workflow_objects"
	case errors.Is(err, ErrWorkflowEventMatchRequired):
		return "workflow_event_match_required"
	case errors.Is(err, ErrWorkflowEventTypeRequired):
		return "workflow_event_type_required"
	case errors.Is(err, ErrWorkflowKeyRequired):
		return "workflow_key_required"
	case errors.Is(err, ErrWorkflowSignalNameRequired):
		return "workflow_signal_name_required"
	case errors.Is(err, invocation.ErrNotAuthenticated):
		return "not_authenticated"
	case errors.Is(err, invocation.ErrAuthorizationDenied):
		return "authorization_denied"
	case errors.Is(err, invocation.ErrInvalidInvocation):
		return "invalid_invocation"
	case errors.Is(err, core.ErrNotFound):
		return "not_found"
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return "grpc_status"
	}
	return "unknown"
}

func (m *Manager) findRun(ctx context.Context, runID, providerSelection string) (*ManagedRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, core.ErrNotFound
	}
	if providerSelection = strings.TrimSpace(providerSelection); providerSelection != "" {
		providerName, provider, err := m.resolveProviderSelection(providerSelection)
		if err != nil {
			return nil, err
		}
		run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{RunID: runID})
		if err != nil {
			return nil, err
		}
		return &ManagedRun{ProviderName: providerName, Run: run, provider: provider}, nil
	}
	var match *ManagedRun
	var firstErr error
	for _, providerName := range m.providerNames() {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{RunID: runID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateWorkflowObjects, runID)
		}
		match = &ManagedRun{ProviderName: strings.TrimSpace(providerName), Run: run, provider: provider}
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, core.ErrNotFound
}

func (m *Manager) requireOwnedRun(ctx context.Context, p *principal.Principal, runID, providerSelection string) (*ManagedRun, error) {
	run, err := m.findRun(ctx, runID, providerSelection)
	if err != nil {
		return nil, err
	}
	if !m.runAccessible(ctx, p, run) {
		return nil, core.ErrNotFound
	}
	return run, nil
}

func (m *Manager) runAccessible(ctx context.Context, p *principal.Principal, run *ManagedRun) bool {
	if run == nil || run.Run == nil {
		return false
	}
	return workflowActorOwnedBy(run.Run.CreatedBy, p) && m.allowStoredTarget(ctx, p, run.Run.Target)
}

func managedSignalResponse(providerName string, provider coreworkflow.Provider, resp *coreworkflow.SignalRunResponse) (*ManagedRunSignal, error) {
	if resp == nil || resp.Run == nil {
		return nil, core.ErrNotFound
	}
	workflowKey := strings.TrimSpace(resp.WorkflowKey)
	if workflowKey == "" {
		workflowKey = strings.TrimSpace(resp.Run.WorkflowKey)
	}
	return &ManagedRunSignal{
		ProviderName: strings.TrimSpace(providerName),
		Run:          resp.Run,
		Signal:       resp.Signal,
		StartedRun:   resp.StartedRun,
		WorkflowKey:  workflowKey,
		provider:     provider,
	}, nil
}
