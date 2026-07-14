package gestalt

import (
	"fmt"
	"strings"
)

// WorkflowRefInput references a run-input path in workflow authoring builders.
func WorkflowRefInput(path string) WorkflowValue {
	return WorkflowValue{Input: strings.TrimSpace(path)}
}

// WorkflowRefSignal references an activation signal path in workflow authoring builders.
func WorkflowRefSignal(path string) WorkflowValue {
	return WorkflowValue{Signal: strings.TrimSpace(path)}
}

// WorkflowRefStepOutput references a prior step output path.
func WorkflowRefStepOutput(stepID, path string) WorkflowValue {
	return WorkflowValue{StepOutput: &WorkflowStepOutputSource{
		StepID: strings.TrimSpace(stepID),
		Path:   strings.TrimSpace(path),
	}}
}

// WorkflowRefStepInput references a prior step input path.
func WorkflowRefStepInput(stepID, path string) WorkflowValue {
	return WorkflowValue{StepInput: &WorkflowStepInputSource{
		StepID: strings.TrimSpace(stepID),
		Path:   strings.TrimSpace(path),
	}}
}

// WorkflowRefLiteral builds a literal workflow value.
func WorkflowRefLiteral(value any) WorkflowValue {
	return WorkflowValue{Literal: value, LiteralSet: true}
}

// WorkflowRefTemplate builds a template workflow value.
func WorkflowRefTemplate(template string) WorkflowValue {
	text := WorkflowText{Template: template}
	return WorkflowValue{Template: &text}
}

// WorkflowRefObject builds an object workflow value from field builders.
func WorkflowRefObject(fields map[string]WorkflowValue) WorkflowValue {
	return WorkflowValue{Object: fields}
}

// WorkflowRefArray builds an array workflow value.
func WorkflowRefArray(values ...WorkflowValue) WorkflowValue {
	return WorkflowValue{Array: values}
}

// DefineWorkflowOptions configures a typed workflow builder.
type DefineWorkflowOptions struct {
	ID     string
	RunAs  string
	Paused bool
}

// WorkflowEventActivationOptions configures an event activation.
type WorkflowEventActivationOptions struct {
	ID      string
	Source  string
	Subject string
	Paused  bool
}

// WorkflowScheduleActivationOptions configures a schedule activation.
type WorkflowScheduleActivationOptions struct {
	ID       string
	Timezone string
	Paused   bool
}

// WorkflowEventActivationConfig is accepted by WorkflowBuilder.On.
type WorkflowEventActivationConfig struct {
	Type     string
	MapInput func(WorkflowEventScope) map[string]WorkflowValue
	Options  WorkflowEventActivationOptions
}

// WorkflowScheduleActivationConfig is accepted by WorkflowBuilder.On.
type WorkflowScheduleActivationConfig struct {
	Cron     string
	MapInput func(WorkflowActivationScope) map[string]WorkflowValue
	Options  WorkflowScheduleActivationOptions
}

// WorkflowEventScope exposes signal references for event activation mapping.
type WorkflowEventScope struct{}

// Data returns a signal reference under event.data.
func (WorkflowEventScope) Data(path string) WorkflowValue {
	return WorkflowRefSignal(joinWorkflowPath("data", path))
}

// Field returns a signal reference at the given path.
func (WorkflowEventScope) Field(path string) WorkflowValue {
	return WorkflowRefSignal(path)
}

// WorkflowActivationScope exposes input references for schedule activation mapping.
type WorkflowActivationScope struct{}

// Input returns a run-input reference.
func (WorkflowActivationScope) Input(path string) WorkflowValue {
	return WorkflowRefInput(path)
}

// WorkflowStepScope exposes references available while authoring a step.
type WorkflowStepScope struct{}

// Input returns a run-input reference.
func (WorkflowStepScope) Input(path string) WorkflowValue {
	return WorkflowRefInput(path)
}

// Signal returns a signal reference.
func (WorkflowStepScope) Signal(path string) WorkflowValue {
	return WorkflowRefSignal(path)
}

// StepOutput returns a prior step output reference.
func (WorkflowStepScope) StepOutput(stepID, path string) WorkflowValue {
	return WorkflowRefStepOutput(stepID, path)
}

// StepInput returns a prior step input reference.
func (WorkflowStepScope) StepInput(stepID, path string) WorkflowValue {
	return WorkflowRefStepInput(stepID, path)
}

