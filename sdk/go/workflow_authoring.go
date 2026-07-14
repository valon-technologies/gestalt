package gestalt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// DefineWorkflow starts a typed workflow definition builder.
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

type workflowLoweringContract struct {
	Cases []workflowLoweringCase `json:"cases"`
}

type workflowLoweringCase struct {
	Name         string                     `json:"name"`
	Init         workflowLoweringInit       `json:"init"`
	Activations  []workflowLoweringActivation `json:"activations"`
	Steps        []workflowLoweringStep     `json:"steps"`
	ExpectedSpec map[string]any             `json:"expectedSpec"`
}

type workflowLoweringInit struct {
	ID     string `json:"id"`
	RunAs  string `json:"runAs"`
	Paused bool   `json:"paused"`
}

type workflowLoweringActivation struct {
	ID       string                    `json:"id"`
	Event    *workflowLoweringEvent    `json:"event"`
	Schedule *workflowLoweringSchedule `json:"schedule"`
	Input    workflowLoweringValueNode `json:"input"`
	Paused   bool                      `json:"paused"`
}

type workflowLoweringEvent struct {
	Type    string `json:"type"`
	Source  string `json:"source"`
	Subject string `json:"subject"`
}

type workflowLoweringSchedule struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

type workflowLoweringStep struct {
	ID             string                     `json:"id"`
	Inputs         workflowLoweringValueNode  `json:"inputs"`
	App            *workflowLoweringAppStep   `json:"app"`
	Agent          *workflowLoweringAgentStep `json:"agent"`
	When           *workflowLoweringWhen      `json:"when"`
	TimeoutSeconds int32                      `json:"timeoutSeconds"`
	Metadata       any                        `json:"metadata"`
}

type workflowLoweringAppStep struct {
	Name           string                    `json:"name"`
	Operation      string                    `json:"operation"`
	Input          workflowLoweringValueNode `json:"input"`
	Connection     string                    `json:"connection"`
	Instance       string                    `json:"instance"`
	CredentialMode string                    `json:"credentialMode"`
}

type workflowLoweringAgentStep struct {
	Provider     string                           `json:"provider"`
	Model        string                           `json:"model"`
	SessionKey   string                           `json:"sessionKey"`
	Prompt       workflowLoweringValueNode        `json:"prompt"`
	Messages     []workflowLoweringAgentMessage   `json:"messages"`
	Tools        []AgentToolRef                   `json:"tools"`
	Output       *AgentOutput                     `json:"output"`
	ModelOptions any                              `json:"modelOptions"`
}

type workflowLoweringAgentMessage struct {
	Role string                    `json:"role"`
	Text workflowLoweringValueNode `json:"text"`
}

type workflowLoweringWhen struct {
	Value  workflowLoweringValueNode `json:"value"`
	Equals any                       `json:"equals"`
}

type workflowLoweringValueNode map[string]any

// LoadWorkflowLoweringContract loads the shared workflow authoring fixture.
func LoadWorkflowLoweringContract() (workflowLoweringContract, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return workflowLoweringContract{}, fmt.Errorf("resolve workflow authoring fixture path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "fixtures", "workflow-authoring", "lowering-contract.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return workflowLoweringContract{}, err
	}
	var contract workflowLoweringContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return workflowLoweringContract{}, err
	}
	return contract, nil
}

