package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type workflowActorInfo struct {
	SubjectID string `json:"subjectId,omitempty"`
}

type workflowRunEventInfo struct {
	ID              string         `json:"id,omitempty"`
	Source          string         `json:"source,omitempty"`
	SpecVersion     string         `json:"specVersion,omitempty"`
	Type            string         `json:"type,omitempty"`
	Subject         string         `json:"subject,omitempty"`
	Time            *time.Time     `json:"time,omitempty"`
	DataContentType string         `json:"dataContentType,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	Extensions      map[string]any `json:"extensions,omitempty"`
}

type workflowRunTriggerInfo struct {
	Kind         string                `json:"kind,omitempty"`
	ActivationID string                `json:"activationId,omitempty"`
	ScheduledFor *time.Time            `json:"scheduledFor,omitempty"`
	Event        *workflowRunEventInfo `json:"event,omitempty"`
}

type workflowRunInfo struct {
	ID                   string                      `json:"id"`
	Provider             string                      `json:"provider"`
	Status               string                      `json:"status,omitempty"`
	Target               workflowScheduleTargetInfo  `json:"target"`
	DefinitionID         string                      `json:"definitionId,omitempty"`
	DefinitionGeneration int64                       `json:"definitionGeneration,omitempty"`
	Input                map[string]any              `json:"input,omitempty"`
	CurrentStepID        string                      `json:"currentStepId,omitempty"`
	Steps                []workflowStepExecutionInfo `json:"steps,omitempty"`
	Trigger              *workflowRunTriggerInfo     `json:"trigger,omitempty"`
	CreatedAt            *time.Time                  `json:"createdAt,omitempty"`
	StartedAt            *time.Time                  `json:"startedAt,omitempty"`
	CompletedAt          *time.Time                  `json:"completedAt,omitempty"`
	StatusMessage        string                      `json:"statusMessage,omitempty"`
	Output               any                         `json:"output,omitempty"`
}

type workflowStepExecutionInfo struct {
	StepID        string                    `json:"stepId,omitempty"`
	Status        string                    `json:"status,omitempty"`
	Attempts      []workflowStepAttemptInfo `json:"attempts,omitempty"`
	Input         any                       `json:"input,omitempty"`
	Output        any                       `json:"output,omitempty"`
	StatusMessage string                    `json:"statusMessage,omitempty"`
	SkipReason    string                    `json:"skipReason,omitempty"`
	StartedAt     *time.Time                `json:"startedAt,omitempty"`
	CompletedAt   *time.Time                `json:"completedAt,omitempty"`
}

type workflowStepAttemptInfo struct {
	ID             string     `json:"id,omitempty"`
	Status         string     `json:"status,omitempty"`
	IdempotencyKey string     `json:"idempotencyKey,omitempty"`
	Input          any        `json:"input,omitempty"`
	Output         any        `json:"output,omitempty"`
	StatusMessage  string     `json:"statusMessage,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

type workflowRunCancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

type workflowRunListResponse struct {
	Runs          []workflowRunInfo `json:"runs"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
}

func (s *Server) listGlobalWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	if !s.requireGlobalWorkflowRunAccess(w, r, p, "ListRuns") {
		return
	}
	listReq, ok := workflowRunListRequestFromQuery(w, r)
	if !ok {
		return
	}
	resp, err := s.workflowSchedules.ListRuns(r.Context(), p, s.workflowProviderName, listReq)
	if err != nil {
		s.writeWorkflowRunManagerError(w, r, "", err)
		return
	}
	if resp == nil {
		resp = &workflowmanager.ListRunsResponse{}
	}
	out := make([]workflowRunInfo, 0, len(resp.Runs))
	for _, managed := range resp.Runs {
		if managed == nil || managed.Run == nil {
			continue
		}
		out = append(out, workflowRunInfoFromManaged(managed))
	}
	writeJSON(w, http.StatusOK, workflowRunListResponse{
		Runs:          out,
		NextPageToken: strings.TrimSpace(resp.NextPageToken),
	})
}

func workflowRunListRequestFromQuery(w http.ResponseWriter, r *http.Request) (coreworkflow.ListRunsRequest, bool) {
	query := r.URL.Query()
	pageSize, ok := parseOptionalIntQuery(w, queryValue(query, "pageSize", "page_size"), "pageSize")
	if !ok {
		return coreworkflow.ListRunsRequest{}, false
	}
	status, ok := workflowRunStatusFromQuery(w, strings.TrimSpace(query.Get("status")))
	if !ok {
		return coreworkflow.ListRunsRequest{}, false
	}
	return coreworkflow.ListRunsRequest{
		PageSize:  pageSize,
		PageToken: strings.TrimSpace(queryValue(query, "pageToken", "page_token")),
		TargetApp: strings.TrimSpace(query.Get("app")),
		Status:    status,
	}, true
}

func queryValue(values url.Values, names ...string) string {
	for _, name := range names {
		if raw := strings.TrimSpace(values.Get(name)); raw != "" {
			return raw
		}
	}
	return ""
}

func parseOptionalIntQuery(w http.ResponseWriter, raw, name string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s must be an integer", name))
		return 0, false
	}
	return value, true
}

func workflowRunStatusFromQuery(w http.ResponseWriter, raw string) (coreworkflow.RunStatus, bool) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", true
	case string(coreworkflow.RunStatusPending):
		return coreworkflow.RunStatusPending, true
	case string(coreworkflow.RunStatusRunning):
		return coreworkflow.RunStatusRunning, true
	case string(coreworkflow.RunStatusSucceeded):
		return coreworkflow.RunStatusSucceeded, true
	case string(coreworkflow.RunStatusFailed):
		return coreworkflow.RunStatusFailed, true
	case string(coreworkflow.RunStatusCanceled):
		return coreworkflow.RunStatusCanceled, true
	default:
		writeError(w, http.StatusBadRequest, "status must be pending, running, succeeded, failed, or canceled")
		return "", false
	}
}

func (s *Server) getGlobalWorkflowRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	if !s.requireGlobalWorkflowRunAccess(w, r, p, "GetRun") {
		return
	}
	managed, err := s.workflowSchedules.GetRun(r.Context(), p, s.workflowProviderName, chi.URLParam(r, "runID"))
	if err != nil {
		s.writeWorkflowRunManagerError(w, r, chi.URLParam(r, "runID"), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowRunInfoFromManaged(managed))
}

func (s *Server) cancelGlobalWorkflowRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	if !s.requireGlobalWorkflowRunAccess(w, r, p, "CancelRun") {
		return
	}
	var req workflowRunCancelRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	managed, err := s.workflowSchedules.CancelRun(r.Context(), p, s.workflowProviderName, chi.URLParam(r, "runID"), req.Reason)
	if err != nil {
		s.writeWorkflowRunManagerError(w, r, chi.URLParam(r, "runID"), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowRunInfoFromManaged(managed))
}

func (s *Server) requireAuthorizationProvider(w http.ResponseWriter) bool {
	if s.authorization == nil {
		writeError(w, http.StatusPreconditionFailed, "authorization provider is not configured")
		return false
	}
	return true
}

func (s *Server) authorizationHTTPContext(_ http.ResponseWriter, r *http.Request) (context.Context, bool) {
	return invocation.WithEntry(r.Context(), invocation.EntryHTTP), true
}

func (s *Server) requireGlobalWorkflowRunAccess(w http.ResponseWriter, r *http.Request, p *principal.Principal, action string) bool {
	if !s.requireAuthorizationProvider(w) {
		return false
	}
	subjectID, err := principal.ResolveCredentialSubjectID(r.Context(), s.users, p)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return false
	}
	ctx, ok := s.authorizationHTTPContext(w, r)
	if !ok {
		return false
	}
	allowed, err := invocation.CheckSubjectAccess(ctx, s.authorization, invocation.SubjectAccessRequest(subjectID, action, &proto.Resource{
		Type: "workflow",
		Id:   strings.TrimSpace(s.workflowProviderName),
	}))
	if err != nil {
		slog.ErrorContext(r.Context(), "workflow REST authorization check failed", "action", action, "error", err)
		writeError(w, http.StatusForbidden, invocation.ErrAuthorizationDenied.Error())
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, invocation.ErrAuthorizationDenied.Error())
		return false
	}
	return true
}

func workflowRunInfoFromManaged(managed *workflowmanager.ManagedRun) workflowRunInfo {
	if managed == nil {
		return workflowRunInfo{}
	}
	return workflowRunInfoFromCore(managed.Run, strings.TrimSpace(managed.ProviderName))
}

func workflowRunInfoFromCore(run *coreworkflow.Run, providerName string) workflowRunInfo {
	info := workflowRunInfo{Provider: providerName}
	if run == nil {
		return info
	}
	info.ID = run.ID
	info.Status = strings.TrimSpace(string(run.Status))
	info.CreatedAt = run.CreatedAt
	info.StartedAt = run.StartedAt
	info.CompletedAt = run.CompletedAt
	info.StatusMessage = run.StatusMessage
	info.Output = run.Output
	info.Target = workflowScheduleTargetInfoFromCore(run.Target)
	info.DefinitionID = strings.TrimSpace(run.DefinitionID)
	info.DefinitionGeneration = run.DefinitionGeneration
	info.Input = maps.Clone(run.Input)
	info.CurrentStepID = strings.TrimSpace(run.CurrentStepID)
	info.Steps = workflowStepExecutionsInfoFromCore(run.Steps)
	info.Trigger = workflowRunTriggerInfoFromCore(run.Trigger)
	return info
}

func workflowStepExecutionsInfoFromCore(steps []coreworkflow.StepExecution) []workflowStepExecutionInfo {
	if len(steps) == 0 {
		return nil
	}
	out := make([]workflowStepExecutionInfo, 0, len(steps))
	for i := range steps {
		step := steps[i]
		out = append(out, workflowStepExecutionInfo{
			StepID:        strings.TrimSpace(step.StepID),
			Status:        strings.TrimSpace(string(step.Status)),
			Attempts:      workflowStepAttemptsInfoFromCore(step.Attempts),
			Input:         step.Input,
			Output:        step.Output,
			StatusMessage: strings.TrimSpace(step.StatusMessage),
			SkipReason:    strings.TrimSpace(step.SkipReason),
			StartedAt:     step.StartedAt,
			CompletedAt:   step.CompletedAt,
		})
	}
	return out
}

func workflowStepAttemptsInfoFromCore(attempts []coreworkflow.StepAttempt) []workflowStepAttemptInfo {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]workflowStepAttemptInfo, 0, len(attempts))
	for i := range attempts {
		attempt := attempts[i]
		out = append(out, workflowStepAttemptInfo{
			ID:             strings.TrimSpace(attempt.ID),
			Status:         strings.TrimSpace(string(attempt.Status)),
			IdempotencyKey: strings.TrimSpace(attempt.IdempotencyKey),
			Input:          attempt.Input,
			Output:         attempt.Output,
			StatusMessage:  strings.TrimSpace(attempt.StatusMessage),
			StartedAt:      attempt.StartedAt,
			CompletedAt:    attempt.CompletedAt,
		})
	}
	return out
}

func workflowRunTriggerInfoFromCore(trigger coreworkflow.RunTrigger) *workflowRunTriggerInfo {
	switch {
	case trigger.Schedule != nil:
		return &workflowRunTriggerInfo{
			Kind:         "schedule",
			ActivationID: trigger.Schedule.ActivationID,
			ScheduledFor: trigger.Schedule.ScheduledFor,
		}
	case trigger.Event != nil:
		return &workflowRunTriggerInfo{
			Kind:         "event",
			ActivationID: trigger.Event.ActivationID,
			Event: &workflowRunEventInfo{
				ID:              trigger.Event.Event.ID,
				Source:          trigger.Event.Event.Source,
				SpecVersion:     trigger.Event.Event.SpecVersion,
				Type:            trigger.Event.Event.Type,
				Subject:         trigger.Event.Event.Subject,
				Time:            trigger.Event.Event.Time,
				DataContentType: trigger.Event.Event.DataContentType,
				Data:            maps.Clone(trigger.Event.Event.Data),
				Extensions:      maps.Clone(trigger.Event.Event.Extensions),
			},
		}
	case trigger.Manual:
		return &workflowRunTriggerInfo{Kind: "manual"}
	default:
		return nil
	}
}

func workflowActorInfoFromSubjectID(subjectID string) *workflowActorInfo {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil
	}
	return &workflowActorInfo{SubjectID: subjectID}
}

func (s *Server) writeWorkflowRunManagerError(w http.ResponseWriter, r *http.Request, runID string, err error) {
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, invocation.ErrInvalidInvocation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, core.ErrNotFound):
		s.writeWorkflowRunProviderError(r.Context(), w, runID, err)
	default:
		s.writeWorkflowRunProviderError(r.Context(), w, runID, err)
	}
}

func (s *Server) writeWorkflowRunProviderError(ctx context.Context, w http.ResponseWriter, runID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow run %q not found", runID))
	case grpcstatus.Code(err) == codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, grpcstatus.Convert(err).Message())
	default:
		slog.ErrorContext(ctx, "workflow run provider error",
			"run_id", runID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "workflow run request failed")
	}
}
