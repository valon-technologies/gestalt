package config

import (
	"maps"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

// WorkflowValueToCore converts config YAML value form into core workflow value form.
func WorkflowValueToCore(value WorkflowValueConfig) coreworkflow.Value {
	out := coreworkflow.Value{
		Literal:    value.Literal,
		LiteralSet: value.LiteralSet,
		Object:     workflowValueMapToCore(value.Object),
		Array:      workflowValueArrayToCore(value.Array),
		Input:      strings.TrimSpace(value.Input),
		Signal:     strings.TrimSpace(value.Signal),
	}
	if value.Template != nil {
		out.Template = &coreworkflow.Text{Template: strings.TrimSpace(value.Template.Template)}
	}
	if value.StepOutput != nil {
		out.StepOutput = &coreworkflow.StepOutputSource{
			StepID: strings.TrimSpace(value.StepOutput.StepID),
			Path:   strings.TrimSpace(value.StepOutput.Path),
		}
	}
	if value.StepInput != nil {
		out.StepInput = &coreworkflow.StepInputSource{
			StepID: strings.TrimSpace(value.StepInput.StepID),
			Path:   strings.TrimSpace(value.StepInput.Path),
		}
	}
	return out
}

func workflowValueMapToCore(values map[string]WorkflowValueConfig) map[string]coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = WorkflowValueToCore(values[key])
	}
	return out
}

func workflowValueArrayToCore(values []WorkflowValueConfig) []coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make([]coreworkflow.Value, 0, len(values))
	for i := range values {
		out = append(out, WorkflowValueToCore(values[i]))
	}
	return out
}

// WorkflowStepsToCore converts config YAML steps into the core workflow target form.
func WorkflowStepsToCore(steps []WorkflowStepConfig) coreworkflow.Target {
	return coreworkflow.Target{Steps: workflowStepsToCore(steps)}
}

// WorkflowTargetToCore converts config target form into core workflow target form.
func WorkflowTargetToCore(target *WorkflowTargetConfig) coreworkflow.Target {
	if target == nil {
		return coreworkflow.Target{}
	}
	return WorkflowStepsToCore(target.Steps)
}

func workflowStepsToCore(steps []WorkflowStepConfig) []coreworkflow.Step {
	if len(steps) == 0 {
		return nil
	}
	out := make([]coreworkflow.Step, 0, len(steps))
	for i := range steps {
		step := &steps[i]
		timeoutSeconds := 0
		if timeout := strings.TrimSpace(step.Timeout); timeout != "" {
			if parsed, err := time.ParseDuration(timeout); err == nil {
				timeoutSeconds = int(parsed.Seconds())
			}
		}
		out = append(out, coreworkflow.Step{
			ID:             strings.TrimSpace(step.ID),
			Inputs:         workflowValueMapToCore(step.Inputs),
			App:            workflowAppCallToCore(step.App),
			Agent:          workflowAgentTurnToCore(step.Agent),
			Metadata:       maps.Clone(step.Metadata),
			TimeoutSeconds: timeoutSeconds,
			When:           WorkflowStepWhenToCore(step.When),
		})
	}
	return out
}

func workflowAppCallToCore(app *WorkflowStepAppCallConfig) *coreworkflow.AppCall {
	if app == nil {
		return nil
	}
	return &coreworkflow.AppCall{
		Name:           strings.TrimSpace(app.Name),
		Operation:      strings.TrimSpace(app.Operation),
		Connection:     strings.TrimSpace(app.Connection),
		Instance:       strings.TrimSpace(app.Instance),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(app.CredentialMode)),
		Input:          WorkflowValueToCore(app.Input),
	}
}

func workflowAgentTurnToCore(agent *WorkflowStepAgentConfig) *coreworkflow.AgentTurn {
	if agent == nil {
		return nil
	}
	messages := make([]coreworkflow.AgentMessage, 0, len(agent.Messages))
	for _, message := range agent.Messages {
		messages = append(messages, coreworkflow.AgentMessage{
			Role:     strings.TrimSpace(message.Role),
			Text:     coreworkflow.Text{Template: strings.TrimSpace(message.Text.Template)},
			Metadata: maps.Clone(message.Metadata),
		})
	}
	tools := make([]coreagent.ToolRef, 0, len(agent.Tools))
	for i := range agent.Tools {
		tool := &agent.Tools[i]
		tools = append(tools, coreagent.ToolRef{
			System:         strings.TrimSpace(tool.System),
			App:            strings.TrimSpace(tool.App),
			Operation:      strings.TrimSpace(tool.Operation),
			Connection:     strings.TrimSpace(tool.Connection),
			Instance:       strings.TrimSpace(tool.Instance),
			CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(tool.CredentialMode)),
			Title:          strings.TrimSpace(tool.Title),
			Description:    strings.TrimSpace(tool.Description),
		})
	}
	return &coreworkflow.AgentTurn{
		ProviderName: strings.TrimSpace(agent.Provider),
		Model:        strings.TrimSpace(agent.Model),
		SessionKey:   strings.TrimSpace(agent.SessionKey),
		Prompt:       coreworkflow.Text{Template: strings.TrimSpace(agent.Prompt.Template)},
		Messages:     messages,
		ToolRefs:     tools,
		Output:       WorkflowAgentOutputToCore(agent.Output),
		ModelOptions: maps.Clone(agent.ModelOptions),
		Workspace:    workflowAgentWorkspaceToCore(agent.Workspace),
	}
}

func workflowAgentWorkspaceToCore(workspace *WorkflowStepAgentWorkspaceConfig) *coreagent.Workspace {
	if workspace == nil {
		return nil
	}
	ws := &coreagent.Workspace{CWD: workspace.CWD}
	for i := range workspace.Checkouts {
		checkout := &workspace.Checkouts[i]
		ws.Checkouts = append(ws.Checkouts, coreagent.WorkspaceGitCheckout{
			URL:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return coreagent.CloneWorkspace(ws)
}

func workflowAgentWorkspaceFromCore(workspace *coreagent.Workspace) *WorkflowStepAgentWorkspaceConfig {
	if workspace == nil {
		return nil
	}
	checkouts := make([]WorkflowStepAgentWorkspaceCheckoutConfig, 0, len(workspace.Checkouts))
	for i := range workspace.Checkouts {
		checkout := workspace.Checkouts[i]
		checkouts = append(checkouts, WorkflowStepAgentWorkspaceCheckoutConfig{
			URL:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return &WorkflowStepAgentWorkspaceConfig{
		Checkouts: checkouts,
		CWD:       workspace.CWD,
	}
}

func WorkflowAgentOutputToCore(output *WorkflowAgentOutputConfig) coreagent.Output {
	if output == nil {
		return coreagent.Output{}
	}
	if output.Structured != nil {
		return coreagent.Output{
			Structured: &coreagent.StructuredOutput{
				Schema: maps.Clone(output.Structured.Schema),
			},
		}
	}
	if output.Text != nil {
		return coreagent.Output{Text: &coreagent.TextOutput{}}
	}
	return coreagent.Output{}
}

// WorkflowStepWhenToCore converts config YAML step-when form into core workflow form.
func WorkflowStepWhenToCore(when *WorkflowStepWhenConfig) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	return &coreworkflow.StepWhen{
		Value:     WorkflowValueToCore(when.Value),
		Equals:    when.Equals,
		EqualsSet: when.EqualsSet(),
	}
}
