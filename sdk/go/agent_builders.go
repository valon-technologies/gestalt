package gestalt

import "fmt"

// AgentMessageInput contains fields for constructing an AgentMessage.
type AgentMessageInput struct {
	Role     string
	Text     string
	Parts    []AgentMessagePartInput
	Metadata any
}

// AgentMessagePartInput contains fields for constructing an
// AgentMessagePart. When Type is unspecified, the builder infers it from the
// first payload field that is set.
type AgentMessagePartInput struct {
	Type       AgentMessagePartType
	Text       string
	JSON       any
	ToolCall   *AgentMessagePartToolCallInput
	ToolResult *AgentMessagePartToolResultInput
	ImageRef   *AgentMessagePartImageRefInput
}

// AgentMessagePartToolCallInput contains fields for an agent tool
// call message part.
type AgentMessagePartToolCallInput struct {
	ID        string
	ToolID    string
	Arguments any
}

// AgentMessagePartToolResultInput contains fields for an agent tool
// result message part.
type AgentMessagePartToolResultInput struct {
	ToolCallID string
	Status     int32
	Content    string
	Output     any
}

// AgentMessagePartImageRefInput contains fields for an image
// reference message part.
type AgentMessagePartImageRefInput struct {
	URI      string
	MimeType string
}

// AgentToolRefInput contains fields for constructing an AgentToolRef.
type AgentToolRefInput struct {
	Plugin      string
	Operation   string
	Connection  string
	Instance    string
	Title       string
	Description string
	System      string
}

// AgentWorkspaceInput contains fields for constructing an
// AgentWorkspace.
type AgentWorkspaceInput struct {
	Checkouts []AgentWorkspaceGitCheckoutInput
	CWD       string
}

// AgentWorkspaceGitCheckoutInput contains fields for constructing an
// AgentWorkspaceGitCheckout.
type AgentWorkspaceGitCheckoutInput struct {
	URL  string
	Ref  string
	Path string
}

