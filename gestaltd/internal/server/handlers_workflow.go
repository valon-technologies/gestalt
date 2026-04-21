package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type workflowScheduleTargetRequest struct {
	Plugin     string         `json:"plugin,omitempty"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowScheduleUpsertRequest struct {
	Provider string                        `json:"provider,omitempty"`
	Cron     string                        `json:"cron"`
	Timezone string                        `json:"timezone,omitempty"`
	Target   workflowScheduleTargetRequest `json:"target"`
	Paused   bool                          `json:"paused,omitempty"`
}

type workflowScheduleTargetInfo struct {
	Plugin     string         `json:"plugin,omitempty"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowScheduleInfo struct {
	ID        string                     `json:"id"`
	Provider  string                     `json:"provider"`
	Cron      string                     `json:"cron"`
	Timezone  string                     `json:"timezone,omitempty"`
	Target    workflowScheduleTargetInfo `json:"target"`
	Paused    bool                       `json:"paused"`
	CreatedAt *time.Time                 `json:"createdAt,omitempty"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
	NextRunAt *time.Time                 `json:"nextRunAt,omitempty"`
}

func (s *Server) listGlobalWorkflowSchedules(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	refs, ok := s.listOwnedWorkflowScheduleExecutionRefs(r.Context(), w, p)
	if !ok {
		return
	}
	out := make([]workflowScheduleInfo, 0)
	for _, ref := range refs {
		if !s.allowWorkflowScheduleTarget(r.Context(), p, ref.Target) {
			continue
		}
		scheduleID := workflowScheduleIDFromExecutionRefID(ref.ID)
		if scheduleID == "" {
			continue
		}
		provider, ok := s.resolveWorkflowScheduleProviderByName(w, ref.ProviderName)
		if !ok {
			return
		}
		schedule, err := provider.GetSchedule(r.Context(), coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			s.writeWorkflowScheduleProviderError(r.Context(), w, strings.TrimSpace(ref.Target.PluginName), scheduleID, err)
			return
		}
		if !workflowScheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
			continue
		}
		out = append(out, workflowScheduleInfoFromCore(schedule, strings.TrimSpace(ref.ProviderName)))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != nil && out[j].CreatedAt != nil && !out[i].CreatedAt.Equal(*out[j].CreatedAt) {
			return out[i].CreatedAt.Before(*out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}

	var req workflowScheduleUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pluginName := strings.TrimSpace(req.Target.Plugin)
	if pluginName == "" {
		writeError(w, http.StatusBadRequest, "workflow target plugin is required")
		return
	}
	providerName, provider, ok := s.resolveWorkflowScheduleProvider(w, strings.TrimSpace(req.Provider))
	if !ok {
		return
	}

	target, err := s.resolveWorkflowScheduleTarget(r.Context(), pluginName, p, req.Target)
	if err != nil {
		s.writeWorkflowScheduleTargetError(w, r, pluginName, strings.TrimSpace(req.Target.Operation), err)
		return
	}

	scheduleID := uuid.NewString()
	executionRefID := workflowScheduleExecutionRefID(scheduleID)
	ref, err := s.putWorkflowExecutionRef(r.Context(), executionRefID, providerName, target, p)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to store workflow execution reference",
			"execution_ref_id", executionRefID,
			"provider", providerName,
			"plugin", pluginName,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to store workflow execution reference")
		return
	}

	schedule, err := provider.UpsertSchedule(r.Context(), coreworkflow.UpsertScheduleRequest{
		ScheduleID:   scheduleID,
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		s.revokeWorkflowExecutionRef(r.Context(), ref)
		s.writeWorkflowScheduleProviderError(r.Context(), w, pluginName, scheduleID, err)
		return
	}
	writeJSON(w, http.StatusCreated, workflowScheduleInfoFromCore(schedule, providerName))
}

func (s *Server) getGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	schedule, _, providerName, _, ok := s.requireOwnedWorkflowScheduleGlobal(r.Context(), w, chi.URLParam(r, "scheduleID"), p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromCore(schedule, providerName))
}

func (s *Server) updateGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	scheduleID := chi.URLParam(r, "scheduleID")
	existing, ref, currentProviderName, currentProvider, ok := s.requireOwnedWorkflowScheduleGlobal(r.Context(), w, scheduleID, p)
	if !ok {
		return
	}

	var req workflowScheduleUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pluginName := strings.TrimSpace(req.Target.Plugin)
	if pluginName == "" {
		writeError(w, http.StatusBadRequest, "workflow target plugin is required")
		return
	}
	nextProviderName, nextProvider, ok := s.resolveWorkflowScheduleProvider(w, strings.TrimSpace(req.Provider))
	if !ok {
		return
	}
	target, err := s.resolveWorkflowScheduleTarget(r.Context(), pluginName, p, req.Target)
	if err != nil {
		s.writeWorkflowScheduleTargetError(w, r, pluginName, strings.TrimSpace(req.Target.Operation), err)
		return
	}

	executionRefID := workflowScheduleExecutionRefID(strings.TrimSpace(existing.ID))
	nextRef, err := s.putWorkflowExecutionRef(r.Context(), executionRefID, nextProviderName, target, p)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to store workflow execution reference",
			"execution_ref_id", executionRefID,
			"provider", nextProviderName,
			"plugin", pluginName,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to store workflow execution reference")
		return
	}
	schedule, err := nextProvider.UpsertSchedule(r.Context(), coreworkflow.UpsertScheduleRequest{
		ScheduleID:   strings.TrimSpace(existing.ID),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		s.revokeWorkflowExecutionRef(r.Context(), nextRef)
		s.writeWorkflowScheduleProviderError(r.Context(), w, pluginName, strings.TrimSpace(existing.ID), err)
		return
	}
	if currentProviderName != nextProviderName {
		if err := currentProvider.DeleteSchedule(r.Context(), coreworkflow.DeleteScheduleRequest{
			ScheduleID: strings.TrimSpace(existing.ID),
		}); err != nil {
			_ = nextProvider.DeleteSchedule(r.Context(), coreworkflow.DeleteScheduleRequest{
				ScheduleID: strings.TrimSpace(existing.ID),
			})
			s.revokeWorkflowExecutionRef(r.Context(), nextRef)
			s.writeWorkflowScheduleProviderError(r.Context(), w, strings.TrimSpace(existing.Target.PluginName), strings.TrimSpace(existing.ID), err)
			return
		}
	}
	if ref != nil && ref.ID != "" && ref.ID != executionRefID {
		s.revokeWorkflowExecutionRef(r.Context(), ref)
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromCore(schedule, nextProviderName))
}

func (s *Server) deleteGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	schedule, ref, _, provider, ok := s.requireOwnedWorkflowScheduleGlobal(r.Context(), w, chi.URLParam(r, "scheduleID"), p)
	if !ok {
		return
	}
	if err := provider.DeleteSchedule(r.Context(), coreworkflow.DeleteScheduleRequest{
		ScheduleID: strings.TrimSpace(schedule.ID),
	}); err != nil {
		s.writeWorkflowScheduleProviderError(r.Context(), w, strings.TrimSpace(schedule.Target.PluginName), strings.TrimSpace(schedule.ID), err)
		return
	}
	if ref != nil && ref.ID != "" {
		s.revokeWorkflowExecutionRef(r.Context(), ref)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) pauseGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	schedule, _, providerName, provider, ok := s.requireOwnedWorkflowScheduleGlobal(r.Context(), w, chi.URLParam(r, "scheduleID"), p)
	if !ok {
		return
	}
	value, err := provider.PauseSchedule(r.Context(), coreworkflow.PauseScheduleRequest{
		ScheduleID: strings.TrimSpace(schedule.ID),
	})
	if err != nil {
		s.writeWorkflowScheduleProviderError(r.Context(), w, strings.TrimSpace(schedule.Target.PluginName), strings.TrimSpace(schedule.ID), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromCore(value, providerName))
}

func (s *Server) resumeGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	schedule, _, providerName, provider, ok := s.requireOwnedWorkflowScheduleGlobal(r.Context(), w, chi.URLParam(r, "scheduleID"), p)
	if !ok {
		return
	}
	value, err := provider.ResumeSchedule(r.Context(), coreworkflow.ResumeScheduleRequest{
		ScheduleID: strings.TrimSpace(schedule.ID),
	})
	if err != nil {
		s.writeWorkflowScheduleProviderError(r.Context(), w, strings.TrimSpace(schedule.Target.PluginName), strings.TrimSpace(schedule.ID), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromCore(value, providerName))
}

func (s *Server) resolveWorkflowScheduleActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	if strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing subject")
		return nil, false
	}
	return p, true
}

func (s *Server) resolveWorkflowScheduleProvider(w http.ResponseWriter, providerName string) (string, coreworkflow.Provider, bool) {
	if s.workflow == nil {
		writeError(w, http.StatusPreconditionFailed, "workflow is not configured")
		return "", nil, false
	}
	providerName, provider, err := s.workflow.ResolveProviderSelection(providerName)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return "", nil, false
	}
	return providerName, provider, true
}

func (s *Server) resolveWorkflowScheduleProviderByName(w http.ResponseWriter, providerName string) (coreworkflow.Provider, bool) {
	if s.workflow == nil {
		writeError(w, http.StatusPreconditionFailed, "workflow is not configured")
		return nil, false
	}
	provider, err := s.workflow.ResolveProvider(strings.TrimSpace(providerName))
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return nil, false
	}
	return provider, true
}

func (s *Server) resolveWorkflowScheduleTarget(
	ctx context.Context,
	pluginName string,
	p *principal.Principal,
	target workflowScheduleTargetRequest,
) (coreworkflow.Target, error) {
	prov, err := s.providers.Get(pluginName)
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
	if !s.allowProviderContext(ctx, p, pluginName) || !s.allowOperationContext(ctx, p, pluginName, operation) {
		return coreworkflow.Target{}, invocation.ErrAuthorizationDenied
	}

	connection := strings.TrimSpace(target.Connection)
	if connection != "" && !safeParamValue.MatchString(connection) {
		return coreworkflow.Target{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = config.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(target.Instance)
	if instance != "" && !safeInstanceValue.MatchString(instance) {
		return coreworkflow.Target{}, fmt.Errorf("instance name contains invalid characters")
	}

	access := s.providerAccessContextWithContext(ctx, p, pluginName)
	ctx = invocation.WithAccessContext(ctx, access)
	var resolver invocation.TokenResolver
	if tr, ok := s.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	boundConnections, sessionInstance := s.boundSessionCatalogConnections(pluginName, p, connection, instance)
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, pluginName, resolver, p, operation, boundConnections, sessionInstance)
	if err != nil {
		return coreworkflow.Target{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if s.authorizer != nil && !s.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
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

func (s *Server) requireOwnedWorkflowScheduleGlobal(
	ctx context.Context,
	w http.ResponseWriter,
	scheduleID string,
	p *principal.Principal,
) (*coreworkflow.Schedule, *coreworkflow.ExecutionReference, string, coreworkflow.Provider, bool) {
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" {
		writeError(w, http.StatusBadRequest, "scheduleID is required")
		return nil, nil, "", nil, false
	}
	ref, ok := s.findOwnedWorkflowScheduleExecutionRef(ctx, w, scheduleID, p)
	if !ok {
		return nil, nil, "", nil, false
	}
	if !s.allowWorkflowScheduleTarget(ctx, p, ref.Target) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow schedule %q not found", scheduleID))
		return nil, nil, "", nil, false
	}
	provider, ok := s.resolveWorkflowScheduleProviderByName(w, ref.ProviderName)
	if !ok {
		return nil, nil, "", nil, false
	}
	schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
	if err != nil {
		s.writeWorkflowScheduleProviderError(ctx, w, strings.TrimSpace(ref.Target.PluginName), scheduleID, err)
		return nil, nil, "", nil, false
	}
	if !workflowScheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow schedule %q not found", scheduleID))
		return nil, nil, "", nil, false
	}
	return schedule, ref, strings.TrimSpace(ref.ProviderName), provider, true
}

func (s *Server) listOwnedWorkflowScheduleExecutionRefs(ctx context.Context, w http.ResponseWriter, p *principal.Principal) ([]*coreworkflow.ExecutionReference, bool) {
	if s.workflowExecutionRefs == nil {
		writeError(w, http.StatusPreconditionFailed, "workflow execution refs are not configured")
		return nil, false
	}
	refs, err := s.workflowExecutionRefs.ListBySubject(ctx, strings.TrimSpace(p.SubjectID))
	if err != nil {
		slog.ErrorContext(ctx, "failed to resolve workflow schedule owner",
			"subject_id", strings.TrimSpace(p.SubjectID),
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to resolve workflow schedule owner")
		return nil, false
	}
	out := make([]*coreworkflow.ExecutionReference, 0, len(refs))
	for _, ref := range refs {
		if !workflowExecutionRefActive(ref) || !workflowExecutionRefOwnedBy(ref, p) {
			continue
		}
		out = append(out, ref)
	}
	return out, true
}

func (s *Server) findOwnedWorkflowScheduleExecutionRef(
	ctx context.Context,
	w http.ResponseWriter,
	scheduleID string,
	p *principal.Principal,
) (*coreworkflow.ExecutionReference, bool) {
	refs, ok := s.listOwnedWorkflowScheduleExecutionRefs(ctx, w, p)
	if !ok {
		return nil, false
	}
	prefix := workflowScheduleExecutionRefPrefix(scheduleID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			slog.ErrorContext(ctx, "workflow schedule matched multiple execution references",
				"schedule_id", scheduleID,
				"subject_id", strings.TrimSpace(p.SubjectID),
			)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow schedule %q matched multiple execution references", scheduleID))
			return nil, false
		}
		match = ref
	}
	if match == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow schedule %q not found", scheduleID))
		return nil, false
	}
	return match, true
}

func (s *Server) allowWorkflowScheduleTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	pluginName := strings.TrimSpace(target.PluginName)
	operation := strings.TrimSpace(target.Operation)
	if pluginName == "" || operation == "" {
		return false
	}
	if !s.allowProviderContext(ctx, p, pluginName) || !s.allowOperationContext(ctx, p, pluginName, operation) {
		return false
	}
	return principal.AllowsOperationPermission(p, pluginName, operation)
}

func workflowExecutionRefOwnedBy(ref *coreworkflow.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(p.SubjectID)
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func workflowExecutionRefActive(ref *coreworkflow.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

const workflowScheduleExecutionRefBasePrefix = "workflow_schedule:"

func workflowScheduleMatchesExecutionRef(providerName string, schedule *coreworkflow.Schedule, ref *coreworkflow.ExecutionReference) bool {
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

func (s *Server) putWorkflowExecutionRef(
	ctx context.Context,
	executionRefID string,
	providerName string,
	target coreworkflow.Target,
	p *principal.Principal,
) (*coreworkflow.ExecutionReference, error) {
	if s.workflowExecutionRefs == nil {
		return nil, fmt.Errorf("workflow execution refs are not configured")
	}
	p = principal.Canonicalized(p)
	subjectID := ""
	if p != nil {
		subjectID = strings.TrimSpace(p.SubjectID)
	}
	if subjectID == "" {
		return nil, fmt.Errorf("workflow execution ref subject is required")
	}
	return s.workflowExecutionRefs.Put(ctx, &coreworkflow.ExecutionReference{
		ID:           executionRefID,
		ProviderName: providerName,
		Target:       target,
		SubjectID:    subjectID,
		Permissions:  principal.PermissionsToAccessPermissions(p.TokenPermissions),
	})
}

func workflowScheduleExecutionRefID(scheduleID string) string {
	return workflowScheduleExecutionRefPrefix(scheduleID) + uuid.NewString()
}

func workflowScheduleExecutionRefPrefix(scheduleID string) string {
	return workflowScheduleExecutionRefBasePrefix + strings.TrimSpace(scheduleID) + ":"
}

func workflowScheduleIDFromExecutionRefID(executionRefID string) string {
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

func workflowScheduleInfoFromCore(schedule *coreworkflow.Schedule, providerName string) workflowScheduleInfo {
	info := workflowScheduleInfo{
		Provider: providerName,
	}
	if schedule == nil {
		return info
	}
	info.ID = schedule.ID
	info.Cron = schedule.Cron
	info.Timezone = schedule.Timezone
	info.Paused = schedule.Paused
	info.CreatedAt = schedule.CreatedAt
	info.UpdatedAt = schedule.UpdatedAt
	info.NextRunAt = schedule.NextRunAt
	info.Target = workflowScheduleTargetInfo{
		Plugin:     schedule.Target.PluginName,
		Operation:  schedule.Target.Operation,
		Connection: userFacingConnectionName(schedule.Target.Connection),
		Instance:   schedule.Target.Instance,
		Input:      maps.Clone(schedule.Target.Input),
	}
	return info
}

func (s *Server) revokeWorkflowExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	if s.workflowExecutionRefs == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return
	}
	cloned := *ref
	now := s.nowUTCSecond()
	cloned.RevokedAt = &now
	_, _ = s.workflowExecutionRefs.Put(ctx, &cloned)
}

func (s *Server) writeWorkflowScheduleProviderError(ctx context.Context, w http.ResponseWriter, pluginName, scheduleID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow schedule %q not found", scheduleID))
	default:
		slog.ErrorContext(ctx, "workflow schedule provider error",
			"plugin", pluginName,
			"schedule_id", scheduleID,
			"error", err,
		)
		if strings.TrimSpace(pluginName) == "" {
			writeError(w, http.StatusInternalServerError, "workflow schedule request failed")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow schedule request failed for integration %q", pluginName))
	}
}

func (s *Server) writeWorkflowScheduleTargetError(w http.ResponseWriter, r *http.Request, pluginName, operation string, err error) {
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoToken),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeInvocationError(w, r, pluginName, operation, err)
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
