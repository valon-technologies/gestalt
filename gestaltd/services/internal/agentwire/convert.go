package agentwire

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/internal/protoutil"
)

func MessageToProto(message coreagent.Message) (*proto.AgentMessage, error) {
	parts, err := MessagePartsToProto(message.Parts)
	if err != nil {
		return nil, err
	}
	metadata, err := protoutil.StructFromMap(message.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.AgentMessage{
		Role:     message.Role,
		Text:     message.Text,
		Parts:    parts,
		Metadata: metadata,
	}, nil
}

func MessagesToProto(messages []coreagent.Message) ([]*proto.AgentMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessage, 0, len(messages))
	for _, message := range messages {
		encoded, err := MessageToProto(message)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func MessageFromProto(message *proto.AgentMessage) coreagent.Message {
	if message == nil {
		return coreagent.Message{}
	}
	return coreagent.Message{
		Role:     message.GetRole(),
		Text:     message.GetText(),
		Parts:    MessagePartsFromProto(message.GetParts()),
		Metadata: protoutil.MapFromStruct(message.GetMetadata()),
	}
}

func MessagesFromProto(messages []*proto.AgentMessage) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, MessageFromProto(message))
	}
	return out
}

func ToolRefFromProto(ref *proto.AgentToolRef) coreagent.ToolRef {
	if ref == nil {
		return coreagent.ToolRef{}
	}
	return coreagent.ToolRef{
		System:      ref.GetSystem(),
		Plugin:      ref.GetPlugin(),
		Operation:   ref.GetOperation(),
		Connection:  ref.GetConnection(),
		Instance:    ref.GetInstance(),
		Title:       ref.GetTitle(),
		Description: ref.GetDescription(),
	}
}

func ToolRefToProto(ref coreagent.ToolRef) *proto.AgentToolRef {
	return &proto.AgentToolRef{
		System:      ref.System,
		Plugin:      ref.Plugin,
		Operation:   ref.Operation,
		Connection:  ref.Connection,
		Instance:    ref.Instance,
		Title:       ref.Title,
		Description: ref.Description,
	}
}

func ToolRefsFromProto(refs []*proto.AgentToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ToolRefFromProto(ref))
	}
	return out
}

func ToolRefsToProto(refs []coreagent.ToolRef) []*proto.AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolRef, 0, len(refs))
	for i := range refs {
		out = append(out, ToolRefToProto(refs[i]))
	}
	return out
}

func ToolSourceModeFromProto(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	switch mode {
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG:
		return coreagent.ToolSourceModeMCPCatalog
	default:
		return coreagent.ToolSourceModeUnspecified
	}
}

func ToolSourceModeToProto(mode coreagent.ToolSourceMode) proto.AgentToolSourceMode {
	switch mode {
	case coreagent.ToolSourceModeMCPCatalog:
		return proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG
	default:
		return proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
	}
}

func MessagePartTypeFromProto(partType proto.AgentMessagePartType) (coreagent.MessagePartType, error) {
	switch partType {
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED:
		return "", nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT:
		return coreagent.MessagePartTypeText, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON:
		return coreagent.MessagePartTypeJSON, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL:
		return coreagent.MessagePartTypeToolCall, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT:
		return coreagent.MessagePartTypeToolResult, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF:
		return coreagent.MessagePartTypeImageRef, nil
	default:
		return "", fmt.Errorf("unknown agent message part type %v", partType)
	}
}

func MessagePartTypeToProto(partType coreagent.MessagePartType) proto.AgentMessagePartType {
	switch partType {
	case "":
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
	case coreagent.MessagePartTypeText:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT
	case coreagent.MessagePartTypeJSON:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON
	case coreagent.MessagePartTypeToolCall:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL
	case coreagent.MessagePartTypeToolResult:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
	case coreagent.MessagePartTypeImageRef:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF
	default:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
	}
}

func MessagePartToProto(part coreagent.MessagePart) (*proto.AgentMessagePart, error) {
	jsonValue, err := protoutil.StructFromMap(part.JSON)
	if err != nil {
		return nil, fmt.Errorf("agent message part json: %w", err)
	}
	var toolCall *proto.AgentMessagePartToolCall
	if part.ToolCall != nil {
		args, err := protoutil.StructFromMap(part.ToolCall.Arguments)
		if err != nil {
			return nil, fmt.Errorf("agent message tool call arguments: %w", err)
		}
		toolCall = &proto.AgentMessagePartToolCall{
			Id:        part.ToolCall.ID,
			ToolId:    part.ToolCall.ToolID,
			Arguments: args,
		}
	}
	var toolResult *proto.AgentMessagePartToolResult
	if part.ToolResult != nil {
		output, err := protoutil.StructFromMap(part.ToolResult.Output)
		if err != nil {
			return nil, fmt.Errorf("agent message tool result output: %w", err)
		}
		toolResult = &proto.AgentMessagePartToolResult{
			ToolCallId: part.ToolResult.ToolCallID,
			Status:     int32(part.ToolResult.Status),
			Content:    part.ToolResult.Content,
			Output:     output,
		}
	}
	var imageRef *proto.AgentMessagePartImageRef
	if part.ImageRef != nil {
		imageRef = &proto.AgentMessagePartImageRef{
			Uri:      part.ImageRef.URI,
			MimeType: part.ImageRef.MIMEType,
		}
	}
	return &proto.AgentMessagePart{
		Type:       MessagePartTypeToProto(part.Type),
		Text:       part.Text,
		Json:       jsonValue,
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   imageRef,
	}, nil
}

func MessagePartsToProto(parts []coreagent.MessagePart) ([]*proto.AgentMessagePart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessagePart, 0, len(parts))
	for _, part := range parts {
		encoded, err := MessagePartToProto(part)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func MessagePartFromProto(part *proto.AgentMessagePart) coreagent.MessagePart {
	if part == nil {
		return coreagent.MessagePart{}
	}
	partType, _ := MessagePartTypeFromProto(part.GetType())
	var toolCall *coreagent.ToolCallPart
	if value := part.GetToolCall(); value != nil {
		toolCall = &coreagent.ToolCallPart{
			ID:        value.GetId(),
			ToolID:    value.GetToolId(),
			Arguments: protoutil.MapFromStruct(value.GetArguments()),
		}
	}
	var toolResult *coreagent.ToolResultPart
	if value := part.GetToolResult(); value != nil {
		toolResult = &coreagent.ToolResultPart{
			ToolCallID: value.GetToolCallId(),
			Status:     int(value.GetStatus()),
			Content:    value.GetContent(),
			Output:     protoutil.MapFromStruct(value.GetOutput()),
		}
	}
	var imageRef *coreagent.ImageRefPart
	if value := part.GetImageRef(); value != nil {
		imageRef = &coreagent.ImageRefPart{
			URI:      value.GetUri(),
			MIMEType: value.GetMimeType(),
		}
	}
	return coreagent.MessagePart{
		Type:       partType,
		Text:       part.GetText(),
		JSON:       protoutil.MapFromStruct(part.GetJson()),
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   imageRef,
	}
}

func MessagePartsFromProto(parts []*proto.AgentMessagePart) []coreagent.MessagePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]coreagent.MessagePart, 0, len(parts))
	for _, part := range parts {
		out = append(out, MessagePartFromProto(part))
	}
	return out
}