// BuildWorkflowFromLoweringCase builds a workflow from one shared fixture case.
func BuildWorkflowFromLoweringCase(caseData workflowLoweringCase) (*WorkflowBuilder, error) {
	builder, err := DefineWorkflow(DefineWorkflowOptions{
		ID:     caseData.Init.ID,
		RunAs:  caseData.Init.RunAs,
		Paused: caseData.Init.Paused,
	})
	if err != nil {
		return nil, err
	}
	for _, activation := range caseData.Activations {
		if activation.Event != nil {
			var mapInput func(WorkflowEventScope) map[string]WorkflowValue
			if activation.Input != nil {
				fields, err := lowerContractObject(activation.Input)
				if err != nil {
					return nil, err
				}
				mapInput = func(WorkflowEventScope) map[string]WorkflowValue { return fields }
			}
			builder.On(Event(activation.Event.Type, mapInput, WorkflowEventActivationOptions{
				ID:      activation.ID,
				Source:  activation.Event.Source,
				Subject: activation.Event.Subject,
				Paused:  activation.Paused,
			}))
			continue
		}
		if activation.Schedule != nil {
			var mapInput func(WorkflowActivationScope) map[string]WorkflowValue
			if activation.Input != nil {
				fields, err := lowerContractObject(activation.Input)
				if err != nil {
					return nil, err
				}
				mapInput = func(WorkflowActivationScope) map[string]WorkflowValue { return fields }
			}
			builder.On(Schedule(activation.Schedule.Cron, mapInput, WorkflowScheduleActivationOptions{
				ID:       activation.ID,
				Timezone: activation.Schedule.Timezone,
				Paused:   activation.Paused,
			}))
		}
	}
	for _, step := range caseData.Steps {
		config := WorkflowStepConfig{
			TimeoutSeconds: step.TimeoutSeconds,
			Metadata:       step.Metadata,
		}
		if step.Inputs != nil {
			fields, err := lowerContractObject(step.Inputs)
			if err != nil {
				return nil, err
			}
			config.Inputs = func(WorkflowStepScope) map[string]WorkflowValue { return fields }
		}
		if step.App != nil {
			var input func(WorkflowStepScope) map[string]WorkflowValue
			if step.App.Input != nil {
				fields, err := lowerContractObject(step.App.Input)
				if err != nil {
					return nil, err
				}
				input = func(WorkflowStepScope) map[string]WorkflowValue { return fields }
			}
			config.App = &WorkflowStepAppConfig{
				Name:           step.App.Name,
				Operation:      step.App.Operation,
				Input:          input,
				Connection:     step.App.Connection,
				Instance:       step.App.Instance,
				CredentialMode: step.App.CredentialMode,
			}
		}
		if step.Agent != nil {
			prompt, err := lowerContractTemplate(step.Agent.Prompt)
			if err != nil {
				return nil, err
			}
			messages := make([]WorkflowStepAgentMessageConfig, 0, len(step.Agent.Messages))
			for _, message := range step.Agent.Messages {
				text, err := lowerContractText(message.Text)
				if err != nil {
					return nil, err
				}
				messages = append(messages, WorkflowStepAgentMessageConfig{
					Role: message.Role,
					Text: text,
				})
			}
			config.Agent = &WorkflowStepAgentConfig{
				Provider:     step.Agent.Provider,
				Model:        step.Agent.Model,
				SessionKey:   step.Agent.SessionKey,
				Prompt:       prompt,
				Messages:     messages,
				Tools:        step.Agent.Tools,
				Output:       step.Agent.Output,
				ModelOptions: step.Agent.ModelOptions,
			}
		}
		if step.When != nil {
			value, err := lowerContractValue(step.When.Value)
			if err != nil {
				return nil, err
			}
			config.When = &WorkflowStepWhenConfig{
				Value:  value,
				Equals: step.When.Equals,
			}
		}
		builder.Step(step.ID, config)
	}
	return builder, nil
}

