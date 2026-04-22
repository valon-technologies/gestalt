package providerhost

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func agentRunStatusFromProto(status proto.AgentRunStatus) (coreagent.RunStatus, error) {
	switch status {
	case proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED:
		return "", nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_PENDING:
		return coreagent.RunStatusPending, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_RUNNING:
		return coreagent.RunStatusRunning, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED:
		return coreagent.RunStatusSucceeded, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_FAILED:
		return coreagent.RunStatusFailed, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_CANCELED:
		return coreagent.RunStatusCanceled, nil
	default:
		return "", fmt.Errorf("unknown agent run status %v", status)
	}
}

func agentMessageToProto(message coreagent.Message) *proto.AgentMessage {
	if message == (coreagent.Message{}) {
		return nil
	}
	return &proto.AgentMessage{
		Role: message.Role,
		Text: message.Text,
	}
}

func agentMessagesToProto(messages []coreagent.Message) []*proto.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]*proto.AgentMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, agentMessageToProto(message))
	}
	return out
}

func agentMessageFromProto(message *proto.AgentMessage) coreagent.Message {
	if message == nil {
		return coreagent.Message{}
	}
	return coreagent.Message{
		Role: message.GetRole(),
		Text: message.GetText(),
	}
}

func agentMessagesFromProto(messages []*proto.AgentMessage) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, agentMessageFromProto(message))
	}
	return out
}

func agentActorToProto(actor coreagent.Actor) *proto.AgentActor {
	if actor == (coreagent.Actor{}) {
		return nil
	}
	return &proto.AgentActor{
		SubjectId:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func agentActorFromProto(actor *proto.AgentActor) coreagent.Actor {
	if actor == nil {
		return coreagent.Actor{}
	}
	return coreagent.Actor{
		SubjectID:   actor.GetSubjectId(),
		SubjectKind: actor.GetSubjectKind(),
		DisplayName: actor.GetDisplayName(),
		AuthSource:  actor.GetAuthSource(),
	}
}

func agentToolTargetToProto(target coreagent.ToolTarget) *proto.BoundAgentToolTarget {
	if target == (coreagent.ToolTarget{}) {
		return nil
	}
	return &proto.BoundAgentToolTarget{
		PluginName: target.PluginName,
		Operation:  target.Operation,
		Connection: target.Connection,
		Instance:   target.Instance,
	}
}

func agentToolToProto(tool coreagent.Tool) (*proto.ResolvedAgentTool, error) {
	schema, err := structFromMap(tool.ParametersSchema)
	if err != nil {
		return nil, fmt.Errorf("agent tool parameters schema: %w", err)
	}
	return &proto.ResolvedAgentTool{
		Id:               tool.ID,
		Name:             tool.Name,
		Description:      tool.Description,
		Target:           agentToolTargetToProto(tool.Target),
		ParametersSchema: schema,
	}, nil
}

func agentToolsToProto(tools []coreagent.Tool) ([]*proto.ResolvedAgentTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]*proto.ResolvedAgentTool, 0, len(tools))
	for _, tool := range tools {
		value, err := agentToolToProto(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func agentToolRefFromProto(ref *proto.AgentToolRef) coreagent.ToolRef {
	if ref == nil {
		return coreagent.ToolRef{}
	}
	return coreagent.ToolRef{
		PluginName:  ref.GetPluginName(),
		Operation:   ref.GetOperation(),
		Connection:  ref.GetConnection(),
		Instance:    ref.GetInstance(),
		Title:       ref.GetTitle(),
		Description: ref.GetDescription(),
	}
}

func agentToolRefsFromProto(refs []*proto.AgentToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, agentToolRefFromProto(ref))
	}
	return out
}

func agentToolSourceModeFromProto(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	switch mode {
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_EXPLICIT:
		return coreagent.ToolSourceModeExplicit
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_INHERIT_INVOKES:
		return coreagent.ToolSourceModeInheritInvokes
	default:
		return coreagent.ToolSourceModeUnspecified
	}
}

func agentRunFromProto(run *proto.BoundAgentRun) (*coreagent.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := agentRunStatusFromProto(run.GetStatus())
	if err != nil {
		return nil, err
	}
	return &coreagent.Run{
		ID:               run.GetId(),
		ProviderName:     run.GetProviderName(),
		Model:            run.GetModel(),
		Status:           status,
		Messages:         agentMessagesFromProto(run.GetMessages()),
		OutputText:       run.GetOutputText(),
		StructuredOutput: mapFromStruct(run.GetStructuredOutput()),
		StatusMessage:    run.GetStatusMessage(),
		SessionRef:       run.GetSessionRef(),
		CreatedBy:        agentActorFromProto(run.GetCreatedBy()),
		CreatedAt:        timeFromProto(run.GetCreatedAt()),
		StartedAt:        timeFromProto(run.GetStartedAt()),
		CompletedAt:      timeFromProto(run.GetCompletedAt()),
		ExecutionRef:     run.GetExecutionRef(),
	}, nil
}

func agentRunStatusToProto(status coreagent.RunStatus) proto.AgentRunStatus {
	switch status {
	case "":
		return proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED
	case coreagent.RunStatusPending:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_PENDING
	case coreagent.RunStatusRunning:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_RUNNING
	case coreagent.RunStatusSucceeded:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED
	case coreagent.RunStatusFailed:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_FAILED
	case coreagent.RunStatusCanceled:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_CANCELED
	default:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED
	}
}

func agentRunToProto(run *coreagent.Run) (*proto.BoundAgentRun, error) {
	if run == nil {
		return nil, nil
	}
	structuredOutput, err := structFromMap(run.StructuredOutput)
	if err != nil {
		return nil, fmt.Errorf("agent structured output: %w", err)
	}
	return &proto.BoundAgentRun{
		Id:               run.ID,
		ProviderName:     run.ProviderName,
		Model:            run.Model,
		Status:           agentRunStatusToProto(run.Status),
		Messages:         agentMessagesToProto(run.Messages),
		OutputText:       run.OutputText,
		StructuredOutput: structuredOutput,
		StatusMessage:    run.StatusMessage,
		SessionRef:       run.SessionRef,
		CreatedBy:        agentActorToProto(run.CreatedBy),
		CreatedAt:        timeToProto(run.CreatedAt),
		StartedAt:        timeToProto(run.StartedAt),
		CompletedAt:      timeToProto(run.CompletedAt),
		ExecutionRef:     run.ExecutionRef,
	}, nil
}

func managedAgentRunToProto(run *coreagent.ManagedRun) (*proto.ManagedAgentRun, error) {
	if run == nil {
		return nil, nil
	}
	encodedRun, err := agentRunToProto(run.Run)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedAgentRun{
		ProviderName: run.ProviderName,
		Run:          encodedRun,
	}, nil
}
