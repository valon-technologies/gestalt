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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type agentProviderListInfo struct {
	Providers []agentProviderInfo `json:"providers"`
}

type agentProviderInfo struct {
	Name         string                         `json:"name"`
	Default      bool                           `json:"default,omitempty"`
	Capabilities *agentProviderCapabilitiesInfo `json:"capabilities,omitempty"`
}

type agentProviderCapabilitiesInfo struct {
	StreamingText             bool     `json:"streamingText,omitempty"`
	ToolCalls                 bool     `json:"toolCalls,omitempty"`
	ParallelToolCalls         bool     `json:"parallelToolCalls,omitempty"`
	Interactions              bool     `json:"interactions,omitempty"`
	ResumableTurns            bool     `json:"resumableTurns,omitempty"`
	ReasoningSummaries        bool     `json:"reasoningSummaries,omitempty"`
	SupportsPreparedWorkspace bool     `json:"supportsPreparedWorkspace,omitempty"`
	BoundedListHydration      bool     `json:"boundedListHydration,omitempty"`
	SupportedToolSources      []string `json:"supportedToolSources,omitempty"`
}

type agentHarnessResolveRequest struct {
	ProviderName string `json:"provider,omitempty"`
	HarnessName  string `json:"harness,omitempty"`
}

type agentHarnessPlanInfo struct {
	Provider         string                   `json:"provider"`
	Harness          string                   `json:"harness,omitempty"`
	Command          string                   `json:"command"`
	Args             []string                 `json:"args,omitempty"`
	Env              map[string]string        `json:"env,omitempty"`
	WorkingDirectory string                   `json:"workingDirectory,omitempty"`
	RequiredCommands []string                 `json:"requiredCommands,omitempty"`
	Install          *agentHarnessInstallInfo `json:"install,omitempty"`
}

type agentHarnessInstallInfo struct {
	Instructions string                           `json:"instructions,omitempty"`
	Commands     []agentHarnessInstallCommandInfo `json:"commands,omitempty"`
}