// NewAgentMessage creates an agent message.
func NewAgentMessage(input AgentMessageInput) (*AgentMessage, error) {
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

// AgentMessageInputFromMessage converts an existing protocol message into
// builder input.
func AgentMessageInputFromMessage(value *AgentMessage) (AgentMessageInput, error) {
	if value == nil {
		return AgentMessageInput{}, nil
	}
	parts, err := agentMessagePartInputsFromParts(value.Parts)
	if err != nil {
		return AgentMessageInput{}, err
	}
	return AgentMessageInput{
		Role:     value.Role,
		Text:     value.Text,
		Parts:    parts,
		Metadata: value.Metadata,
	}, nil
}

// NewAgentMessagePart creates an agent message part.
func NewAgentMessagePart(input AgentMessagePartInput) (*AgentMessagePart, error) {
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

// AgentMessagePartInputFromPart converts an existing protocol part into builder input.
func AgentMessagePartInputFromPart(value *AgentMessagePart) (AgentMessagePartInput, error) {
	if value == nil {
		return AgentMessagePartInput{}, nil
	}
	return AgentMessagePartInput{
		Type:       value.Type,
		Text:       value.Text,
		JSON:       value.JSON,
		ToolCall:   agentToolCallInputPtrFromCall(value.ToolCall),
		ToolResult: agentToolResultInputPtrFromResult(value.ToolResult),
		ImageRef:   agentImageRefInputPtrFromRef(value.ImageRef),
	}, nil
}

// NewAgentMessagePartToolCall creates a tool-call payload.
func NewAgentMessagePartToolCall(input AgentMessagePartToolCallInput) (*AgentMessagePartToolCall, error) {
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
func NewAgentMessagePartToolResult(input AgentMessagePartToolResultInput) (*AgentMessagePartToolResult, error) {
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
func NewAgentMessagePartImageRef(input AgentMessagePartImageRefInput) *AgentMessagePartImageRef {
	return &AgentMessagePartImageRef{
		URI:      input.URI,
		MimeType: input.MimeType,
	}
}

// NewAgentToolRef creates an agent tool reference.
func NewAgentToolRef(input AgentToolRefInput) *AgentToolRef {
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

// AgentToolRefInputFromRef converts an existing protocol tool ref into builder input.
func AgentToolRefInputFromRef(value *AgentToolRef) AgentToolRefInput {
	if value == nil {
		return AgentToolRefInput{}
	}
	return AgentToolRefInput{
		Plugin:      value.Plugin,
		Operation:   value.Operation,
		Connection:  value.Connection,
		Instance:    value.Instance,
		Title:       value.Title,
		Description: value.Description,
		System:      value.System,
	}
}

// NewAgentWorkspace creates an agent workspace.
func NewAgentWorkspace(input AgentWorkspaceInput) *AgentProtocolWorkspace {
	checkouts := make([]AgentProtocolWorkspaceGitCheckout, 0, len(input.Checkouts))
	for _, checkout := range input.Checkouts {
		checkouts = append(checkouts, AgentProtocolWorkspaceGitCheckout{
			URL:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return &AgentProtocolWorkspace{
		Checkouts: checkouts,
		Cwd:       input.CWD,
	}
}

func agentMessagesFromInputs(values []AgentMessageInput) ([]*AgentMessage, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*AgentMessage, 0, len(values))
	for index, value := range values {
		message, err := NewAgentMessage(value)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", index, err)
		}
		out = append(out, message)
	}
	return out, nil
}

func agentMessageInputsFromMessages(values []*AgentMessage) ([]AgentMessageInput, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]AgentMessageInput, 0, len(values))
	for index, value := range values {
		input, err := AgentMessageInputFromMessage(value)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", index, err)
		}
		out = append(out, input)
	}
	return out, nil
}

func agentMessagePartsFromInputs(values []AgentMessagePartInput) ([]AgentMessagePart, error) {
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

func agentMessagePartInputsFromParts(values []AgentMessagePart) ([]AgentMessagePartInput, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]AgentMessagePartInput, 0, len(values))
	for index, value := range values {
		input, err := AgentMessagePartInputFromPart(&value)
		if err != nil {
			return nil, fmt.Errorf("parts[%d]: %w", index, err)
		}
		out = append(out, input)
	}
	return out, nil
}

func agentToolRefsFromInputs(values []AgentToolRefInput) []*AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, NewAgentToolRef(value))
	}
	return out
}

func inferAgentMessagePartType(input AgentMessagePartInput) AgentMessagePartType {
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

func newOptionalAgentToolCall(input *AgentMessagePartToolCallInput) (*AgentMessagePartToolCall, error) {
	if input == nil {
		return nil, nil
	}
	return NewAgentMessagePartToolCall(*input)
}

func newOptionalAgentToolResult(input *AgentMessagePartToolResultInput) (*AgentMessagePartToolResult, error) {
	if input == nil {
		return nil, nil
	}
	return NewAgentMessagePartToolResult(*input)
}

func newOptionalAgentImageRef(input *AgentMessagePartImageRefInput) *AgentMessagePartImageRef {
	if input == nil {
		return nil
	}
	return NewAgentMessagePartImageRef(*input)
}

func agentToolCallInputPtrFromCall(value *AgentMessagePartToolCall) *AgentMessagePartToolCallInput {
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolCallInput{
		ID:        value.ID,
		ToolID:    value.ToolID,
		Arguments: value.Arguments,
	}
}

func agentToolResultInputPtrFromResult(value *AgentMessagePartToolResult) *AgentMessagePartToolResultInput {
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolResultInput{
		ToolCallID: value.ToolCallID,
		Status:     value.Status,
		Content:    value.Content,
		Output:     value.Output,
	}
}

func agentImageRefInputPtrFromRef(value *AgentMessagePartImageRef) *AgentMessagePartImageRefInput {
	if value == nil {
		return nil
	}
	return &AgentMessagePartImageRefInput{
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
