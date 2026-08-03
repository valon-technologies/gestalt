package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/agents/agentroute"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
)

const (
	contractAgentEventPageSize   = 100
	contractAgentStreamHeartbeat = 15 * time.Second
	agentIdempotencyKeyHeader    = "Idempotency-Key"
)

var contractProtoJSONMarshal = protojson.MarshalOptions{
	UseProtoNames:   false,
	EmitUnpopulated: false,
}

var contractProtoJSONUnmarshal = protojson.UnmarshalOptions{
	DiscardUnknown: false,
}

func (s *Server) createContractAgent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	req := &proto.CreateAgentRequest{}
	if !decodeContractProtoJSON(w, r, req) {
		return
	}
	if !rejectPublicRequestContext(w, req.GetContext()) {
		return
	}
	req.Context = nil
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.GetIdempotencyKey())
	if !ok {
		return
	}
	req.IdempotencyKey = idempotencyKey
	resource, err := s.agentContract.CreateAgent(r.Context(), p, req)
	if err != nil {
		s.writeContractAgentError(w, r, "agent", "", err)
		return
	}
	writeContractProtoJSON(w, http.StatusCreated, resource)
}

func (s *Server) getContractAgent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	resource, err := s.agentContract.GetAgent(r.Context(), p, &proto.GetAgentRequest{AgentId: agentID})
	if err != nil {
		s.writeContractAgentError(w, r, "agent", agentID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, resource)
}

func (s *Server) listContractAgents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	pageSize, ok := contractPageSizeFromQuery(w, r)
	if !ok {
		return
	}
	state, ok := contractSessionStateFromQuery(w, r.URL.Query().Get("state"))
	if !ok {
		return
	}
	response, err := s.agentContract.ListAgents(r.Context(), p, &proto.ListAgentsRequest{
		AgentIds: r.URL.Query()["agentId"],
		State:    state,
		PageSize: pageSize,
		PageToken: strings.TrimSpace(
			r.URL.Query().Get("pageToken"),
		),
	})
	if err != nil {
		s.writeContractAgentError(w, r, "agent", "", err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, response)
}

func (s *Server) archiveContractAgent(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	resource, err := s.agentContract.ArchiveAgent(r.Context(), p, &proto.ArchiveAgentRequest{
		AgentId:        agentID,
		IdempotencyKey: strings.TrimSpace(r.Header.Get(agentIdempotencyKeyHeader)),
	})
	if err != nil {
		s.writeContractAgentError(w, r, "agent", agentID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, resource)
}

func (s *Server) updateContractAgentConfig(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	req := &proto.CreateAgentConfigRevisionRequest{}
	if !decodeContractProtoJSON(w, r, req) {
		return
	}
	if !rejectPublicRequestContext(w, req.GetContext()) {
		return
	}
	req.Context = nil
	req.AgentId = strings.TrimSpace(chi.URLParam(r, "agentID"))
	expected := strings.TrimSpace(r.Header.Get("If-Match"))
	if expected != "" {
		expected = strings.Trim(expected, `"`)
		if bodyExpected := strings.TrimSpace(req.GetExpectedRevision()); bodyExpected != "" && bodyExpected != expected {
			writeError(w, http.StatusBadRequest, "If-Match and expectedRevision must match")
			return
		}
		req.ExpectedRevision = expected
	}
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.GetIdempotencyKey())
	if !ok {
		return
	}
	req.IdempotencyKey = idempotencyKey
	revision, err := s.agentContract.CreateConfigRevision(r.Context(), p, req)
	if err != nil {
		s.writeContractAgentError(w, r, "agent config", req.GetAgentId(), err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, revision)
}

func (s *Server) createContractAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	req := &proto.CreateAgentRunRequest{}
	if !decodeContractProtoJSON(w, r, req) {
		return
	}
	if !rejectPublicRequestContext(w, req.GetContext()) {
		return
	}
	req.Context = nil
	req.AgentId = strings.TrimSpace(chi.URLParam(r, "agentID"))
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.GetIdempotencyKey())
	if !ok {
		return
	}
	req.IdempotencyKey = idempotencyKey
	run, err := s.agentContract.CreateRun(r.Context(), p, req)
	if err != nil {
		s.writeContractAgentError(w, r, "run", "", err)
		return
	}
	writeContractProtoJSON(w, http.StatusCreated, run)
}

func (s *Server) getContractAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	agentID, runID := contractAgentRunPath(r)
	run, err := s.agentContract.GetRun(r.Context(), p, &proto.GetAgentRunRequest{
		AgentId: agentID,
		RunId:   runID,
	})
	if err != nil {
		s.writeContractAgentError(w, r, "run", runID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, run)
}