// WorkflowStepAppConfig configures an app step action.
type WorkflowStepAppConfig struct {
	Name           string
	Operation      string
	Input          func(WorkflowStepScope) map[string]WorkflowValue
	Connection     string
	Instance       string
	CredentialMode string
}

// WorkflowStepAgentMessageConfig configures one agent message.
type WorkflowStepAgentMessageConfig struct {
	Role string
	Text string
}

// WorkflowStepAgentConfig configures an agent step action.
type WorkflowStepAgentConfig struct {
	Provider     string
	Model        string
	SessionKey   string
	Prompt       string
	Messages     []WorkflowStepAgentMessageConfig
	Tools        []AgentToolRef
	Output       *AgentOutput
	ModelOptions any
}

// WorkflowStepWhenConfig configures a step guard.
type WorkflowStepWhenConfig struct {
	Value  WorkflowValue
	Equals any
}

// WorkflowStepConfig configures one workflow step.
type WorkflowStepConfig struct {
	Inputs         func(WorkflowStepScope) map[string]WorkflowValue
	App            *WorkflowStepAppConfig
	Agent          *WorkflowStepAgentConfig
	When           *WorkflowStepWhenConfig
	TimeoutSeconds int32
	Metadata       any
}

// WorkflowBuilder incrementally authors a workflow definition spec.
type WorkflowBuilder struct {
	id          string
	runAs       string
	paused      bool
	activations []WorkflowActivation
	steps       []WorkflowStep
}

// Event creates an event activation configuration.
func Event(typeName string, mapInput func(WorkflowEventScope) map[string]WorkflowValue, options WorkflowEventActivationOptions) WorkflowEventActivationConfig {
	return WorkflowEventActivationConfig{
		Type:     typeName,
		MapInput: mapInput,
		Options:  options,
	}
}

// Schedule creates a schedule activation configuration.
func Schedule(cron string, mapInput func(WorkflowActivationScope) map[string]WorkflowValue, options WorkflowScheduleActivationOptions) WorkflowScheduleActivationConfig {
	return WorkflowScheduleActivationConfig{
		Cron:     cron,
		MapInput: mapInput,
		Options:  options,
	}
}

// DefineWorkflow starts a fluent workflow definition builder.
func DefineWorkflow(options DefineWorkflowOptions) (*WorkflowBuilder, error) {
	runAs := strings.TrimSpace(options.RunAs)
	if runAs == "" {
		return nil, fmt.Errorf("DefineWorkflow requires RunAs")
	}
	id := strings.TrimSpace(options.ID)
	if id == "" {
		return nil, fmt.Errorf("DefineWorkflow requires ID")
	}
	return &WorkflowBuilder{
		id:     id,
		runAs:  runAs,
		paused: options.Paused,
	}, nil
}

// On appends one activation to the builder.
func (b *WorkflowBuilder) On(activation any) *WorkflowBuilder {
	switch typed := activation.(type) {
	case WorkflowEventActivationConfig:
		activationID := strings.TrimSpace(typed.Options.ID)
		if activationID == "" {
			activationID = typed.Type
		}
		var input WorkflowValue
		if typed.MapInput != nil {
			input = WorkflowValue{Object: typed.MapInput(WorkflowEventScope{})}
		}
		b.activations = append(b.activations, WorkflowActivation{
			ID:     activationID,
			Paused: typed.Options.Paused,
			Event: &WorkflowEventActivation{Match: &WorkflowEventMatch{
				Type:    typed.Type,
				Source:  typed.Options.Source,
				Subject: typed.Options.Subject,
			}},
			Input: input,
		})
	case WorkflowScheduleActivationConfig:
		activationID := strings.TrimSpace(typed.Options.ID)
		if activationID == "" {
			activationID = typed.Cron
		}
		var input WorkflowValue
		if typed.MapInput != nil {
			input = WorkflowValue{Object: typed.MapInput(WorkflowActivationScope{})}
		}
		b.activations = append(b.activations, WorkflowActivation{
			ID:     activationID,
			Paused: typed.Options.Paused,
			Schedule: &WorkflowScheduleActivation{
				Cron:     typed.Cron,
				Timezone: typed.Options.Timezone,
			},
			Input: input,
		})
	}
	return b
}

