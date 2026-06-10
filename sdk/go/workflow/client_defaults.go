package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"google.golang.org/grpc"
)

const hostServiceDialTimeout = 5 * time.Second

// generatedAppInvoker is the default AppInvoker. It dials the "app" host
// service advertised through the GESTALT_HOST_SERVICE_SOCKET environment for
// each invocation and calls through the generated client.
type generatedAppInvoker struct{}

func (generatedAppInvoker) InvokeWorkflowApp(ctx context.Context, call AppInvocation) (*AppResult, error) {
	reqCtx := clientRequestContext(call.Request)
	if len(call.WorkflowContext) > 0 {
		if reqCtx == nil {
			reqCtx = &client.RequestContext{}
		}
		reqCtx.Workflow = call.WorkflowContext
	}
	params := call.Params
	if params == nil {
		params = map[string]any{}
	}
	target, token, err := host.Target("app")
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, hostServiceDialTimeout)
	conn, err := host.DialService(dialCtx, "app", target, token, "")
	cancel()
	if err != nil {
		return nil, fmt.Errorf("workflow app step: connect to host: %w", err)
	}
	defer func() { _ = conn.Close() }()
	result, err := client.NewApp(conn).InvokeRaw(ctx, &client.AppInvokeRequest{
		App:            call.App,
		Operation:      call.Operation,
		Params:         params,
		Connection:     call.Connection,
		Instance:       call.Instance,
		IdempotencyKey: strings.TrimSpace(call.IdempotencyKey),
		CredentialMode: strings.TrimSpace(call.CredentialMode),
		Context:        reqCtx,
	})
	if err != nil {
		return nil, err
	}
	out := &AppResult{}
	if result != nil {
		out.Status = int(result.Status)
		out.Body = string(result.Body)
	}
	return out, nil
}

// newGeneratedAgentClient is the default NewAgent factory. It dials the
// "agent" host service and adapts the generated client to the AgentClient
// contract used by the executor.
func newGeneratedAgentClient(req gestalt.Request) (AgentClient, error) {
	target, token, err := host.Target("agent")
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), hostServiceDialTimeout)
	defer cancel()
	conn, err := host.DialService(dialCtx, "agent", target, token, "")
	if err != nil {
		return nil, fmt.Errorf("workflow agent step: connect to host: %w", err)
	}
	return &generatedAgentClient{
		conn:    conn,
		agent:   client.NewAgent(conn),
		context: clientRequestContext(req),
	}, nil
}

type generatedAgentClient struct {
	conn    *grpc.ClientConn
	agent   *client.Agent
	context *client.RequestContext
}

func (c *generatedAgentClient) Close() error {
	return c.conn.Close()
}

// requestContext layers the ambient workflow context from ctx over the
// request context captured at construction.
func (c *generatedAgentClient) requestContext(ctx context.Context) *client.RequestContext {
	workflow := gestalt.WorkflowContextFromContext(ctx)
	if workflow == nil {
		return c.context
	}
	out := client.RequestContext{}
	if c.context != nil {
		out = *c.context
	}
	out.Workflow = workflow
	return &out
}

func (c *generatedAgentClient) CreateSession(ctx context.Context, input gestalt.AgentCreateSession) (*gestalt.AgentSession, error) {
	metadata, err := workflowMapFromAny(input.Metadata, "agent session metadata")
	if err != nil {
		return nil, err
	}
	session, err := c.agent.CreateSessionRaw(ctx, &client.CreateAgentProviderSessionRequest{
		ProviderName:   input.ProviderName,
		IdempotencyKey: input.IdempotencyKey,
		Model:          input.Model,
		ClientRef:      input.ClientRef,
		Metadata:       metadata,
		Workspace:      agentWorkspaceToClient(input.Workspace),
		Tools:          agentToolConfigToClient(input.Tools),
		Context:        c.requestContext(ctx),
	})
	if err != nil {
		return nil, err
	}
	return agentSessionFromClient(session), nil
}