func lowerContractObject(node workflowLoweringValueNode) (map[string]WorkflowValue, error) {
	if node["kind"] != "object" {
		return nil, fmt.Errorf("lowering contract input must be an object node")
	}
	fields, ok := node["fields"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("lowering contract object node requires fields")
	}
	out := make(map[string]WorkflowValue, len(fields))
	for key, nested := range fields {
		value, err := lowerContractValue(workflowLoweringValueNode(nested.(map[string]any)))
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func lowerContractValue(node workflowLoweringValueNode) (WorkflowValue, error) {
	kind, _ := node["kind"].(string)
	switch kind {
	case "input":
		path, _ := node["path"].(string)
		return WorkflowRefInput(path), nil
	case "signal":
		path, _ := node["path"].(string)
		return WorkflowRefSignal(path), nil
	case "stepOutput":
		stepID, _ := node["stepId"].(string)
		path, _ := node["path"].(string)
		return WorkflowRefStepOutput(stepID, path), nil
	case "stepInput":
		stepID, _ := node["stepId"].(string)
		path, _ := node["path"].(string)
		return WorkflowRefStepInput(stepID, path), nil
	case "literal":
		return WorkflowRefLiteral(node["value"]), nil
	case "template":
		template, _ := node["template"].(string)
		return WorkflowRefTemplate(template), nil
	case "object":
		return WorkflowValue{Object: mustLowerContractObject(node)}, nil
	case "array":
		values, ok := node["values"].([]any)
		if !ok {
			return WorkflowValue{}, fmt.Errorf("workflow array node requires values")
		}
		out := make([]WorkflowValue, 0, len(values))
		for _, item := range values {
			value, err := lowerContractValue(workflowLoweringValueNode(item.(map[string]any)))
			if err != nil {
				return WorkflowValue{}, err
			}
			out = append(out, value)
		}
		return WorkflowRefArray(out...), nil
	default:
		return WorkflowValue{}, fmt.Errorf("unsupported workflow value kind: %s", kind)
	}
}

func mustLowerContractObject(node workflowLoweringValueNode) map[string]WorkflowValue {
	out, err := lowerContractObject(node)
	if err != nil {
		panic(err)
	}
	return out
}

func lowerContractTemplate(node workflowLoweringValueNode) (string, error) {
	if node["kind"] != "template" {
		return "", fmt.Errorf("agent prompt must be a template node in lowering contract")
	}
	template, _ := node["template"].(string)
	return template, nil
}

func lowerContractText(node workflowLoweringValueNode) (string, error) {
	switch node["kind"] {
	case "literal":
		text, _ := node["value"].(string)
		return text, nil
	case "template":
		template, _ := node["template"].(string)
		return template, nil
	default:
		return "", fmt.Errorf("agent message text must be literal or template")
	}
}

func joinWorkflowPath(prefix, path string) string {
	prefix = strings.TrimSpace(prefix)
	path = strings.TrimSpace(path)
	switch {
	case prefix == "":
		return path
	case path == "":
		return prefix
	default:
		return prefix + "." + path
	}
}

// CanonicalWorkflowDefinitionSpec returns a JSON-friendly spec for golden tests.
func CanonicalWorkflowDefinitionSpec(spec WorkflowDefinitionSpec) (map[string]any, error) {
	out := map[string]any{
		"id":     spec.ID,
		"runAs":  workflowSubjectID(spec.RunAs),
		"paused": spec.Paused,
	}
	activations := make([]any, 0, len(spec.Activations))
	for _, activation := range spec.Activations {
		activations = append(activations, canonicalWorkflowActivation(activation))
	}
	out["activations"] = activations
	if spec.Target != nil {
		out["target"] = canonicalWorkflowTarget(*spec.Target)
	}
	return out, nil
}

func canonicalWorkflowActivation(activation WorkflowActivation) map[string]any {
	out := map[string]any{
		"id":     activation.ID,
		"paused": activation.Paused,
	}
	if activation.Schedule != nil {
		out["schedule"] = map[string]any{
			"cron":     activation.Schedule.Cron,
			"timezone": activation.Schedule.Timezone,
		}
	}
	if activation.Event != nil {
		match := activation.Event.Match
		if match == nil {
			match = &WorkflowEventMatch{}
		}
		out["event"] = map[string]any{
			"match": map[string]any{
				"type":    match.Type,
				"source":  match.Source,
				"subject": match.Subject,
			},
		}
	}
	if !workflowValueIsEmpty(activation.Input) {
		out["input"] = canonicalWorkflowValue(activation.Input)
	}
	return out
}

func canonicalWorkflowTarget(target BoundWorkflowTarget) map[string]any {
	steps := make([]any, 0, len(target.Steps))
	for _, step := range target.Steps {
		steps = append(steps, canonicalWorkflowStep(step))
	}
	return map[string]any{"steps": steps}
}

func canonicalWorkflowStep(step WorkflowStep) map[string]any {
	out := map[string]any{"id": step.ID}
	if len(step.Inputs) > 0 {
		out["inputs"] = canonicalWorkflowValueMap(step.Inputs)
	}
	if step.App != nil {
		app := map[string]any{
			"name":      step.App.Name,
			"operation": step.App.Operation,
		}
		if !workflowValueIsEmpty(step.App.Input) {
			app["input"] = canonicalWorkflowValue(step.App.Input)
		}
		out["app"] = app
	}
	if step.Agent != nil {
		agent := map[string]any{
			"provider": step.Agent.Provider,
			"model":    step.Agent.Model,
		}
		if step.Agent.Prompt.Template != "" {
			agent["prompt"] = map[string]any{"template": step.Agent.Prompt.Template}
		}
		if len(step.Agent.Messages) > 0 {
			messages := make([]any, 0, len(step.Agent.Messages))
			for _, message := range step.Agent.Messages {
				messages = append(messages, map[string]any{
					"role": message.Role,
					"text": map[string]any{"template": message.Text.Template},
				})
			}
			agent["messages"] = messages
		}
		if len(step.Agent.Tools) > 0 {
			tools := make([]any, 0, len(step.Agent.Tools))
			for _, tool := range step.Agent.Tools {
				tools = append(tools, map[string]any{
					"app":       tool.App,
					"operation": tool.Operation,
				})
			}
			agent["tools"] = tools
		}
		if step.Agent.Output != nil {
			output := map[string]any{}
			if step.Agent.Output.Structured != nil && step.Agent.Output.Structured.Schema != nil {
				output["structured"] = map[string]any{
					"schema": step.Agent.Output.Structured.Schema,
				}
			}
			if len(output) > 0 {
				agent["output"] = output
			}
		}
		if step.Agent.ModelOptions != nil {
			agent["modelOptions"] = step.Agent.ModelOptions
		}
		out["agent"] = agent
	}
	if step.When != nil {
		out["when"] = map[string]any{
			"value":  canonicalWorkflowValue(step.When.Value),
			"equals": step.When.Equals,
		}
	}
	if step.TimeoutSeconds != 0 {
		out["timeoutSeconds"] = step.TimeoutSeconds
	}
	if step.Metadata != nil {
		out["metadata"] = step.Metadata
	}
	return out
}

func canonicalWorkflowValueMap(values map[string]WorkflowValue) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = canonicalWorkflowValue(value)
	}
	return out
}

