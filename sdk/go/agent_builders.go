package gestalt

import "fmt"

// NewAgentMessage creates an agent message.
func NewAgentMessage(input AgentMessage) (*AgentMessage, error) {
	metadata, err := agentMapFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	parts, err := agentMessagePartsFromInputs(input.Parts)
	if err != nil {
		return nil, err
	}
	return &AgentMessage{
		Role:     input.Role,
		Text:     input.Text,
		Parts:    parts,
		Metadata: metadata,
	}, nil
}

// AgentMessageFromMessage converts an existing protocol message into
// builder input.
func AgentMessageFromMessage(value *AgentMessage) (AgentMessage, error) {
	if value == nil {
		return AgentMessage{}, nil
	}
	parts, err := agentMessagePartInputsFromParts(value.Parts)
	if err != nil {
		return AgentMessage{}, err
	}
	return AgentMessage{
		Role:     value.Role,
		Text:     value.Text,
		Parts:    parts,
		Metadata: value.Metadata,
	}, nil
}

// NewAgentMessagePart creates an agent message part.
func NewAgentMessagePart(input AgentMessagePart) (*AgentMessagePart, error) {
	partType := input.Type
	if partType == AgentMessagePartTypeUnspecified {
		partType = inferAgentMessagePartType(input)
	}
	jsonValue, err := agentMapFromAny(input.JSON)
	if err != nil {
		return nil, err
	}
	toolCall, err := newOptionalAgentToolCall(input.ToolCall)
	if err != nil {
		return nil, err
	}
	toolResult, err := newOptionalAgentToolResult(input.ToolResult)
	if err != nil {
		return nil, err
	}
	return &AgentMessagePart{
		Type:       partType,
		Text:       input.Text,
		JSON:       jsonValue,
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   newOptionalAgentImageRef(input.ImageRef),
	}, nil
}

// AgentMessagePartFromPart converts an existing protocol part into builder input.
func AgentMessagePartFromPart(value *AgentMessagePart) (AgentMessagePart, error) {
	if value == nil {
		return AgentMessagePart{}, nil
	}
	return AgentMessagePart{
		Type:       value.Type,
		Text:       value.Text,
		JSON:       value.JSON,
		ToolCall:   agentToolCallInputPtrFromCall(value.ToolCall),
		ToolResult: agentToolResultInputPtrFromResult(value.ToolResult),
		ImageRef:   agentImageRefInputPtrFromRef(value.ImageRef),
	}, nil
}

// NewAgentMessagePartToolCall creates a tool-call payload.
func NewAgentMessagePartToolCall(input AgentMessagePartToolCall) (*AgentMessagePartToolCall, error) {
	arguments, err := agentMapFromAny(input.Arguments)
	if err != nil {
		return nil, err
	}
	return &AgentMessagePartToolCall{
		ID:        input.ID,
		ToolID:    input.ToolID,
		Arguments: arguments,
	}, nil
}

// NewAgentMessagePartToolResult creates a tool-result payload.
func NewAgentMessagePartToolResult(input AgentMessagePartToolResult) (*AgentMessagePartToolResult, error) {
	output, err := agentMapFromAny(input.Output)
	if err != nil {
		return nil, err
	}
	return &AgentMessagePartToolResult{
		ToolCallID: input.ToolCallID,
		Status:     input.Status,
		Content:    input.Content,
		Output:     output,
	}, nil
}

// NewAgentMessagePartImageRef creates an image-reference payload.
func NewAgentMessagePartImageRef(input AgentMessagePartImageRef) *AgentMessagePartImageRef {
	return &AgentMessagePartImageRef{
		URI:      input.URI,
		MimeType: input.MimeType,
	}
}

// NewAgentToolRef creates an agent tool reference.
func NewAgentToolRef(input AgentToolRef) *AgentToolRef {
	return &AgentToolRef{
		Plugin:      input.Plugin,
		Operation:   input.Operation,
		Connection:  input.Connection,
		Instance:    input.Instance,
		Title:       input.Title,
		Description: input.Description,
		System:      input.System,
	}
}