type agentHarnessInstallCommandInfo struct {
	Description string            `json:"description,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Shell       string            `json:"shell,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

type agentTurnEventInfo struct {
	ID         string                `json:"id"`
	TurnID     string                `json:"turnId"`
	Seq        int64                 `json:"seq"`
	Type       string                `json:"type"`
	Source     string                `json:"source"`
	Visibility string                `json:"visibility"`
	Data       map[string]any        `json:"data"`
	CreatedAt  *time.Time            `json:"createdAt"`
	Display    *agentTurnDisplayInfo `json:"display,omitempty"`
}

type agentTurnDisplayInfo struct {
	Kind      string `json:"kind,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Text      string `json:"text,omitempty"`
	Label     string `json:"label,omitempty"`
	Ref       string `json:"ref,omitempty"`
	ParentRef string `json:"parentRef,omitempty"`
	Input     any    `json:"input,omitempty"`
	Output    any    `json:"output,omitempty"`
	Error     any    `json:"error,omitempty"`
	Action    string `json:"action,omitempty"`
	Format    string `json:"format,omitempty"`
	Language  string `json:"language,omitempty"`
}

const defaultAgentTurnEventLimit = 100
const maxAgentTurnEventLimit = 1000
const agentTurnEventStreamUntilTerminal = "terminal"
const agentTurnEventStreamUntilBlockedOrTerminal = "blocked_or_terminal"
const defaultAgentTurnEventStreamHeartbeatInterval = 15 * time.Second

type agentToolRefRequest struct {
	System         string `json:"system,omitempty"`
	App            string `json:"app,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Connection     string `json:"connection,omitempty"`
	Instance       string `json:"instance,omitempty"`
	CredentialMode string `json:"credentialMode,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
}

func (s *Server) listAgentProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveAgentActor(w, r); !ok {
		return
	}
	if s == nil || s.agent == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}

	defaultProvider := ""
	if name, _, err := s.agent.ResolveProvider(r.Context(), ""); err == nil {
		defaultProvider = strings.TrimSpace(name)
	}

	names := s.agent.ProviderNames()
	out := agentProviderListInfo{Providers: make([]agentProviderInfo, 0, len(names))}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info := agentProviderInfo{
			Name:    name,
			Default: name == defaultProvider,
		}
		_, provider, err := s.agent.ResolveProvider(r.Context(), name)
		if err == nil && provider != nil {
			if caps, err := provider.GetCapabilities(r.Context(), &proto.GetAgentProviderCapabilitiesRequest{}); err == nil {
				info.Capabilities = agentProviderCapabilitiesInfoFromCore(caps)
			} else {
				slog.WarnContext(r.Context(), "agent provider capabilities unavailable", "provider", name, "error", err)
			}
		}
		out.Providers = append(out.Providers, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) resolveAgentHarness(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	if s == nil || s.agent == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}
	var req agentHarnessResolveRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	providerName, _, err := s.agent.ResolveProvider(r.Context(), strings.TrimSpace(req.ProviderName))
	if err != nil {
		s.writeAgentManagerError(w, r, "provider", "", nil, err)
		return
	}
	if !s.allowsAgentProvider(r.Context(), p, providerName) {
		s.writeAgentManagerError(w, r, "provider", providerName, nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName))
		return
	}
	entry := s.agentDefs[providerName]
	effective, err := config.ResolveProviderEntryAgentHarness(providerName, entry, req.HarnessName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	harness := effective.Harness
	writeJSON(w, http.StatusOK, agentHarnessPlanInfo{
		Provider:         effective.ProviderName,
		Harness:          effective.HarnessName,
		Command:          strings.TrimSpace(harness.Command),
		Args:             append([]string(nil), harness.Args...),
		Env:              maps.Clone(harness.Env),
		WorkingDirectory: strings.TrimSpace(harness.WorkingDirectory),
		RequiredCommands: append([]string(nil), harness.RequiredCommands...),
		Install:          agentHarnessInstallInfoFromConfig(harness.Install),
	})
}

func agentHarnessInstallInfoFromConfig(install *config.ProviderEntryHarnessInstallConfig) *agentHarnessInstallInfo {
	if install == nil {
		return nil
	}
	out := &agentHarnessInstallInfo{
		Instructions: strings.TrimSpace(install.Instructions),
		Commands:     make([]agentHarnessInstallCommandInfo, 0, len(install.Commands)),
	}
	for _, command := range install.Commands {
		out.Commands = append(out.Commands, agentHarnessInstallCommandInfo{
			Description: strings.TrimSpace(command.Description),
			Command:     strings.TrimSpace(command.Command),
			Args:        append([]string(nil), command.Args...),
			Shell:       strings.TrimSpace(command.Shell),
			Env:         maps.Clone(command.Env),
		})
	}
	if out.Instructions == "" && len(out.Commands) == 0 {
		return nil
	}
	return out
}

func (s *Server) streamAgentTurnEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	providerName := strings.TrimSpace(r.URL.Query().Get("provider"))
	turnID := chi.URLParam(r, "turnID")
	afterSeq, limit, ok := parseAgentTurnEventQuery(w, r)
	if !ok {
		return
	}
	until, ok := parseAgentTurnEventStreamUntil(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	events, err := s.agentRuns.ListTurnEvents(ctx, p, &proto.ListAgentProviderTurnEventsRequest{
		ProviderName: providerName,
		TurnId:       strings.TrimSpace(turnID),
		AfterSeq:     afterSeq,
		Limit:        int32(limit),
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	tickInterval := 250 * time.Millisecond
	if s.agentStreamHeartbeat > 0 && s.agentStreamHeartbeat < tickInterval {
		tickInterval = s.agentStreamHeartbeat
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	lastWrite := time.Now()

	writeHeartbeat := func(comment string) {
		_, _ = w.Write([]byte(": "))
		_, _ = w.Write([]byte(comment))
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
		lastWrite = time.Now()
	}

	writeEvents := func(events []*coreagent.TurnEvent) bool {
		pageFull := limit > 0 && len(events) == limit
		for _, event := range events {
			info := agentTurnEventInfoFromCore(event)
			payload, err := json.Marshal(info)
			if err != nil {
				slog.ErrorContext(ctx, "marshal agent turn event", "turn_id", turnID, "error", err)
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
			lastWrite = time.Now()
			if info.Seq > afterSeq {
				afterSeq = info.Seq
			}
		}
		return pageFull
	}

	writeStreamError := func(err error) {
		if agentTurnEventStreamContextDone(ctx, err) {
			return
		}
		slog.ErrorContext(ctx, "agent turn event stream failed", "turn_id", turnID, "error", err)
		info := agentTurnEventInfo{
			TurnID:     turnID,
			Type:       "stream.error",
			Source:     "gestaltd",
			Visibility: "public",
			Data: map[string]any{
				"message": "agent event stream failed",
			},
			Display: &agentTurnDisplayInfo{
				Kind:  "error",
				Phase: "failed",
				Text:  "agent event stream failed",
			},
		}
		payload, marshalErr := json.Marshal(info)
		if marshalErr != nil {
			slog.ErrorContext(ctx, "marshal agent turn stream error", "turn_id", turnID, "error", marshalErr)
			return
		}
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
		lastWrite = time.Now()
	}

	writeHeartbeat("stream-open")
	pageFull := writeEvents(events)
	for {
		if pageFull {
			events, err := s.agentRuns.ListTurnEvents(ctx, p, &proto.ListAgentProviderTurnEventsRequest{
				ProviderName: providerName,
				TurnId:       strings.TrimSpace(turnID),
				AfterSeq:     afterSeq,
				Limit:        int32(limit),
			})
			if err != nil {
				writeStreamError(err)
				return
			}
			pageFull = writeEvents(events)
			continue
		}
		done, err := s.agentTurnStreamDone(ctx, p, providerName, turnID, until)
		if err != nil {
			writeStreamError(err)
			return
		}
		if done {
			events, err := s.agentRuns.ListTurnEvents(ctx, p, &proto.ListAgentProviderTurnEventsRequest{
				ProviderName: providerName,
				TurnId:       strings.TrimSpace(turnID),
				AfterSeq:     afterSeq,
				Limit:        int32(limit),
			})
			if err != nil {
				writeStreamError(err)
				return
			}
			if len(events) == 0 {
				return
			}
			pageFull = writeEvents(events)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastWrite) >= s.agentStreamHeartbeat {
				writeHeartbeat("keepalive")
			}
			events, err := s.agentRuns.ListTurnEvents(ctx, p, &proto.ListAgentProviderTurnEventsRequest{
				ProviderName: providerName,
				TurnId:       strings.TrimSpace(turnID),
				AfterSeq:     afterSeq,
				Limit:        int32(limit),
			})
			if err != nil {
				writeStreamError(err)
				return
			}
			pageFull = writeEvents(events)
		}
	}
}

func agentTurnEventStreamContextDone(ctx context.Context, err error) bool {
	if err == nil || ctx == nil {
		return false
	}
	ctxErr := ctx.Err()
	if ctxErr == nil {
		return false
	}
	return errors.Is(err, ctxErr) ||
		errors.Is(err, context.Canceled) && errors.Is(ctxErr, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) && errors.Is(ctxErr, context.DeadlineExceeded)
}

func (s *Server) resolveAgentActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return nil, false
	}
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

func (s *Server) allowsAgentProvider(ctx context.Context, p *principal.Principal, providerName string) bool {
	return principal.AllowsProviderPermission(p, providerName)
}

func parseAgentTurnEventQuery(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	afterSeq := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "after must be a non-negative integer")
			return 0, 0, false
		}
		afterSeq = value
	}
	limit := defaultAgentTurnEventLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > maxAgentTurnEventLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 0 and %d", maxAgentTurnEventLimit))
			return 0, 0, false
		}
		limit = value
	}
	return afterSeq, limit, true
}

func parseAgentTurnEventStreamUntil(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("until"))
	switch value {
	case "", agentTurnEventStreamUntilTerminal:
		return agentTurnEventStreamUntilTerminal, true
	case agentTurnEventStreamUntilBlockedOrTerminal:
		return agentTurnEventStreamUntilBlockedOrTerminal, true
	default:
		writeError(w, http.StatusBadRequest, "until must be terminal or blocked_or_terminal")
		return "", false
	}
}

func (s *Server) agentTurnStreamDone(ctx context.Context, p *principal.Principal, providerName, turnID string, until string) (bool, error) {
	turn, err := s.agentRuns.GetTurn(ctx, p, &proto.GetAgentProviderTurnRequest{
		ProviderName: strings.TrimSpace(providerName),
		TurnId:       strings.TrimSpace(turnID),
	})
	if err != nil {
		return false, err
	}
	switch turn.Status {
	case coreagent.ExecutionStatusSucceeded, coreagent.ExecutionStatusFailed, coreagent.ExecutionStatusCanceled:
		return true, nil
	case coreagent.ExecutionStatusWaitingForInput:
		return until == agentTurnEventStreamUntilBlockedOrTerminal, nil
	default:
		return false, nil
	}
}

func agentTurnEventInfoFromCore(event *coreagent.TurnEvent) agentTurnEventInfo {
	if event == nil {
		return agentTurnEventInfo{}
	}
	data := maps.Clone(event.Data)
	if data == nil {
		data = map[string]any{}
	}
	return agentTurnEventInfo{
		ID:         event.ID,
		TurnID:     event.TurnID,
		Seq:        event.Seq,
		Type:       strings.TrimSpace(event.Type),
		Source:     strings.TrimSpace(event.Source),
		Visibility: strings.TrimSpace(event.Visibility),
		Data:       data,
		CreatedAt:  event.CreatedAt,
		Display:    agentTurnDisplayInfoFromCore(event.Display),
	}
}

func agentToolSourceModesInfoFromCore(modes []coreagent.ToolSourceMode) []string {
	if len(modes) == 0 {
		return nil
	}
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		value := strings.TrimSpace(string(mode))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func agentProviderCapabilitiesInfoFromCore(caps *coreagent.ProviderCapabilities) *agentProviderCapabilitiesInfo {
	if caps == nil {
		return nil
	}
	return &agentProviderCapabilitiesInfo{
		StreamingText:             caps.StreamingText,
		ToolCalls:                 caps.ToolCalls,
		ParallelToolCalls:         caps.ParallelToolCalls,
		Interactions:              caps.Interactions,
		ResumableTurns:            caps.ResumableTurns,
		ReasoningSummaries:        caps.ReasoningSummaries,
		SupportsPreparedWorkspace: caps.SupportsPreparedWorkspace,
		BoundedListHydration:      caps.BoundedListHydration,
		SupportedToolSources:      agentToolSourceModesInfoFromCore(caps.SupportedToolSources),
	}
}

func agentTurnDisplayInfoFromCore(display *coreagent.TurnDisplay) *agentTurnDisplayInfo {
	if display == nil {
		return nil
	}
	return &agentTurnDisplayInfo{
		Kind:      strings.TrimSpace(display.Kind),
		Phase:     strings.TrimSpace(display.Phase),
		Text:      display.Text,
		Label:     display.Label,
		Ref:       display.Ref,
		ParentRef: display.ParentRef,
		Input:     display.Input,
		Output:    display.Output,
		Error:     display.Error,
		Action:    strings.TrimSpace(display.Action),
		Format:    strings.TrimSpace(display.Format),
		Language:  strings.TrimSpace(display.Language),
	}
}

func firstAgentToolTarget(refs []agentToolRefRequest) (string, string) {
	for i := range refs {
		ref := refs[i]
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			return systemName, strings.TrimSpace(ref.Operation)
		}
		pluginName := strings.TrimSpace(ref.App)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" && operation == "" {
			continue
		}
		return pluginName, operation
	}
	return "", ""
}
func (s *Server) writeAgentManagerError(w http.ResponseWriter, r *http.Request, resource string, id string, toolRefs []agentToolRefRequest, err error) {
	pluginName, operation := firstAgentToolTarget(toolRefs)
	switch {
	case errors.Is(err, agentmanager.ErrAgentNotConfigured),
		errors.Is(err, agentmanager.ErrAgentProviderRequired),
		errors.Is(err, agentmanager.ErrAgentWorkflowToolsNotConfigured),
		errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported),
		errors.Is(err, agentmanager.ErrAgentSessionStartUnsupported),
		errors.Is(err, agentmanager.ErrAgentWorkspaceUnsupported):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, agentmanager.ErrAgentProviderNotAvailable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool),
		errors.Is(err, agentmanager.ErrAgentInteractionRequired),
		errors.Is(err, agentmanager.ErrAgentSessionMetadataInvalid),
		errors.Is(err, agentmanager.ErrAgentWorkspaceInvalid),
		errors.Is(err, agentmanager.ErrAgentInvalidListRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInteractionNotFound):
		writeError(w, http.StatusNotFound, "agent interaction not found")
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoCredential),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInvalidInvocation),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeAgentTargetError(w, r, pluginName, operation, err)
	case errors.Is(err, core.ErrNotFound):
		s.writeAgentProviderError(r.Context(), w, resource, id, err)
	default:
		s.writeAgentProviderError(r.Context(), w, resource, id, err)
	}
}

func (s *Server) writeAgentTargetError(w http.ResponseWriter, r *http.Request, pluginName, operation string, err error) {
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoCredential),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInvalidInvocation),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeInvocationError(w, r, pluginName, operation, err)
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) writeAgentProviderError(ctx context.Context, w http.ResponseWriter, resource string, id string, err error) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		resource = "agent resource"
	}
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, id))
	case grpcstatus.Code(err) == codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, grpcstatus.Convert(err).Message())
	case grpcstatus.Code(err) == codes.Unavailable ||
		grpcstatus.Code(err) == codes.DeadlineExceeded ||
		errors.Is(err, context.DeadlineExceeded):
		slog.WarnContext(ctx, "agent provider unavailable", "resource", resource, "id", id, "error", err)
		writeError(w, http.StatusServiceUnavailable, "agent provider unavailable")
	default:
		slog.ErrorContext(ctx, "agent provider error", "resource", resource, "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "agent request failed")
	}
}
