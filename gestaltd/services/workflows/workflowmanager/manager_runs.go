package workflowmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunStart)
	audit.setCallerApp(req.CallerAppName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
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

	executionRefID := newRunExecutionRefID(workflowCreateIdempotencyScope(p, req.CallerAppName, req.IdempotencyKey), req.WorkflowKey)
	ref, createdRef, err := m.putRunExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerAppName, req.DefinitionID, req.Permissions)
	if err != nil {
		return nil, err
	}
	run, err := provider.StartRun(ctx, coreworkflow.StartRunRequest{
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:    strings.TrimSpace(req.WorkflowKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		ExecutionRef:   executionRefID,
	})
	if err != nil {
		if createdRef {
			m.revokeExecutionRef(ctx, ref)
		}
		return nil, err
	}
	if !runMatchesExecutionRef(providerName, run, ref) || strings.TrimSpace(ref.ID) != strings.TrimSpace(run.ExecutionRef) {
		if createdRef {
			m.revokeExecutionRef(ctx, ref)
		}
		return nil, core.ErrNotFound
	}
	return &ManagedRun{
		ProviderName: providerName,
		Run:          run,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
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
			if isWorkflowProviderNotFound(err) {
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

func (m *Manager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (out *ManagedRun, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunCancel)
	audit.setObjectTarget(workflowAuditTargetRun, runID, "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.GetRun(ctx, p, runID)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	run, err := existingRunProvider(value).CancelRun(ctx, coreworkflow.CancelRunRequest{
		RunID:  strings.TrimSpace(runID),
		Reason: strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, err
	}
	if !runMatchesExecutionRef(value.ProviderName, run, value.ExecutionRef) {
		return nil, core.ErrNotFound
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
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.GetRun(ctx, p, req.RunID)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}
	resp, err := existingRunProvider(value).SignalRun(ctx, coreworkflow.SignalRunRequest{
		RunID:  strings.TrimSpace(req.RunID),
		Signal: signal,
	})
	if err != nil {
		return nil, err
	}
	return m.managedSignalResponse(ctx, p, value.ProviderName, existingRunProvider(value), resp, value.ExecutionRef, signalTargetPrincipalCaller)
}

func (m *Manager) SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (out *ManagedRunSignal, err error) {
	phase := "validate_subject"
	providerName := ""
	var target coreworkflow.Target
	executionRefID := ""
	var targetAuthFailure *targetAuthorizationFailure
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunSignalOrStart)
	audit.setCallerApp(req.CallerAppName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			audit.setWorkflowKey(out.WorkflowKey)
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		if targetAuthFailure != nil {
			audit.setWorkflowTargetAuthorizationFailure(target, *targetAuthFailure)
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
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	phase = "normalize_signal"
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}

	phase = "authorize_target"
	executionRefPermissions := m.executionRefPermissions(p, target, req.CallerAppName)
	targetAuth := m.checkTargetAuthorization(ctx, executionRefPrincipal(p, executionRefPermissions), target)
	if !targetAuth.allowed {
		targetAuthFailure = &targetAuth.failure
		return nil, core.ErrNotFound
	}
	phase = "derive_execution_ref"
	executionRefID, err = signalOrStartExecutionRefID(providerName, workflowKey, target, p, req.CallerAppName, executionRefPermissions)
	if err != nil {
		return nil, err
	}
	phase = "put_execution_ref"
	ref, err := m.putSignalOrStartExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerAppName, req.DefinitionID, executionRefPermissions)
	if err != nil {
		return nil, err
	}
	phase = "provider_signal_or_start"
	resp, err := provider.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{
		WorkflowKey:    workflowKey,
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		ExecutionRef:   executionRefID,
		Signal:         signal,
	})
	if err != nil {
		return nil, err
	}
	phase = "bind_response"
	return m.managedSignalResponse(ctx, p, providerName, provider, resp, ref, signalTargetPrincipalExecutionRef)
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
	case errors.Is(err, ErrExecutionRefsNotConfigured):
		return "workflow_execution_refs_not_configured"
	case errors.Is(err, ErrWorkflowSubjectRequired):
		return "workflow_subject_required"
	case errors.Is(err, ErrDuplicateExecutionRefs):
		return "duplicate_execution_refs"
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
