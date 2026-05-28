package workflowwire

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

// ParseTargetMap converts a JSON-shaped target object into core workflow target form.
func ParseTargetMap(value any, path string) (coreworkflow.Target, error) {
	target, ok := asMap(value)
	if !ok {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(target, path, "steps"); err != nil {
		return coreworkflow.Target{}, err
	}
	steps, err := parseSteps(target["steps"], path+".steps")
	if err != nil {
		return coreworkflow.Target{}, err
	}
	if len(steps) == 0 {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.steps is required", ErrInvalid, path)
	}
	return coreworkflow.Target{Steps: steps}, nil
}

func parseSteps(value any, path string) ([]coreworkflow.Step, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := asArray(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array", ErrInvalid, path)
	}
	out := make([]coreworkflow.Step, 0, len(items))
	previousSteps := map[string]struct{}{}
	for i, item := range items {
		stepMap, ok := asMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be an object", ErrInvalid, path, i)
		}
		stepPath := fmt.Sprintf("%s[%d]", path, i)
		if err := rejectUnknownKeys(stepMap, stepPath, "id", "inputs", "app", "agent", "when", "timeoutSeconds", "metadata"); err != nil {
			return nil, err
		}
		hasApp := stepMap["app"] != nil
		hasAgent := stepMap["agent"] != nil
		if hasApp == hasAgent {
			return nil, fmt.Errorf("%w: %s must set exactly one of app or agent", ErrInvalid, stepPath)
		}
		inputs, err := parseValueMap(stepMap["inputs"], stepPath+".inputs")
		if err != nil {
			return nil, err
		}
		metadata, err := objectArg(stepMap, "metadata", stepPath)
		if err != nil {
			return nil, err
		}
		when, err := parseStepWhen(stepMap["when"], stepPath+".when")
		if err != nil {
			return nil, err
		}
		step := coreworkflow.Step{
			ID:             stringArg(stepMap, "id"),
			Inputs:         inputs,
			Metadata:       metadata,
			TimeoutSeconds: intArg(stepMap, "timeoutSeconds"),
			When:           when,
		}
		if hasApp {
			app, err := parseAppCall(stepMap["app"], stepPath+".app")
			if err != nil {
				return nil, err
			}
			step.App = app
		}
		if hasAgent {
			agent, err := parseAgentTurn(stepMap["agent"], stepPath+".agent")
			if err != nil {
				return nil, err
			}
			step.Agent = agent
		}
		stepID := strings.TrimSpace(step.ID)
		if stepID == "" {
			return nil, fmt.Errorf("%w: %s.id is required", ErrInvalid, stepPath)
		}
		if _, ok := previousSteps[stepID]; ok {
			return nil, fmt.Errorf("%w: %s.id %q is duplicated", ErrInvalid, stepPath, stepID)
		}
		if err := validateStepRefs(stepPath, step, previousSteps); err != nil {
			return nil, err
		}
		out = append(out, step)
		previousSteps[stepID] = struct{}{}
	}
	return out, nil
}