func canonicalWorkflowValue(value WorkflowValue) map[string]any {
	switch {
	case value.LiteralSet:
		return map[string]any{"literal": value.Literal}
	case value.Template != nil:
		return map[string]any{"template": value.Template.Template}
	case strings.TrimSpace(value.Input) != "":
		return map[string]any{"input": value.Input}
	case strings.TrimSpace(value.Signal) != "":
		return map[string]any{"signal": value.Signal}
	case value.StepOutput != nil:
		return map[string]any{"stepOutput": map[string]any{
			"stepId": value.StepOutput.StepID,
			"path":   value.StepOutput.Path,
		}}
	case value.StepInput != nil:
		return map[string]any{"stepInput": map[string]any{
			"stepId": value.StepInput.StepID,
			"path":   value.StepInput.Path,
		}}
	case value.Object != nil:
		return map[string]any{"object": canonicalWorkflowValueMap(value.Object)}
	case value.Array != nil:
		items := make([]any, 0, len(value.Array))
		for _, item := range value.Array {
			items = append(items, canonicalWorkflowValue(item))
		}
		return map[string]any{"array": items}
	default:
		return map[string]any{}
	}
}

func workflowValueIsEmpty(value WorkflowValue) bool {
	return !value.LiteralSet &&
		value.Object == nil &&
		value.Array == nil &&
		value.Template == nil &&
		strings.TrimSpace(value.Input) == "" &&
		strings.TrimSpace(value.Signal) == "" &&
		value.StepOutput == nil &&
		value.StepInput == nil
}