func (s *Server) listContractAgentRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	pageSize, ok := contractPageSizeFromQuery(w, r)
	if !ok {
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	response, err := s.agentContract.ListRuns(r.Context(), p, &proto.ListAgentRunsRequest{
		AgentId:  agentID,
		PageSize: pageSize,
		PageToken: strings.TrimSpace(
			r.URL.Query().Get("pageToken"),
		),
	})
	if err != nil {
		s.writeContractAgentError(w, r, "agent", agentID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, response)
}

func (s *Server) cancelContractAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	req := &proto.CancelAgentRunRequest{}
	if !decodeContractProtoJSON(w, r, req) {
		return
	}
	if !rejectPublicRequestContext(w, req.GetContext()) {
		return
	}
	req.Context = nil
	req.AgentId, req.RunId = contractAgentRunPath(r)
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.GetIdempotencyKey())
	if !ok {
		return
	}
	req.IdempotencyKey = idempotencyKey
	run, err := s.agentContract.CancelRun(r.Context(), p, req)
	if err != nil {
		s.writeContractAgentError(w, r, "run", req.GetRunId(), err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, run)
}

func (s *Server) contractAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	agentID, runID := contractAgentRunPath(r)
	pageSize, ok := contractPageSizeFromQuery(w, r)
	if !ok {
		return
	}
	if pageSize == 0 {
		pageSize = contractAgentEventPageSize
	}
	after := strings.TrimSpace(r.URL.Query().Get("after"))
	if after == "" {
		after = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		response, err := s.agentContract.ListRunEvents(r.Context(), p, &proto.ListAgentRunEventsRequest{
			AgentId:     agentID,
			RunId:       runID,
			AfterCursor: after,
			PageSize:    pageSize,
		})
		if err != nil {
			s.writeContractAgentError(w, r, "run", runID, err)
			return
		}
		writeContractProtoJSON(w, http.StatusOK, response)
		return
	}
	s.streamContractAgentRunEvents(w, r, p, agentID, runID, after, pageSize)
}

func (s *Server) streamContractAgentRunEvents(
	w http.ResponseWriter,
	r *http.Request,
	p *principal.Principal,
	agentID string,
	runID string,
	after string,
	pageSize int32,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(contractAgentStreamHeartbeat)
	defer heartbeat.Stop()
	for {
		response, err := s.agentContract.ListRunEvents(r.Context(), p, &proto.ListAgentRunEventsRequest{
			AgentId:     agentID,
			RunId:       runID,
			AfterCursor: after,
			PageSize:    pageSize,
		})
		if err != nil {
			writeContractSSEError(w, flusher, err)
			return
		}
		for _, event := range response.GetEvents() {
			data, err := contractProtoJSONMarshal.Marshal(event)
			if err != nil {
				writeContractSSEError(w, flusher, err)
				return
			}
			eventName := strings.ToLower(strings.TrimPrefix(event.GetType().String(), "AGENT_RUN_EVENT_TYPE_"))
			if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.GetCursor(), eventName, data); err != nil {
				return
			}
			after = event.GetCursor()
			flusher.Flush()
			if contractRunEventTerminal(event.GetType()) {
				return
			}
		}
		if len(response.GetEvents()) == 0 {
			run, err := s.agentContract.GetRun(r.Context(), p, &proto.GetAgentRunRequest{
				AgentId: agentID,
				RunId:   runID,
			})
			if err != nil {
				writeContractSSEError(w, flusher, err)
				return
			}
			if contractRunStatusTerminal(run.GetStatus()) {
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) listContractAgentRunInteractions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	pageSize, ok := contractPageSizeFromQuery(w, r)
	if !ok {
		return
	}
	state, ok := contractInteractionStateFromQuery(w, r.URL.Query().Get("state"))
	if !ok {
		return
	}
	agentID, runID := contractAgentRunPath(r)
	response, err := s.agentContract.ListRunInteractions(r.Context(), p, &proto.ListAgentRunInteractionsRequest{
		AgentId:  agentID,
		RunId:    runID,
		State:    state,
		PageSize: pageSize,
		PageToken: strings.TrimSpace(
			r.URL.Query().Get("pageToken"),
		),
	})
	if err != nil {
		s.writeContractAgentError(w, r, "run", runID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, response)
}

func (s *Server) getContractAgentRunInteraction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	agentID, runID := contractAgentRunPath(r)
	interactionID := strings.TrimSpace(chi.URLParam(r, "interactionID"))
	interaction, err := s.agentContract.GetRunInteraction(r.Context(), p, &proto.GetAgentRunInteractionRequest{
		AgentId:       agentID,
		RunId:         runID,
		InteractionId: interactionID,
	})
	if err != nil {
		s.writeContractAgentError(w, r, "interaction", interactionID, err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, interaction)
}

func (s *Server) resolveContractAgentRunInteraction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveContractAgentActor(w, r)
	if !ok {
		return
	}
	req := &proto.ResolveAgentRunInteractionRequest{}
	if !decodeContractProtoJSON(w, r, req) {
		return
	}
	if !rejectPublicRequestContext(w, req.GetContext()) {
		return
	}
	req.Context = nil
	req.AgentId, req.RunId = contractAgentRunPath(r)
	req.InteractionId = strings.TrimSpace(chi.URLParam(r, "interactionID"))
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.GetIdempotencyKey())
	if !ok {
		return
	}
	req.IdempotencyKey = idempotencyKey
	interaction, err := s.agentContract.ResolveRunInteraction(r.Context(), p, req)
	if err != nil {
		s.writeContractAgentError(w, r, "interaction", req.GetInteractionId(), err)
		return
	}
	writeContractProtoJSON(w, http.StatusOK, interaction)
}

