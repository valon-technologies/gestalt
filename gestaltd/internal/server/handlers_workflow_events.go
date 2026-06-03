package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

type workflowEventDeliverRequest struct {
	ID              string         `json:"id,omitempty"`
	Source          string         `json:"source,omitempty"`
	SpecVersion     string         `json:"specVersion,omitempty"`
	Type            string         `json:"type"`
	Subject         string         `json:"subject,omitempty"`
	Time            *time.Time     `json:"time,omitempty"`
	DataContentType string         `json:"dataContentType,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	Extensions      map[string]any `json:"extensions,omitempty"`
}

type workflowEventDeliverResponse struct {
	Status string               `json:"status"`
	Event  workflowRunEventInfo `json:"event"`
}

func (s *Server) deliverWorkflowEvent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}

	var req workflowEventDeliverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	appName := strings.TrimSpace(req.Source)
	if appName != "" && !principal.AllowsProviderPermission(p, appName) {
		writeError(w, http.StatusForbidden, invocation.ErrAuthorizationDenied.Error())
		return
	}

	event, err := s.workflowSchedules.DeliverEvent(r.Context(), p, workflowmanager.EventDeliver{
		AppName: appName,
		Event:   workflowEventFromPublishRequest(req),
	})
	if err != nil {
		s.writeWorkflowDeliverEventError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, workflowEventDeliverResponse{
		Status: "delivered",
		Event:  workflowRunEventInfoFromCore(event),
	})
}

func workflowEventFromPublishRequest(req workflowEventDeliverRequest) coreworkflow.Event {
	return coreworkflow.Event{
		ID:              strings.TrimSpace(req.ID),
		Source:          strings.TrimSpace(req.Source),
		SpecVersion:     strings.TrimSpace(req.SpecVersion),
		Type:            strings.TrimSpace(req.Type),
		Subject:         strings.TrimSpace(req.Subject),
		Time:            req.Time,
		DataContentType: strings.TrimSpace(req.DataContentType),
		Data:            maps.Clone(req.Data),
		Extensions:      maps.Clone(req.Extensions),
	}
}

func workflowRunEventInfoFromCore(event coreworkflow.Event) workflowRunEventInfo {
	return workflowRunEventInfo{
		ID:              event.ID,
		Source:          event.Source,
		SpecVersion:     event.SpecVersion,
		Type:            event.Type,
		Subject:         event.Subject,
		Time:            event.Time,
		DataContentType: event.DataContentType,
		Data:            maps.Clone(event.Data),
		Extensions:      maps.Clone(event.Extensions),
	}
}

func (s *Server) writeWorkflowDeliverEventError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowEventSourceRequired),
		errors.Is(err, workflowmanager.ErrWorkflowEventTypeRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeWorkflowDeliverEventProviderError(r.Context(), w, err)
	}
}

func (s *Server) writeWorkflowDeliverEventProviderError(ctx context.Context, w http.ResponseWriter, err error) {
	slog.ErrorContext(ctx, "workflow event delivery failed", "error", err)
	writeError(w, http.StatusInternalServerError, "workflow event delivery failed")
}
