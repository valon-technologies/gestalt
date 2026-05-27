package workflows

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/internal/agentwire"
)

func workflowTargetToProto(target coreworkflow.Target) (*proto.BoundWorkflowTarget, error) {
	steps, err := workflowStepsToProto(target.Steps)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{Steps: steps}, nil
}

func workflowTargetFromProto(target *proto.BoundWorkflowTarget) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	return coreworkflow.Target{Steps: workflowStepsFromProto(target.GetSteps())}
}

func workflowStepsToProto(steps []coreworkflow.Step) ([]*proto.WorkflowStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowStep, 0, len(steps))
	for i := range steps {
		step, err := workflowStepToProto(steps[i])
		if err != nil {
			return nil, fmt.Errorf("workflow steps[%d]: %w", i, err)
		}
		out = append(out, step)
	}
	return out, nil
}

func workflowStepsFromProto(steps []*proto.WorkflowStep) []coreworkflow.Step {
	if len(steps) == 0 {
		return nil
	}
	out := make([]coreworkflow.Step, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			continue
		}
		out = append(out, workflowStepFromProto(step))
	}
	return out
}

func workflowStepToProto(step coreworkflow.Step) (*proto.WorkflowStep, error) {
	inputs, err := workflowValueMapToProto(step.Inputs)
	if err != nil {
		return nil, fmt.Errorf("inputs: %w", err)
	}
	when, err := workflowStepWhenToProto(step.When)
	if err != nil {
		return nil, fmt.Errorf("when: %w", err)
	}
	metadata, err := structFromMap(step.Metadata)
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	out := &proto.WorkflowStep{
		Id:             step.ID,
		Inputs:         inputs,
		When:           when,
		TimeoutSeconds: int32(step.TimeoutSeconds),
		Metadata:       metadata,
	}
	switch {
	case step.App != nil && step.Agent != nil:
		return nil, fmt.Errorf("cannot set both app and agent")
	case step.App != nil:
		app, err := workflowStepAppCallToProto(step.App)
		if err != nil {
			return nil, fmt.Errorf("app: %w", err)
		}
		out.Action = &proto.WorkflowStep_App{App: app}
	case step.Agent != nil:
		agent, err := workflowStepAgentTurnToProto(step.Agent)
		if err != nil {
			return nil, fmt.Errorf("agent: %w", err)
		}
		out.Action = &proto.WorkflowStep_Agent{Agent: agent}
	}
	return out, nil
}

func workflowStepFromProto(step *proto.WorkflowStep) coreworkflow.Step {
	out := coreworkflow.Step{
		ID:             strings.TrimSpace(step.GetId()),
		Inputs:         workflowValueMapFromProto(step.GetInputs()),
		When:           workflowStepWhenFromProto(step.GetWhen()),
		TimeoutSeconds: int(step.GetTimeoutSeconds()),
		Metadata:       mapFromStruct(step.GetMetadata()),
	}
	if step.GetApp() != nil {
		out.App = workflowStepAppCallFromProto(step.GetApp())
	}
	if step.GetAgent() != nil {
		out.Agent = workflowStepAgentTurnFromProto(step.GetAgent())
	}
	return out
}

func workflowStepAppCallToProto(target *coreworkflow.AppCall) (*proto.WorkflowStepAppCall, error) {
	if target == nil {
		return nil, nil
	}
	input, err := workflowValueToProto(target.Input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	return &proto.WorkflowStepAppCall{
		Name:           target.Name,
		Operation:      target.Operation,
		Input:          input,
		Connection:     target.Connection,
		Instance:       target.Instance,
		CredentialMode: string(target.CredentialMode),
	}, nil
}

func workflowStepAppCallFromProto(target *proto.WorkflowStepAppCall) *coreworkflow.AppCall {
	if target == nil {
		return nil
	}
	return &coreworkflow.AppCall{
		Name:           strings.TrimSpace(target.GetName()),
		Operation:      strings.TrimSpace(target.GetOperation()),
		Connection:     strings.TrimSpace(target.GetConnection()),
		Instance:       strings.TrimSpace(target.GetInstance()),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(target.GetCredentialMode())),
		Input:          workflowValueFromProto(target.GetInput()),
	}
}

