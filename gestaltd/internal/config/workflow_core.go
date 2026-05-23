package config

import (
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func workflowValueConfigToCore(value WorkflowValueConfig) coreworkflow.Value {
	out := coreworkflow.Value{
		Literal:       value.Literal,
		LiteralSet:    value.LiteralSet,
		Object:        workflowValueConfigMapToCore(value.Object),
		Array:         workflowValueConfigArrayToCore(value.Array),
		RunInput:      strings.TrimSpace(value.RunInput),
		SignalPayload: strings.TrimSpace(value.SignalPayload),
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
	return out
}

func workflowValueConfigMapToCore(values map[string]WorkflowValueConfig) map[string]coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = workflowValueConfigToCore(values[key])
	}
	return out
}

func workflowValueConfigArrayToCore(values []WorkflowValueConfig) []coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make([]coreworkflow.Value, 0, len(values))
	for i := range values {
		out = append(out, workflowValueConfigToCore(values[i]))
	}
	return out
}