func (c *generatedAgentClient) CreateTurn(ctx context.Context, input gestalt.AgentCreateTurn) (*gestalt.AgentTurn, error) {
	metadata, err := workflowMapFromAny(input.Metadata, "agent turn metadata")
	if err != nil {
		return nil, err
	}
	modelOptions, err := workflowMapFromAny(input.ModelOptions, "agent turn model options")
	if err != nil {
		return nil, err
	}
	turn, err := c.agent.CreateTurnRaw(ctx, &client.CreateAgentProviderTurnRequest{
		ProviderName:   input.ProviderName,
		SessionId:      input.SessionID,
		IdempotencyKey: input.IdempotencyKey,
		Model:          input.Model,
		Messages:       agentMessagesToClient(input.Messages),
		Metadata:       metadata,
		ModelOptions:   modelOptions,
		TimeoutSeconds: input.TimeoutSeconds,
		Output:         agentOutputToClient(input.Output),
		Context:        c.requestContext(ctx),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromClient(turn), nil
}

func (c *generatedAgentClient) GetTurn(ctx context.Context, input gestalt.AgentGetTurn) (*gestalt.AgentTurn, error) {
	turn, err := c.agent.GetTurnRaw(ctx, &client.GetAgentProviderTurnRequest{
		TurnId:       input.TurnID,
		ProviderName: input.ProviderName,
		Context:      c.requestContext(ctx),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromClient(turn), nil
}

func (c *generatedAgentClient) CancelTurn(ctx context.Context, input gestalt.AgentCancelTurn) (*gestalt.AgentTurn, error) {
	turn, err := c.agent.CancelTurnRaw(ctx, &client.CancelAgentProviderTurnRequest{
		TurnId:       input.TurnID,
		Reason:       input.Reason,
		ProviderName: input.ProviderName,
		Context:      c.requestContext(ctx),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromClient(turn), nil
}

// clientRequestContext maps the public fields of a provider request onto the
// generated client request context. It returns nil when every field is empty.
func clientRequestContext(req gestalt.Request) *client.RequestContext {
	out := &client.RequestContext{
		Subject:      subjectToClient(req.Subject),
		AgentSubject: subjectToClient(req.AgentSubject),
	}
	if req.Credential != (gestalt.Credential{}) {
		out.Credential = &client.CredentialContext{
			Mode:       req.Credential.Mode,
			SubjectId:  req.Credential.SubjectID,
			Connection: req.Credential.Connection,
			Instance:   req.Credential.Instance,
		}
	}
	if req.Access != (gestalt.Access{}) {
		out.Access = &client.AccessContext{Policy: req.Access.Policy, Role: req.Access.Role}
	}
	if req.Host.PublicBaseURL != "" {
		out.Host = &client.HostContext{PublicBaseUrl: req.Host.PublicBaseURL}
	}
	callerKind := strings.TrimSpace(string(req.Caller.Kind))
	callerName := strings.TrimSpace(req.Caller.Name)
	if callerKind != "" && callerName != "" {
		out.Caller = &client.ProviderContext{Kind: callerKind, Name: callerName}
	}
	if len(req.WorkflowContext) > 0 {
		out.Workflow = req.WorkflowContext
	}
	if req.ToolRefsSet {
		out.ToolRefsSet = true
		out.ToolRefs = agentToolRefsToClient(req.ToolRefs)
	}
	if out.Subject == nil && out.AgentSubject == nil && out.Credential == nil &&
		out.Access == nil && out.Host == nil && out.Caller == nil &&
		out.Workflow == nil && !out.ToolRefsSet {
		return nil
	}
	return out
}

func subjectToClient(subject gestalt.Subject) *client.SubjectContext {
	if subject.ID == "" && subject.CredentialSubjectID == "" && subject.Email == "" &&
		subject.DisplayName == "" && len(subject.Scopes) == 0 && len(subject.Permissions) == 0 {
		return nil
	}
	out := &client.SubjectContext{
		Id:                  subject.ID,
		CredentialSubjectId: subject.CredentialSubjectID,
		Email:               subject.Email,
		DisplayName:         subject.DisplayName,
		Scopes:              append([]string(nil), subject.Scopes...),
	}
	for _, permission := range subject.Permissions {
		out.Permissions = append(out.Permissions, &client.SubjectPermissionContext{
			App:        permission.App,
			Operations: append([]string(nil), permission.Operations...),
		})
	}
	return out
}

func agentToolRefsToClient(refs []gestalt.AgentToolRef) []*client.AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*client.AgentToolRef, 0, len(refs))
	for i := range refs {
		out = append(out, agentToolRefToClient(refs[i]))
	}
	return out
}

func agentToolRefToClient(ref gestalt.AgentToolRef) *client.AgentToolRef {
	out := &client.AgentToolRef{
		App:            ref.App,
		Operation:      ref.Operation,
		Connection:     ref.Connection,
		Instance:       ref.Instance,
		Title:          ref.Title,
		Description:    ref.Description,
		CredentialMode: ref.CredentialMode,
		System:         ref.System,
	}
	if ref.RunAs != nil {
		out.RunAs = subjectToClient(*ref.RunAs)
	}
	return out
}

func agentToolConfigToClient(config gestalt.AgentToolConfig) *client.AgentToolConfig {
	switch value := config.(type) {
	case nil:
		return nil
	case *gestalt.AgentNoTools:
		if value == nil {
			return nil
		}
		return &client.AgentToolConfig{Source: &client.AgentToolConfigSourceNone{Value: &client.AgentNoTools{}}}
	case *gestalt.AgentCatalogToolConfig:
		if value == nil {
			return nil
		}
		catalog := &client.AgentCatalogToolConfig{Refs: agentToolRefsToClient(value.Refs)}
		for i := range value.Tools {
			catalog.Tools = append(catalog.Tools, listedAgentToolToClient(value.Tools[i]))
		}
		return &client.AgentToolConfig{Source: &client.AgentToolConfigSourceCatalog{Value: catalog}}
	default:
		return nil
	}
}

func listedAgentToolToClient(tool gestalt.ListedAgentTool) *client.ListedAgentTool {
	out := &client.ListedAgentTool{
		Id:           tool.ID,
		McpName:      tool.MCPName,
		Title:        tool.Title,
		Description:  tool.Description,
		InputSchema:  tool.InputSchema,
		OutputSchema: tool.OutputSchema,
		Tags:         append([]string(nil), tool.Tags...),
		SearchText:   tool.SearchText,
	}
	if tool.Annotations != nil {
		out.Annotations = &client.OperationAnnotations{
			ReadOnlyHint:    tool.Annotations.ReadOnlyHint,
			IdempotentHint:  tool.Annotations.IdempotentHint,
			DestructiveHint: tool.Annotations.DestructiveHint,
			OpenWorldHint:   tool.Annotations.OpenWorldHint,
		}
	}
	if tool.Ref != nil {
		out.Ref = agentToolRefToClient(*tool.Ref)
	}
	return out
}

func agentWorkspaceToClient(workspace *gestalt.AgentWorkspace) *client.AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &client.AgentWorkspace{Cwd: workspace.CWD}
	for _, checkout := range workspace.Checkouts {
		out.Checkouts = append(out.Checkouts, &client.AgentWorkspaceGitCheckout{
			Url:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func agentMessagesToClient(messages []gestalt.AgentMessage) []*client.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]*client.AgentMessage, 0, len(messages))
	for i := range messages {
		message := messages[i]
		converted := &client.AgentMessage{
			Role:     message.Role,
			Text:     message.Text,
			Metadata: message.Metadata,
		}
		for j := range message.Parts {
			converted.Parts = append(converted.Parts, agentMessagePartToClient(message.Parts[j]))
		}
		out = append(out, converted)
	}
	return out
}

func agentMessagePartToClient(part gestalt.AgentMessagePart) *client.AgentMessagePart {
	out := &client.AgentMessagePart{
		Type: client.AgentMessagePartType(part.Type),
		Text: part.Text,
		Json: part.JSON,
	}
	if part.ToolCall != nil {
		out.ToolCall = &client.AgentMessagePartToolCall{
			Id:        part.ToolCall.ID,
			ToolId:    part.ToolCall.ToolID,
			Arguments: part.ToolCall.Arguments,
		}
	}
	if part.ToolResult != nil {
		out.ToolResult = &client.AgentMessagePartToolResult{
			ToolCallId: part.ToolResult.ToolCallID,
			Status:     part.ToolResult.Status,
			Content:    part.ToolResult.Content,
			Output:     part.ToolResult.Output,
		}
	}
	if part.ImageRef != nil {
		out.ImageRef = &client.AgentMessagePartImageRef{
			Uri:      part.ImageRef.URI,
			MimeType: part.ImageRef.MimeType,
		}
	}
	return out
}

func agentMessagesFromClient(messages []*client.AgentMessage) []gestalt.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]gestalt.AgentMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		converted := gestalt.AgentMessage{
			Role:     message.Role,
			Text:     message.Text,
			Metadata: message.Metadata,
		}
		for _, part := range message.Parts {
			if part == nil {
				continue
			}
			converted.Parts = append(converted.Parts, agentMessagePartFromClient(part))
		}
		out = append(out, converted)
	}
	return out
}

func agentMessagePartFromClient(part *client.AgentMessagePart) gestalt.AgentMessagePart {
	out := gestalt.AgentMessagePart{
		Type: gestalt.AgentMessagePartType(part.Type),
		Text: part.Text,
		JSON: part.Json,
	}
	if part.ToolCall != nil {
		out.ToolCall = &gestalt.AgentMessagePartToolCall{
			ID:        part.ToolCall.Id,
			ToolID:    part.ToolCall.ToolId,
			Arguments: part.ToolCall.Arguments,
		}
	}
	if part.ToolResult != nil {
		out.ToolResult = &gestalt.AgentMessagePartToolResult{
			ToolCallID: part.ToolResult.ToolCallId,
			Status:     part.ToolResult.Status,
			Content:    part.ToolResult.Content,
			Output:     part.ToolResult.Output,
		}
	}
	if part.ImageRef != nil {
		out.ImageRef = &gestalt.AgentMessagePartImageRef{
			URI:      part.ImageRef.Uri,
			MimeType: part.ImageRef.MimeType,
		}
	}
	return out
}

func agentOutputToClient(output *gestalt.AgentOutput) *client.AgentOutput {
	if output == nil {
		return nil
	}
	if output.Structured != nil {
		return &client.AgentOutput{Kind: &client.AgentOutputKindStructured{
			Value: &client.AgentStructuredOutput{Schema: output.Structured.Schema},
		}}
	}
	if output.Text != nil {
		return &client.AgentOutput{Kind: &client.AgentOutputKindText{Value: &client.AgentTextOutput{}}}
	}
	return &client.AgentOutput{}
}

func agentSessionFromClient(session *client.AgentSession) *gestalt.AgentSession {
	if session == nil {
		return nil
	}
	out := &gestalt.AgentSession{
		ID:                 session.Id,
		ProviderName:       session.ProviderName,
		Model:              session.Model,
		ClientRef:          session.ClientRef,
		State:              gestalt.AgentSessionState(session.State),
		Metadata:           session.Metadata,
		CreatedBySubjectID: session.CreatedBySubjectId,
		LastTurnAt:         session.LastTurnAt,
	}
	if session.CreatedAt != nil {
		out.CreatedAt = *session.CreatedAt
	}
	if session.UpdatedAt != nil {
		out.UpdatedAt = *session.UpdatedAt
	}
	return out
}

func agentTurnFromClient(turn *client.AgentTurn) *gestalt.AgentTurn {
	if turn == nil {
		return nil
	}
	out := &gestalt.AgentTurn{
		ID:                 turn.Id,
		SessionID:          turn.SessionId,
		ProviderName:       turn.ProviderName,
		Model:              turn.Model,
		Status:             gestalt.AgentExecutionStatus(turn.Status),
		Messages:           agentMessagesFromClient(turn.Messages),
		StatusMessage:      turn.StatusMessage,
		CreatedBySubjectID: turn.CreatedBySubjectId,
		StartedAt:          turn.StartedAt,
		CompletedAt:        turn.CompletedAt,
		ExecutionRef:       turn.ExecutionRef,
	}
	if turn.CreatedAt != nil {
		out.CreatedAt = *turn.CreatedAt
	}
	switch value := turn.Output.(type) {
	case *client.AgentTurnOutputText:
		if value.Value != nil {
			out.Output = &gestalt.AgentTurnOutput{Text: &gestalt.AgentTurnTextOutput{Text: value.Value.Text}}
		}
	case *client.AgentTurnOutputStructured:
		if value.Value != nil {
			out.Output = &gestalt.AgentTurnOutput{Structured: &gestalt.AgentTurnStructuredOutput{
				Text:  value.Value.Text,
				Value: value.Value.Value,
			}}
		}
	}
	return out
}

func workflowMapFromAny(value any, what string) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return typed, nil
	default:
		return nil, fmt.Errorf("workflow %s must be a map[string]any, got %T", what, value)
	}
}
