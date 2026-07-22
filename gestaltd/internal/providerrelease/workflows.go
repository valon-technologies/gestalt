package providerrelease

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

// WorkflowDefinitions documents app-call steps declared by packaged workflow definitions.
type WorkflowDefinitions struct {
	Definitions []WorkflowDefinition `yaml:"definitions,omitempty"`
}

// WorkflowDefinition is one workflow definition and its app-call steps.
type WorkflowDefinition struct {
	ID    string            `yaml:"id,omitempty"`
	Steps []WorkflowAppCall `yaml:"steps,omitempty"`
}

// WorkflowAppCall names a workflow app-call target.
type WorkflowAppCall struct {
	App       string `yaml:"app"`
	Operation string `yaml:"operation,omitempty"`
}

// WorkflowDefinitionsFromStatic copies packaged workflow metadata into release form.
func WorkflowDefinitionsFromStatic(static *packageio.StaticWorkflowDefinitions) *WorkflowDefinitions {
	if static == nil || len(static.Definitions) == 0 {
		return nil
	}
	out := &WorkflowDefinitions{Definitions: make([]WorkflowDefinition, 0, len(static.Definitions))}
	for _, definition := range static.Definitions {
		if len(definition.Steps) == 0 {
			continue
		}
		steps := make([]WorkflowAppCall, 0, len(definition.Steps))
		for _, step := range definition.Steps {
			appName := strings.TrimSpace(step.App)
			if appName == "" {
				continue
			}
			steps = append(steps, WorkflowAppCall{
				App:       appName,
				Operation: strings.TrimSpace(step.Operation),
			})
		}
		if len(steps) == 0 {
			continue
		}
		out.Definitions = append(out.Definitions, WorkflowDefinition{
			ID:    strings.TrimSpace(definition.ID),
			Steps: steps,
		})
	}
	if len(out.Definitions) == 0 {
		return nil
	}
	return out
}