func validateStepRefs(path string, step coreworkflow.Step, previousSteps map[string]struct{}) error {
	for key, value := range step.Inputs {
		if err := coreworkflow.ValidateValueRefs(path+".inputs."+key, value, previousSteps); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if step.When != nil {
		if err := coreworkflow.ValidateStepWhen(path+".when", step.When, previousSteps); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if step.App != nil {
		if err := coreworkflow.ValidateValueRefs(path+".app.input", step.App.Input, previousSteps); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	return nil
}

func parseAppCall(value any, path string) (*coreworkflow.AppCall, error) {
	appMap, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(appMap, path, "name", "operation", "connection", "instance", "credentialMode", "input"); err != nil {
		return nil, err
	}
	appName := stringArg(appMap, "name")
	operation := stringArg(appMap, "operation")
	if appName == "" || operation == "" {
		return nil, fmt.Errorf("%w: %s.name and %s.operation are required", ErrInvalid, path, path)
	}
	input, err := parseValue(appMap["input"], path+".input")
	if err != nil {
		return nil, err
	}
	return &coreworkflow.AppCall{
		Name:           appName,
		Operation:      operation,
		Connection:     stringArg(appMap, "connection"),
		Instance:       stringArg(appMap, "instance"),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(stringArg(appMap, "credentialMode"))),
		Input:          input,
	}, nil
}

func parseAgentTurn(value any, path string) (*coreworkflow.AgentTurn, error) {
	agentMap, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(agentMap, path, "provider", "model", "sessionKey", "prompt", "messages", "tools", "output", "modelOptions"); err != nil {
		return nil, err
	}
	messages, err := parseMessages(agentMap["messages"], path+".messages")
	if err != nil {
		return nil, err
	}
	tools, err := parseToolRefs(agentMap["tools"])
	if err != nil {
		return nil, err
	}
	output, err := parseAgentOutput(agentMap["output"], path+".output")
	if err != nil {
		return nil, err
	}
	modelOptions, err := objectArg(agentMap, "modelOptions", path)
	if err != nil {
		return nil, err
	}
	prompt, err := parseText(agentMap["prompt"], path+".prompt")
	if err != nil {
		return nil, err
	}
	return &coreworkflow.AgentTurn{
		ProviderName: stringArg(agentMap, "provider"),
		Model:        stringArg(agentMap, "model"),
		SessionKey:   stringArg(agentMap, "sessionKey"),
		Prompt:       prompt,
		Messages:     messages,
		ToolRefs:     tools,
		Output:       output,
		ModelOptions: modelOptions,
	}, nil
}

func parseAgentOutput(value any, path string) (coreagent.Output, error) {
	if value == nil {
		return coreagent.Output{}, fmt.Errorf("%w: %s is required", ErrInvalid, path)
	}
	outputMap, ok := asMap(value)
	if !ok {
		return coreagent.Output{}, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(outputMap, path, "text", "structured"); err != nil {
		return coreagent.Output{}, err
	}
	_, hasText := outputMap["text"]
	_, hasStructured := outputMap["structured"]
	if hasText == hasStructured {
		return coreagent.Output{}, fmt.Errorf("%w: exactly one of %s.text or %s.structured is required", ErrInvalid, path, path)
	}
	if hasText {
		textMap, ok := asMap(outputMap["text"])
		if !ok {
			return coreagent.Output{}, fmt.Errorf("%w: %s.text must be an object", ErrInvalid, path)
		}
		if err := rejectUnknownKeys(textMap, path+".text"); err != nil {
			return coreagent.Output{}, err
		}
		return coreagent.Output{Text: &coreagent.TextOutput{}}, nil
	}
	structuredMap, ok := asMap(outputMap["structured"])
	if !ok {
		return coreagent.Output{}, fmt.Errorf("%w: %s.structured must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(structuredMap, path+".structured", "schema"); err != nil {
		return coreagent.Output{}, err
	}
	schema, err := objectArg(structuredMap, "schema", path+".structured")
	if err != nil {
		return coreagent.Output{}, err
	}
	return coreagent.Output{Structured: &coreagent.StructuredOutput{Schema: schema}}, nil
}

func parseStepWhen(value any, path string) (*coreworkflow.StepWhen, error) {
	if value == nil {
		return nil, nil
	}
	whenMap, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(whenMap, path, "value", "equals"); err != nil {
		return nil, err
	}
	equals, ok := whenMap["equals"]
	if !ok {
		return nil, fmt.Errorf("%w: %s.equals is required", ErrInvalid, path)
	}
	if !coreworkflow.IsScalarJSON(equals) {
		return nil, fmt.Errorf("%w: %s.equals must be a scalar JSON value", ErrInvalid, path)
	}
	whenValueArg, ok := whenMap["value"]
	if !ok {
		return nil, fmt.Errorf("%w: %s.value is required", ErrInvalid, path)
	}
	whenValue, err := parseValue(whenValueArg, path+".value")
	if err != nil {
		return nil, err
	}
	return &coreworkflow.StepWhen{
		Value:     whenValue,
		Equals:    equals,
		EqualsSet: true,
	}, nil
}

func parseMessages(value any, path string) ([]coreworkflow.AgentMessage, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array", ErrInvalid, path)
	}
	out := make([]coreworkflow.AgentMessage, 0, len(items))
	for i, item := range items {
		messageMap, ok := asMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be an object", ErrInvalid, path, i)
		}
		messagePath := fmt.Sprintf("%s[%d]", path, i)
		if err := rejectUnknownKeys(messageMap, messagePath, "role", "text", "metadata"); err != nil {
			return nil, err
		}
		metadata, err := objectArg(messageMap, "metadata", messagePath)
		if err != nil {
			return nil, err
		}
		text, err := parseText(messageMap["text"], messagePath+".text")
		if err != nil {
			return nil, err
		}
		out = append(out, coreworkflow.AgentMessage{
			Role:     stringArg(messageMap, "role"),
			Text:     text,
			Metadata: metadata,
		})
	}
	return out, nil
}

func parseToolRefs(value any) ([]coreagent.ToolRef, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: agent tools must be an array", ErrInvalid)
	}
	out := make([]coreagent.ToolRef, 0, len(items))
	for i, item := range items {
		refMap, ok := asMap(item)
		if !ok {
			return nil, fmt.Errorf("%w: agent tools[%d] must be an object", ErrInvalid, i)
		}
		path := fmt.Sprintf("agent tools[%d]", i)
		if err := rejectUnknownKeys(refMap, path, "system", "app", "operation", "connection", "instance", "title", "description"); err != nil {
			return nil, err
		}
		out = append(out, coreagent.ToolRef{
			System:      stringArg(refMap, "system"),
			App:         stringArg(refMap, "app"),
			Operation:   stringArg(refMap, "operation"),
			Connection:  stringArg(refMap, "connection"),
			Instance:    stringArg(refMap, "instance"),
			Title:       stringArg(refMap, "title"),
			Description: stringArg(refMap, "description"),
		})
	}
	return out, nil
}

// EncodeTargetMap converts a core workflow target into canonical JSON shape.
func EncodeTargetMap(target coreworkflow.Target) map[string]any {
	steps := make([]map[string]any, 0, len(target.Steps))
	for i := range target.Steps {
		steps = append(steps, encodeStep(target.Steps[i]))
	}
	return map[string]any{"steps": steps}
}

func encodeStep(step coreworkflow.Step) map[string]any {
	stepInfo := map[string]any{
		"id":             step.ID,
		"inputs":         encodeValueMap(step.Inputs),
		"timeoutSeconds": step.TimeoutSeconds,
		"metadata":       mapDeepClone(step.Metadata),
	}
	if step.App != nil {
		stepInfo["app"] = encodeAppCall(*step.App)
	}
	if step.Agent != nil {
		stepInfo["agent"] = encodeAgentTurn(*step.Agent)
	}
	if step.When != nil {
		stepInfo["when"] = encodeStepWhen(*step.When)
	}
	return stepInfo
}

func encodeAppCall(app coreworkflow.AppCall) map[string]any {
	return map[string]any{
		"name":           app.Name,
		"operation":      app.Operation,
		"connection":     app.Connection,
		"instance":       app.Instance,
		"credentialMode": string(app.CredentialMode),
		"input":          EncodeValue(app.Input),
	}
}

func encodeAgentTurn(agent coreworkflow.AgentTurn) map[string]any {
	value := map[string]any{
		"provider":     agent.ProviderName,
		"model":        agent.Model,
		"sessionKey":   agent.SessionKey,
		"prompt":       encodeText(agent.Prompt),
		"tools":        encodeToolRefs(agent.ToolRefs),
		"modelOptions": mapDeepClone(agent.ModelOptions),
	}
	if len(agent.Messages) > 0 {
		messages := make([]map[string]any, 0, len(agent.Messages))
		for _, message := range agent.Messages {
			messages = append(messages, map[string]any{
				"role":     message.Role,
				"text":     encodeText(message.Text),
				"metadata": mapDeepClone(message.Metadata),
			})
		}
		value["messages"] = messages
	}
	if agent.Output.Text != nil {
		value["output"] = map[string]any{"text": map[string]any{}}
	}
	if agent.Output.Structured != nil {
		value["output"] = map[string]any{
			"structured": map[string]any{
				"schema": mapDeepClone(agent.Output.Structured.Schema),
			},
		}
	}
	return value
}

func encodeStepWhen(when coreworkflow.StepWhen) map[string]any {
	return map[string]any{
		"value":  EncodeValue(when.Value),
		"equals": when.Equals,
	}
}

func encodeToolRefs(refs []coreagent.ToolRef) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		value := map[string]any{}
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			value["system"] = systemName
		}
		if appName := strings.TrimSpace(ref.App); appName != "" {
			value["app"] = appName
		}
		if operation := strings.TrimSpace(ref.Operation); operation != "" {
			value["operation"] = operation
		}
		if connection := strings.TrimSpace(ref.Connection); connection != "" {
			value["connection"] = connection
		}
		if instance := strings.TrimSpace(ref.Instance); instance != "" {
			value["instance"] = instance
		}
		if len(value) > 0 {
			out = append(out, value)
		}
	}
	return out
}