func workflowStepAgentTurnToProto(target *coreworkflow.AgentTurn) (*proto.WorkflowStepAgentTurn, error) {
	if target == nil {
		return nil, nil
	}
	messages, err := workflowAgentMessagesToProto(target.Messages)
	if err != nil {
		return nil, err
	}
	output, err := workflowAgentOutputToProto(target.Output)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	modelOptions, err := structFromMap(target.ModelOptions)
	if err != nil {
		return nil, fmt.Errorf("model_options: %w", err)
	}
	return &proto.WorkflowStepAgentTurn{
		Provider:     target.ProviderName,
		Model:        target.Model,
		SessionKey:   target.SessionKey,
		Prompt:       workflowTextToProto(target.Prompt),
		Messages:     messages,
		Tools:        agentwire.ToolRefsToProto(target.ToolRefs),
		Output:       output,
		ModelOptions: modelOptions,
	}, nil
}

func workflowStepAgentTurnFromProto(target *proto.WorkflowStepAgentTurn) *coreworkflow.AgentTurn {
	if target == nil {
		return nil
	}
	return &coreworkflow.AgentTurn{
		ProviderName: strings.TrimSpace(target.GetProvider()),
		Model:        strings.TrimSpace(target.GetModel()),
		SessionKey:   strings.TrimSpace(target.GetSessionKey()),
		Prompt:       workflowTextFromProto(target.GetPrompt()),
		Messages:     workflowAgentMessagesFromProto(target.GetMessages()),
		ToolRefs:     agentwire.ToolRefsFromProto(target.GetTools()),
		Output:       workflowAgentOutputFromProto(target.GetOutput()),
		ModelOptions: mapFromStruct(target.GetModelOptions()),
	}
}

func workflowAgentOutputToProto(output coreagent.Output) (*proto.AgentOutput, error) {
	textSet := output.Text != nil
	structuredSet := output.Structured != nil
	if textSet == structuredSet {
		return nil, fmt.Errorf("exactly one of output.text or output.structured is required")
	}
	if output.Structured != nil {
		responseSchema, err := structFromMap(output.Structured.ResponseSchema)
		if err != nil {
			return nil, err
		}
		return &proto.AgentOutput{
			Kind: &proto.AgentOutput_Structured{
				Structured: &proto.AgentStructuredOutput{ResponseSchema: responseSchema},
			},
		}, nil
	}
	if output.Text != nil {
		return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}, nil
	}
	return nil, fmt.Errorf("exactly one of output.text or output.structured is required")
}

func workflowAgentOutputFromProto(output *proto.AgentOutput) coreagent.Output {
	if output == nil {
		return coreagent.Output{}
	}
	if structured := output.GetStructured(); structured != nil {
		return coreagent.Output{
			Structured: &coreagent.StructuredOutput{ResponseSchema: mapFromStruct(structured.GetResponseSchema())},
		}
	}
	if output.GetText() != nil {
		return coreagent.Output{Text: &coreagent.TextOutput{}}
	}
	return coreagent.Output{}
}

func workflowAgentMessagesToProto(messages []coreworkflow.AgentMessage) ([]*proto.WorkflowAgentMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowAgentMessage, 0, len(messages))
	for i := range messages {
		message := messages[i]
		metadata, err := structFromMap(message.Metadata)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].metadata: %w", i, err)
		}
		out = append(out, &proto.WorkflowAgentMessage{
			Role:     message.Role,
			Text:     workflowTextToProto(message.Text),
			Metadata: metadata,
		})
	}
	return out, nil
}

func workflowAgentMessagesFromProto(messages []*proto.WorkflowAgentMessage) []coreworkflow.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreworkflow.AgentMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		out = append(out, coreworkflow.AgentMessage{
			Role:     strings.TrimSpace(message.GetRole()),
			Text:     workflowTextFromProto(message.GetText()),
			Metadata: mapFromStruct(message.GetMetadata()),
		})
	}
	return out
}

func workflowTextToProto(text coreworkflow.Text) *proto.WorkflowText {
	if text.Template == "" {
		return nil
	}
	return &proto.WorkflowText{Template: text.Template}
}

func workflowTextFromProto(text *proto.WorkflowText) coreworkflow.Text {
	if text == nil {
		return coreworkflow.Text{}
	}
	return coreworkflow.Text{Template: text.GetTemplate()}
}

func workflowStepWhenToProto(when *coreworkflow.StepWhen) (*proto.WorkflowStepWhen, error) {
	if when == nil {
		return nil, nil
	}
	if !when.EqualsSet {
		return nil, fmt.Errorf("equals is required")
	}
	value, err := workflowValueToProto(when.Value)
	if err != nil {
		return nil, fmt.Errorf("value: %w", err)
	}
	equals, err := protoValueFromAny(when.Equals)
	if err != nil {
		return nil, fmt.Errorf("equals: %w", err)
	}
	return &proto.WorkflowStepWhen{Value: value, Equals: equals}, nil
}