// AgentToolRefFromRef converts an existing protocol tool ref into builder input.
func AgentToolRefFromRef(value *AgentToolRef) AgentToolRef {
	if value == nil {
		return AgentToolRef{}
	}
	return AgentToolRef{
		Plugin:      value.Plugin,
		Operation:   value.Operation,
		Connection:  value.Connection,
		Instance:    value.Instance,
		Title:       value.Title,
		Description: value.Description,
		System:      value.System,
	}
}

func agentMessageInputsFromMessages(values []*AgentMessage) ([]AgentMessage, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]AgentMessage, 0, len(values))
	for index, value := range values {
		input, err := AgentMessageFromMessage(value)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", index, err)
		}
		out = append(out, input)
	}
	return out, nil
}

func agentMessagesFromPtrs(values []*AgentMessage) []AgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentMessage, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func agentMessagePartsFromInputs(values []AgentMessagePart) ([]AgentMessagePart, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]AgentMessagePart, 0, len(values))
	for index, value := range values {
		part, err := NewAgentMessagePart(value)
		if err != nil {
			return nil, fmt.Errorf("parts[%d]: %w", index, err)
		}
		out = append(out, *part)
	}
	return out, nil
}

func agentMessagePartInputsFromParts(values []AgentMessagePart) ([]AgentMessagePart, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]AgentMessagePart, 0, len(values))
	for index, value := range values {
		input, err := AgentMessagePartFromPart(&value)
		if err != nil {
			return nil, fmt.Errorf("parts[%d]: %w", index, err)
		}
		out = append(out, input)
	}
	return out, nil
}

func agentToolRefsFromInputs(values []AgentToolRef) []*AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, NewAgentToolRef(value))
	}
	return out
}

func agentToolRefsFromPtrs(values []*AgentToolRef) []AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentToolRef, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func inferAgentMessagePartType(input AgentMessagePart) AgentMessagePartType {
	switch {
	case input.ToolCall != nil:
		return AgentMessagePartTypeToolCall
	case input.ToolResult != nil:
		return AgentMessagePartTypeToolResult
	case input.ImageRef != nil:
		return AgentMessagePartTypeImageRef
	case input.JSON != nil:
		return AgentMessagePartTypeJSON
	case input.Text != "":
		return AgentMessagePartTypeText
	default:
		return AgentMessagePartTypeUnspecified
	}
}

func newOptionalAgentToolCall(input *AgentMessagePartToolCall) (*AgentMessagePartToolCall, error) {
	if input == nil {
		return nil, nil
	}
	return NewAgentMessagePartToolCall(*input)
}

func newOptionalAgentToolResult(input *AgentMessagePartToolResult) (*AgentMessagePartToolResult, error) {
	if input == nil {
		return nil, nil
	}
	return NewAgentMessagePartToolResult(*input)
}

func newOptionalAgentImageRef(input *AgentMessagePartImageRef) *AgentMessagePartImageRef {
	if input == nil {
		return nil
	}
	return NewAgentMessagePartImageRef(*input)
}

func agentToolCallInputPtrFromCall(value *AgentMessagePartToolCall) *AgentMessagePartToolCall {
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolCall{
		ID:        value.ID,
		ToolID:    value.ToolID,
		Arguments: value.Arguments,
	}
}

func agentToolResultInputPtrFromResult(value *AgentMessagePartToolResult) *AgentMessagePartToolResult {
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolResult{
		ToolCallID: value.ToolCallID,
		Status:     value.Status,
		Content:    value.Content,
		Output:     value.Output,
	}
}

func agentImageRefInputPtrFromRef(value *AgentMessagePartImageRef) *AgentMessagePartImageRef {
	if value == nil {
		return nil
	}
	return &AgentMessagePartImageRef{
		URI:      value.URI,
		MimeType: value.MimeType,
	}
}

func agentMapFromAny(value any) (map[string]any, error) {
	structValue, err := StructFromAny(value)
	if err != nil {
		return nil, err
	}
	return MapFromStruct(structValue), nil
}