// Step appends one step to the builder.
func (b *WorkflowBuilder) Step(stepID string, config WorkflowStepConfig) *WorkflowBuilder {
	scope := WorkflowStepScope{}
	step := WorkflowStep{ID: stepID}
	if config.Inputs != nil {
		step.Inputs = config.Inputs(scope)
	}
	if config.App != nil {
		var input WorkflowValue
		if config.App.Input != nil {
			input = WorkflowValue{Object: config.App.Input(scope)}
		}
		step.App = &WorkflowStepAppCall{
			Name:           config.App.Name,
			Operation:      config.App.Operation,
			Input:          input,
			Connection:     config.App.Connection,
			Instance:       config.App.Instance,
			CredentialMode: config.App.CredentialMode,
		}
	}
	if config.Agent != nil {
		messages := make([]WorkflowAgentMessage, 0, len(config.Agent.Messages))
		for _, message := range config.Agent.Messages {
			messages = append(messages, WorkflowAgentMessage{
				Role: message.Role,
				Text: WorkflowText{Template: message.Text},
			})
		}
		step.Agent = &WorkflowStepAgentTurn{
			Provider:     config.Agent.Provider,
			Model:        config.Agent.Model,
			SessionKey:   config.Agent.SessionKey,
			Prompt:       WorkflowText{Template: config.Agent.Prompt},
			Messages:     messages,
			Tools:        config.Agent.Tools,
			Output:       config.Agent.Output,
			ModelOptions: config.Agent.ModelOptions,
		}
	}
	if config.When != nil {
		step.When = &WorkflowStepWhen{
			Value:  config.When.Value,
			Equals: config.When.Equals,
		}
	}
	step.TimeoutSeconds = config.TimeoutSeconds
	step.Metadata = config.Metadata
	b.steps = append(b.steps, step)
	return b
}

// ToSpec returns the authored workflow definition spec.
func (b *WorkflowBuilder) ToSpec() WorkflowDefinitionSpec {
	var target *BoundWorkflowTarget
	if len(b.steps) > 0 {
		target = &BoundWorkflowTarget{Steps: append([]WorkflowStep(nil), b.steps...)}
	}
	return WorkflowDefinitionSpec{
		ID:          b.id,
		RunAs:       &Subject{ID: b.runAs},
		Paused:      b.paused,
		Activations: append([]WorkflowActivation(nil), b.activations...),
		Target:      target,
	}
}

// WorkflowComposeText builds workflow template text from literals and workflow value references.
func WorkflowComposeText(parts ...any) string {
	var builder strings.Builder
	for _, part := range parts {
		switch typed := part.(type) {
		case string:
			builder.WriteString(typed)
		case WorkflowValue:
			builder.WriteString(workflowValueToTemplatePlaceholder(typed))
		default:
			panic(fmt.Sprintf("WorkflowComposeText parts must be strings or WorkflowValue, got %T", part))
		}
	}
	return builder.String()
}

func workflowValueToTemplatePlaceholder(value WorkflowValue) string {
	if value.Input != "" {
		return fmt.Sprintf("${{ input.%s }}", strings.TrimSpace(value.Input))
	}
	if value.Signal != "" {
		return fmt.Sprintf("${{ signal.%s }}", strings.TrimSpace(value.Signal))
	}
	if value.StepOutput != nil {
		return fmt.Sprintf(
			"${{ steps.%s.outputs.%s }}",
			strings.TrimSpace(value.StepOutput.StepID),
			strings.TrimSpace(value.StepOutput.Path),
		)
	}
	if value.StepInput != nil {
		return fmt.Sprintf(
			"${{ steps.%s.inputs.%s }}",
			strings.TrimSpace(value.StepInput.StepID),
			strings.TrimSpace(value.StepInput.Path),
		)
	}
	panic("WorkflowComposeText references must be input, signal, step output, or step input paths")
}

// ResolveWorkflowDefinitionSpec accepts either a builder or a raw spec.
func ResolveWorkflowDefinitionSpec(input any) (WorkflowDefinitionSpec, error) {
	switch typed := input.(type) {
	case *WorkflowBuilder:
		return typed.ToSpec(), nil
	case WorkflowBuilder:
		return typed.ToSpec(), nil
	case WorkflowDefinitionSpec:
		return typed, nil
	case *WorkflowDefinitionSpec:
		if typed == nil {
			return WorkflowDefinitionSpec{}, nil
		}
		return *typed, nil
	default:
		return WorkflowDefinitionSpec{}, fmt.Errorf("unsupported workflow definition spec input %T", input)
	}
}

