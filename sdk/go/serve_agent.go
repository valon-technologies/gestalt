package gestalt

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeAgentProvider starts a gRPC server for an [AgentProvider].
func ServeAgentProvider(ctx context.Context, provider AgentProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAgent, provider))
		proto.RegisterAgentServer(srv, client.NewAgentProviderServer(agentHandler{provider: provider}))
	})
}

// agentHandler bridges the ergonomic [AgentProvider] facade onto the
// generated transport handler; wire conversion lives in the generated adapter.
// providerRPCError preserves root sentinel-error mapping.
type agentHandler struct {
	client.UnimplementedAgentProvider
	provider AgentProvider
}

func (h agentHandler) CreateSession(ctx context.Context, req *client.CreateAgentProviderSessionRequest) (*client.AgentSession, error) {
	rootReq := &CreateAgentProviderSessionRequest{
		ProviderName:       req.GetProviderName(),
		IdempotencyKey:     req.GetIdempotencyKey(),
		Model:              req.GetModel(),
		ClientRef:          req.GetClientRef(),
		Metadata:           req.GetMetadata(),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectID()),
		Subject:            clientSubjectContextToRootSubject(req.GetSubject()),
		Context:            requestContextFromContext(ctx),
		SessionStart:       clientAgentSessionStartConfigToRoot(req.GetSessionStart()),
		PreparedWorkspace:  clientAgentPreparedWorkspaceToRoot(req.GetPreparedWorkspace()),
		Workspace:          clientAgentWorkspaceToRoot(req.GetWorkspace()),
		Tools:              clientAgentToolConfigToRoot(req.GetTools()),
	}
	session, err := h.provider.CreateSession(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent create session", err)
	}
	return rootAgentSessionToClient(session), nil
}