func (s *Server) resolveContractAgentActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	if s == nil || s.agentContract == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return nil, false
	}
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	return p, true
}

func decodeContractProtoJSON(w http.ResponseWriter, r *http.Request, message gproto.Message) bool {
	if r.Body == nil {
		return true
	}
	defer func() { _ = r.Body.Close() }()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return true
	}
	if err := contractProtoJSONUnmarshal.Unmarshal(data, message); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeContractProtoJSON(w http.ResponseWriter, statusCode int, message gproto.Message) {
	data, err := contractProtoJSONMarshal.Marshal(message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode agent response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(append(data, '\n'))
}

func rejectPublicRequestContext(w http.ResponseWriter, requestContext *proto.RequestContext) bool {
	if requestContext == nil {
		return true
	}
	writeError(w, http.StatusBadRequest, "context is server-derived and cannot be provided")
	return false
}

func contractAgentRunPath(r *http.Request) (string, string) {
	return strings.TrimSpace(chi.URLParam(r, "agentID")), strings.TrimSpace(chi.URLParam(r, "runID"))
}

func contractPageSizeFromQuery(w http.ResponseWriter, r *http.Request) (int32, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("pageSize"))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		writeError(w, http.StatusBadRequest, "pageSize must be a non-negative integer")
		return 0, false
	}
	return int32(value), true
}

func contractSessionStateFromQuery(w http.ResponseWriter, raw string) (proto.AgentSessionState, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED, true
	case "active":
		return proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE, true
	case "archived":
		return proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED, true
	default:
		writeError(w, http.StatusBadRequest, "state must be active or archived")
		return proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED, false
	}
}

func contractInteractionStateFromQuery(w http.ResponseWriter, raw string) (proto.AgentInteractionState, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED, true
	case "pending":
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING, true
	case "resolved":
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED, true
	case "canceled":
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_CANCELED, true
	default:
		writeError(w, http.StatusBadRequest, "state must be pending, resolved, or canceled")
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED, false
	}
}

func resolveAgentIdempotencyKey(w http.ResponseWriter, r *http.Request, bodyValue string) (string, bool) {
	idempotencyKey := strings.TrimSpace(bodyValue)
	if headerKey := strings.TrimSpace(r.Header.Get(agentIdempotencyKeyHeader)); headerKey != "" {
		if idempotencyKey != "" && idempotencyKey != headerKey {
			writeError(w, http.StatusBadRequest, "idempotency key header and body must match")
			return "", false
		}
		idempotencyKey = headerKey
	}
	return idempotencyKey, true
}

func contractRunStatusTerminal(value proto.AgentExecutionStatus) bool {
	switch value {
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED,
		proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED,
		proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED:
		return true
	default:
		return false
	}
}

func contractRunEventTerminal(value proto.AgentRunEventType) bool {
	switch value {
	case proto.AgentRunEventType_AGENT_RUN_EVENT_TYPE_TURN_COMPLETED,
		proto.AgentRunEventType_AGENT_RUN_EVENT_TYPE_TURN_FAILED,
		proto.AgentRunEventType_AGENT_RUN_EVENT_TYPE_TURN_CANCELED:
		return true
	default:
		return false
	}
}

func writeContractSSEError(w io.Writer, flusher http.Flusher, err error) {
	message := strings.ReplaceAll(err.Error(), "\n", " ")
	_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", message)
	flusher.Flush()
}

func (s *Server) writeContractAgentError(
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	id string,
	err error,
) {
	switch {
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired),
		errors.Is(err, invocation.ErrNotAuthenticated):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, core.ErrNotFound),
		errors.Is(err, agentroute.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, id))
	case errors.Is(err, agentroute.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, invocation.ErrInvalidInvocation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, agentmanager.ErrAgentContractFeatureNotReady),
		errors.Is(err, coreagent.ErrContractUnsupported):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, agentmanager.ErrAgentRoutesNotConfigured),
		errors.Is(err, agentmanager.ErrAgentContractVersionMismatch):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case grpcstatus.Code(err) == codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, grpcstatus.Convert(err).Message())
	case grpcstatus.Code(err) == codes.NotFound:
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, id))
	case grpcstatus.Code(err) == codes.Aborted,
		grpcstatus.Code(err) == codes.AlreadyExists:
		writeError(w, http.StatusConflict, grpcstatus.Convert(err).Message())
	case grpcstatus.Code(err) == codes.FailedPrecondition:
		writeError(w, http.StatusPreconditionFailed, grpcstatus.Convert(err).Message())
	case grpcstatus.Code(err) == codes.Unavailable,
		grpcstatus.Code(err) == codes.DeadlineExceeded,
		errors.Is(err, context.DeadlineExceeded):
		slog.WarnContext(r.Context(), "agent provider unavailable", "resource", resource, "id", id, "error", err)
		writeError(w, http.StatusServiceUnavailable, "agent provider unavailable")
	default:
		slog.ErrorContext(r.Context(), "agent contract error", "resource", resource, "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "agent request failed")
	}
}