func workflowStepWhenFromProto(when *proto.WorkflowStepWhen) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	return &coreworkflow.StepWhen{
		Value:     workflowValueFromProto(when.GetValue()),
		Equals:    protoValueToAny(when.GetEquals()),
		EqualsSet: when.Equals != nil,
	}
}

func workflowValueMapToProto(values map[string]coreworkflow.Value) (map[string]*proto.WorkflowValue, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*proto.WorkflowValue, len(values))
	for key := range values {
		converted, err := workflowValueToProto(values[key])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = converted
	}
	return out, nil
}

func workflowValueMapFromProto(values map[string]*proto.WorkflowValue) map[string]coreworkflow.Value {
	if len(values) == 0 {
		return nil
	}
	return workflowValueObjectMapFromProto(values)
}

func workflowValueObjectMapFromProto(values map[string]*proto.WorkflowValue) map[string]coreworkflow.Value {
	out := make(map[string]coreworkflow.Value, len(values))
	for key, value := range values {
		out[key] = workflowValueFromProto(value)
	}
	return out
}

func workflowValueToProto(value coreworkflow.Value) (*proto.WorkflowValue, error) {
	set := 0
	if value.LiteralSet {
		set++
	}
	if value.Object != nil {
		set++
	}
	if value.Array != nil {
		set++
	}
	if value.Template != nil {
		set++
	}
	if strings.TrimSpace(value.RunInput) != "" {
		set++
	}
	if strings.TrimSpace(value.SignalPayload) != "" {
		set++
	}
	if value.StepOutput != nil {
		set++
	}
	if set == 0 {
		return nil, nil
	}
	if set != 1 {
		return nil, fmt.Errorf("must set exactly one value kind")
	}
	switch {
	case value.LiteralSet:
		literal, err := protoValueFromAny(value.Literal)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Literal{Literal: literal}}, nil
	case value.Object != nil:
		fields, err := workflowValueMapToProto(value.Object)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Object{Object: &proto.WorkflowObject{Fields: fields}}}, nil
	case value.Array != nil:
		items := make([]*proto.WorkflowValue, 0, len(value.Array))
		for i := range value.Array {
			item, err := workflowValueToProto(value.Array[i])
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			items = append(items, item)
		}
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Array{Array: &proto.WorkflowArray{Values: items}}}, nil
	case value.Template != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_Template{Template: workflowTextToProto(*value.Template)}}, nil
	case strings.TrimSpace(value.RunInput) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_RunInput{RunInput: &proto.WorkflowPathSource{Path: value.RunInput}}}, nil
	case strings.TrimSpace(value.SignalPayload) != "":
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_SignalPayload{SignalPayload: &proto.WorkflowPathSource{Path: value.SignalPayload}}}, nil
	case value.StepOutput != nil:
		return &proto.WorkflowValue{Kind: &proto.WorkflowValue_StepOutput{StepOutput: &proto.WorkflowStepOutputSource{
			StepId: value.StepOutput.StepID,
			Path:   value.StepOutput.Path,
		}}}, nil
	default:
		return nil, nil
	}
}

func workflowValueFromProto(value *proto.WorkflowValue) coreworkflow.Value {
	if value == nil {
		return coreworkflow.Value{}
	}
	switch typed := value.GetKind().(type) {
	case *proto.WorkflowValue_Literal:
		return coreworkflow.Value{Literal: protoValueToAny(typed.Literal), LiteralSet: true}
	case *proto.WorkflowValue_Object:
		return coreworkflow.Value{Object: workflowValueObjectMapFromProto(typed.Object.GetFields())}
	case *proto.WorkflowValue_Array:
		items := typed.Array.GetValues()
		out := make([]coreworkflow.Value, 0, len(items))
		for _, item := range items {
			out = append(out, workflowValueFromProto(item))
		}
		return coreworkflow.Value{Array: out}
	case *proto.WorkflowValue_Template:
		text := workflowTextFromProto(typed.Template)
		return coreworkflow.Value{Template: &text}
	case *proto.WorkflowValue_RunInput:
		return coreworkflow.Value{RunInput: strings.TrimSpace(typed.RunInput.GetPath())}
	case *proto.WorkflowValue_SignalPayload:
		return coreworkflow.Value{SignalPayload: strings.TrimSpace(typed.SignalPayload.GetPath())}
	case *proto.WorkflowValue_StepOutput:
		return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{
			StepID: strings.TrimSpace(typed.StepOutput.GetStepId()),
			Path:   strings.TrimSpace(typed.StepOutput.GetPath()),
		}}
	default:
		return coreworkflow.Value{}
	}
}