func (h agentHandler) GetSession(ctx context.Context, req *client.GetAgentProviderSessionRequest) (*client.AgentSession, error) {
	rootReq := &GetAgentProviderSessionRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionID(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	session, err := h.provider.GetSession(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent get session", err)
	}
	return rootAgentSessionToClient(session), nil
}

func (h agentHandler) ListSessions(ctx context.Context, req *client.ListAgentProviderSessionsRequest) (*client.ListAgentProviderSessionsResponse, error) {
	rootReq := &ListAgentProviderSessionsRequest{
		ProviderName: req.GetProviderName(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
		SessionIDs:   append([]string(nil), req.GetSessionIDs()...),
		State:        AgentSessionState(req.GetState()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
	resp, err := h.provider.ListSessions(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent list sessions", err)
	}
	return rootListAgentSessionsResponseToClient(resp), nil
}

func (h agentHandler) UpdateSession(ctx context.Context, req *client.UpdateAgentProviderSessionRequest) (*client.AgentSession, error) {
	rootReq := &UpdateAgentProviderSessionRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionID(),
		ClientRef:    req.GetClientRef(),
		State:        AgentSessionState(req.GetState()),
		Metadata:     req.GetMetadata(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	session, err := h.provider.UpdateSession(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent update session", err)
	}
	return rootAgentSessionToClient(session), nil
}

func (h agentHandler) CreateTurn(ctx context.Context, req *client.CreateAgentProviderTurnRequest) (*client.AgentTurn, error) {
	if req.GetTimeoutSeconds() < 0 {
		return nil, providerRPCError("agent create turn", InvalidArgument("agent create turn timeout_seconds must not be negative"))
	}
	rootReq := &CreateAgentProviderTurnRequest{
		ProviderName:       req.GetProviderName(),
		TurnID:             req.GetTurnID(),
		SessionID:          req.GetSessionID(),
		IdempotencyKey:     req.GetIdempotencyKey(),
		Model:              req.GetModel(),
		Messages:           clientAgentMessagesToRoot(req.GetMessages()),
		Output:             clientAgentOutputToRootOutput(req.GetOutput()),
		Metadata:           req.GetMetadata(),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectID()),
		ExecutionRef:       req.GetExecutionRef(),
		Subject:            clientSubjectContextToRootSubject(req.GetSubject()),
		ModelOptions:       req.GetModelOptions(),
		Context:            requestContextFromContext(ctx),
		TimeoutSeconds:     req.GetTimeoutSeconds(),
	}
	turn, err := h.provider.CreateTurn(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent create turn", err)
	}
	return rootAgentTurnToClient(turn), nil
}

func (h agentHandler) GetTurn(ctx context.Context, req *client.GetAgentProviderTurnRequest) (*client.AgentTurn, error) {
	rootReq := &GetAgentProviderTurnRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnID(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	turn, err := h.provider.GetTurn(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent get turn", err)
	}
	return rootAgentTurnToClient(turn), nil
}

func (h agentHandler) ListTurns(ctx context.Context, req *client.ListAgentProviderTurnsRequest) (*client.ListAgentProviderTurnsResponse, error) {
	rootReq := &ListAgentProviderTurnsRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionID(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
		TurnIDs:      append([]string(nil), req.GetTurnIDs()...),
		Status:       AgentExecutionStatus(req.GetStatus()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
	resp, err := h.provider.ListTurns(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent list turns", err)
	}
	return rootListAgentTurnsResponseToClient(resp), nil
}

func (h agentHandler) CancelTurn(ctx context.Context, req *client.CancelAgentProviderTurnRequest) (*client.AgentTurn, error) {
	rootReq := &CancelAgentProviderTurnRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnID(),
		Reason:       req.GetReason(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	turn, err := h.provider.CancelTurn(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent cancel turn", err)
	}
	return rootAgentTurnToClient(turn), nil
}

func (h agentHandler) ListTurnEvents(ctx context.Context, req *client.ListAgentProviderTurnEventsRequest) (*client.ListAgentProviderTurnEventsResponse, error) {
	rootReq := &ListAgentProviderTurnEventsRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnID(),
		AfterSeq:     req.GetAfterSeq(),
		Limit:        req.GetLimit(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	resp, err := h.provider.ListTurnEvents(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent list turn events", err)
	}
	return rootListAgentTurnEventsResponseToClient(resp), nil
}

func (h agentHandler) GetInteraction(ctx context.Context, req *client.GetAgentProviderInteractionRequest) (*client.AgentInteraction, error) {
	rootReq := &GetAgentProviderInteractionRequest{
		InteractionID: req.GetInteractionID(),
		Subject:       clientSubjectContextToRootSubject(req.GetSubject()),
		Context:       requestContextFromContext(ctx),
	}
	interaction, err := h.provider.GetInteraction(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent get interaction", err)
	}
	return rootAgentInteractionToClient(interaction), nil
}

func (h agentHandler) ListInteractions(ctx context.Context, req *client.ListAgentProviderInteractionsRequest) (*client.ListAgentProviderInteractionsResponse, error) {
	rootReq := &ListAgentProviderInteractionsRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnID(),
		Subject:      clientSubjectContextToRootSubject(req.GetSubject()),
		Context:      requestContextFromContext(ctx),
	}
	resp, err := h.provider.ListInteractions(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent list interactions", err)
	}
	return rootListAgentInteractionsResponseToClient(resp), nil
}

func (h agentHandler) ResolveInteraction(ctx context.Context, req *client.ResolveAgentProviderInteractionRequest) (*client.AgentInteraction, error) {
	rootReq := &ResolveAgentProviderInteractionRequest{
		ProviderName:  req.GetProviderName(),
		TurnID:        req.GetTurnID(),
		InteractionID: req.GetInteractionID(),
		Resolution:    req.GetResolution(),
		Subject:       clientSubjectContextToRootSubject(req.GetSubject()),
		Context:       requestContextFromContext(ctx),
	}
	interaction, err := h.provider.ResolveInteraction(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("agent resolve interaction", err)
	}
	return rootAgentInteractionToClient(interaction), nil
}

func (h agentHandler) GetCapabilities(ctx context.Context, _ *client.GetAgentProviderCapabilitiesRequest) (*client.AgentProviderCapabilities, error) {
	capabilities, err := h.provider.GetCapabilities(ctx, &GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return nil, providerRPCError("agent get capabilities", err)
	}
	return rootAgentCapabilitiesToClient(capabilities), nil
}

// --- root → client conversions ---

func rootAgentSessionToClient(in *AgentSession) *client.AgentSession {
	if in == nil {
		return nil
	}
	out := &client.AgentSession{
		ID:                 in.ID,
		ProviderName:       in.ProviderName,
		Model:              in.Model,
		ClientRef:          in.ClientRef,
		State:              client.AgentSessionState(in.State),
		Metadata:           in.Metadata,
		CreatedBySubjectID: strings.TrimSpace(in.CreatedBySubjectID),
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	if !in.UpdatedAt.IsZero() {
		t := in.UpdatedAt
		out.UpdatedAt = &t
	}
	if in.LastTurnAt != nil {
		out.LastTurnAt = in.LastTurnAt
	}
	return out
}

func rootListAgentSessionsResponseToClient(in *ListAgentProviderSessionsResponse) *client.ListAgentProviderSessionsResponse {
	if in == nil {
		return nil
	}
	out := &client.ListAgentProviderSessionsResponse{}
	for i := range in.Sessions {
		out.Sessions = append(out.Sessions, rootAgentSessionToClient(&in.Sessions[i]))
	}
	return out
}

func rootAgentTurnToClient(in *AgentTurn) *client.AgentTurn {
	if in == nil {
		return nil
	}
	out := &client.AgentTurn{
		ID:                 in.ID,
		SessionID:          in.SessionID,
		ProviderName:       in.ProviderName,
		Model:              in.Model,
		Status:             client.AgentExecutionStatus(in.Status),
		Messages:           rootAgentMessagesToClient(in.Messages),
		StatusMessage:      in.StatusMessage,
		CreatedBySubjectID: strings.TrimSpace(in.CreatedBySubjectID),
		ExecutionRef:       in.ExecutionRef,
		StartedAt:          in.StartedAt,
		CompletedAt:        in.CompletedAt,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	if in.Output != nil {
		out.Output = rootAgentTurnOutputToClient(in.Output)
	}
	return out
}

func rootAgentTurnOutputToClient(in *AgentTurnOutput) client.AgentTurnOutput {
	if in == nil {
		return nil
	}
	switch {
	case in.Text != nil:
		return &client.AgentTurnOutputText{Value: &client.AgentTurnTextOutput{Text: in.Text.Text}}
	case in.Structured != nil:
		return &client.AgentTurnOutputStructured{Value: &client.AgentTurnStructuredOutput{
			Text:  in.Structured.Text,
			Value: in.Structured.Value,
		}}
	default:
		return nil
	}
}

func rootListAgentTurnsResponseToClient(in *ListAgentProviderTurnsResponse) *client.ListAgentProviderTurnsResponse {
	if in == nil {
		return nil
	}
	out := &client.ListAgentProviderTurnsResponse{}
	for i := range in.Turns {
		out.Turns = append(out.Turns, rootAgentTurnToClient(&in.Turns[i]))
	}
	return out
}

func rootAgentMessagesToClient(in []AgentMessage) []*client.AgentMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]*client.AgentMessage, 0, len(in))
	for i := range in {
		out = append(out, rootAgentMessageToClient(&in[i]))
	}
	return out
}

func rootAgentMessageToClient(in *AgentMessage) *client.AgentMessage {
	if in == nil {
		return nil
	}
	out := &client.AgentMessage{
		Role:     in.Role,
		Text:     in.Text,
		Metadata: in.Metadata,
	}
	for i := range in.Parts {
		out.Parts = append(out.Parts, rootAgentMessagePartToClient(&in.Parts[i]))
	}
	return out
}

func rootAgentMessagePartToClient(in *AgentMessagePart) *client.AgentMessagePart {
	if in == nil {
		return nil
	}
	out := &client.AgentMessagePart{
		Type: client.AgentMessagePartType(in.Type),
		Text: in.Text,
		JSON: in.JSON,
	}
	if in.ToolCall != nil {
		out.ToolCall = &client.AgentMessagePartToolCall{
			ID:        in.ToolCall.ID,
			ToolID:    in.ToolCall.ToolID,
			Arguments: in.ToolCall.Arguments,
		}
	}
	if in.ToolResult != nil {
		out.ToolResult = &client.AgentMessagePartToolResult{
			ToolCallID: in.ToolResult.ToolCallID,
			Status:     in.ToolResult.Status,
			Content:    in.ToolResult.Content,
			Output:     in.ToolResult.Output,
		}
	}
	if in.ImageRef != nil {
		out.ImageRef = &client.AgentMessagePartImageRef{
			URI:      in.ImageRef.URI,
			MimeType: in.ImageRef.MimeType,
		}
	}
	return out
}

func rootAgentTurnEventToClient(in *AgentTurnEvent) *client.AgentTurnEvent {
	if in == nil {
		return nil
	}
	out := &client.AgentTurnEvent{
		ID:         in.ID,
		TurnID:     in.TurnID,
		Seq:        in.Seq,
		Type:       in.Type,
		Source:     in.Source,
		Visibility: in.Visibility,
		Data:       in.Data,
		Display:    rootAgentTurnDisplayToClient(in.Display),
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	return out
}

func rootAgentTurnDisplayToClient(in *AgentTurnDisplay) *client.AgentTurnDisplay {
	if in == nil {
		return nil
	}
	return &client.AgentTurnDisplay{
		Kind:      in.Kind,
		Phase:     in.Phase,
		Text:      in.Text,
		Label:     in.Label,
		Ref:       in.Ref,
		ParentRef: in.ParentRef,
		Input:     in.Input,
		Output:    in.Output,
		Error:     in.Error,
		Action:    in.Action,
		Format:    in.Format,
		Language:  in.Language,
	}
}

func rootListAgentTurnEventsResponseToClient(in *ListAgentProviderTurnEventsResponse) *client.ListAgentProviderTurnEventsResponse {
	if in == nil {
		return nil
	}
	out := &client.ListAgentProviderTurnEventsResponse{}
	for i := range in.Events {
		out.Events = append(out.Events, rootAgentTurnEventToClient(&in.Events[i]))
	}
	return out
}

func rootAgentInteractionToClient(in *AgentInteraction) *client.AgentInteraction {
	if in == nil {
		return nil
	}
	out := &client.AgentInteraction{
		ID:         in.ID,
		Type:       client.AgentInteractionType(in.Type),
		State:      client.AgentInteractionState(in.State),
		Title:      in.Title,
		Prompt:     in.Prompt,
		Request:    in.Request,
		Resolution: in.Resolution,
		TurnID:     in.TurnID,
		SessionID:  in.SessionID,
		ResolvedAt: in.ResolvedAt,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	return out
}

func rootListAgentInteractionsResponseToClient(in *ListAgentProviderInteractionsResponse) *client.ListAgentProviderInteractionsResponse {
	if in == nil {
		return nil
	}
	out := &client.ListAgentProviderInteractionsResponse{}
	for i := range in.Interactions {
		out.Interactions = append(out.Interactions, rootAgentInteractionToClient(&in.Interactions[i]))
	}
	return out
}

func rootAgentCapabilitiesToClient(in *AgentProviderCapabilities) *client.AgentProviderCapabilities {
	if in == nil {
		return nil
	}
	out := &client.AgentProviderCapabilities{
		StreamingText:             in.StreamingText,
		ToolCalls:                 in.ToolCalls,
		ParallelToolCalls:         in.ParallelToolCalls,
		Interactions:              in.Interactions,
		ResumableTurns:            in.ResumableTurns,
		ReasoningSummaries:        in.ReasoningSummaries,
		BoundedListHydration:      in.BoundedListHydration,
		SupportsSessionStart:      in.SupportsSessionStart,
		SupportsPreparedWorkspace: in.SupportsPreparedWorkspace,
	}
	for _, src := range in.SupportedToolSources {
		out.SupportedToolSources = append(out.SupportedToolSources, client.AgentToolSourceMode(src))
	}
	return out
}

// --- client → root conversions ---

func clientAgentSessionStartConfigToRoot(in *client.AgentSessionStartConfig) *AgentSessionStartConfig {
	if in == nil {
		return nil
	}
	out := &AgentSessionStartConfig{}
	for _, hook := range in.GetHooks() {
		out.Hooks = append(out.Hooks, clientAgentSessionStartHookToRoot(hook))
	}
	return out
}

func clientAgentSessionStartHookToRoot(in *client.AgentSessionStartHook) AgentSessionStartHook {
	if in == nil {
		return AgentSessionStartHook{}
	}
	out := AgentSessionStartHook{
		ID:      in.GetID(),
		Type:    in.GetType(),
		Command: append([]string(nil), in.GetCommand()...),
		Cwd:     in.GetCwd(),
		Timeout: in.GetTimeout(),
		Env:     cloneAgentStringMap(in.GetEnv()),
	}
	if hook := in.GetOutput(); hook != nil {
		out.Output = &AgentSessionStartHookOutput{
			AdditionalContext: hook.GetAdditionalContext(),
			Metadata:          hook.GetMetadata(),
		}
	}
	return out
}

func clientAgentPreparedWorkspaceToRoot(in *client.PreparedAgentWorkspace) *AgentPreparedWorkspace {
	if in == nil {
		return nil
	}
	return &AgentPreparedWorkspace{
		Root: in.GetRoot(),
		Cwd:  in.GetCwd(),
	}
}

func clientAgentToolConfigToRoot(in *client.AgentToolConfig) AgentToolConfig {
	if in == nil {
		return nil
	}
	switch src := in.GetSource().(type) {
	case *client.AgentToolConfigSourceNone:
		_ = src
		return &AgentNoTools{}
	case *client.AgentToolConfigSourceCatalog:
		if src.Value == nil {
			return &AgentCatalogToolConfig{}
		}
		catalog := &AgentCatalogToolConfig{}
		for _, ref := range src.Value.GetRefs() {
			catalog.Refs = append(catalog.Refs, clientAgentToolRefToRoot(ref))
		}
		for _, tool := range src.Value.GetTools() {
			catalog.Tools = append(catalog.Tools, clientListedAgentToolToRoot(tool))
		}
		return catalog
	default:
		return nil
	}
}

func clientListedAgentToolToRoot(in *client.ListedAgentTool) ListedAgentTool {
	if in == nil {
		return ListedAgentTool{}
	}
	out := ListedAgentTool{
		ID:           in.GetID(),
		MCPName:      in.GetMcpName(),
		Title:        in.GetTitle(),
		Description:  in.GetDescription(),
		InputSchema:  in.GetInputSchema(),
		OutputSchema: in.GetOutputSchema(),
		Tags:         append([]string(nil), in.GetTags()...),
		SearchText:   in.GetSearchText(),
	}
	if ann := in.GetAnnotations(); ann != nil {
		out.Annotations = &AgentToolAnnotations{
			ReadOnlyHint:    ann.ReadOnlyHint,
			IdempotentHint:  ann.IdempotentHint,
			DestructiveHint: ann.DestructiveHint,
			OpenWorldHint:   ann.OpenWorldHint,
		}
	}
	if ref := in.GetRef(); ref != nil {
		r := clientAgentToolRefToRoot(ref)
		out.Ref = &r
	}
	return out
}

func clientAgentMessagesToRoot(in []*client.AgentMessage) []AgentMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]AgentMessage, 0, len(in))
	for _, msg := range in {
		out = append(out, clientAgentMessageToRoot(msg))
	}
	return out
}

func clientAgentMessageToRoot(in *client.AgentMessage) AgentMessage {
	if in == nil {
		return AgentMessage{}
	}
	out := AgentMessage{
		Role:     in.GetRole(),
		Text:     in.GetText(),
		Metadata: in.GetMetadata(),
	}
	for _, part := range in.GetParts() {
		out.Parts = append(out.Parts, clientAgentMessagePartToRoot(part))
	}
	return out
}

func clientAgentMessagePartToRoot(in *client.AgentMessagePart) AgentMessagePart {
	if in == nil {
		return AgentMessagePart{}
	}
	out := AgentMessagePart{
		Type: AgentMessagePartType(in.GetType()),
		Text: in.Text,
		JSON: in.JSON,
	}
	if in.ToolCall != nil {
		out.ToolCall = &AgentMessagePartToolCall{
			ID:        in.ToolCall.ID,
			ToolID:    in.ToolCall.ToolID,
			Arguments: in.ToolCall.Arguments,
		}
	}
	if in.ToolResult != nil {
		out.ToolResult = &AgentMessagePartToolResult{
			ToolCallID: in.ToolResult.ToolCallID,
			Status:     in.ToolResult.Status,
			Content:    in.ToolResult.Content,
			Output:     in.ToolResult.Output,
		}
	}
	if in.ImageRef != nil {
		out.ImageRef = &AgentMessagePartImageRef{
			URI:      in.ImageRef.URI,
			MimeType: in.ImageRef.MimeType,
		}
	}
	return out
}

func clientAgentOutputToRootOutput(in *client.AgentOutput) *AgentOutput {
	if in == nil {
		return nil
	}
	switch k := in.GetKind().(type) {
	case *client.AgentOutputKindText:
		_ = k
		return &AgentOutput{Text: &AgentTextOutput{}}
	case *client.AgentOutputKindStructured:
		var schema map[string]any
		if k.Value != nil {
			schema = k.Value.Schema
		}
		return &AgentOutput{Structured: &AgentStructuredOutput{Schema: schema}}
	default:
		return nil
	}
}
