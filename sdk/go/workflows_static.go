package gestalt

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const envWriteWorkflows = "GESTALT_APP_WRITE_WORKFLOWS"

type staticWorkflowDefinitions struct {
	Definitions []staticWorkflowDefinition `yaml:"definitions,omitempty"`
}

type staticWorkflowDefinition struct {
	ID    string                 `yaml:"id,omitempty"`
	Steps []staticWorkflowAppCall `yaml:"steps,omitempty"`
}

type staticWorkflowAppCall struct {
	App       string `yaml:"app"`
	Operation string `yaml:"operation,omitempty"`
}

func writeDeclaredWorkflowsYAML(provider any, path string) error {
	decl, ok := provider.(interface {
		DeclaredWorkflowDefinitions() ([]WorkflowDefinitionSpec, error)
	})
	if !ok {
		return nil
	}
	specs, err := decl.DeclaredWorkflowDefinitions()
	if err != nil {
		return fmt.Errorf("declare workflow definitions: %w", err)
	}
	workflows := staticWorkflowDefinitionsFromSpecs(specs)
	if len(workflows.Definitions) == 0 {
		return nil
	}
	if err := ensureOutputDir("workflows", path); err != nil {
		return err
	}
	data, err := yaml.Marshal(workflows)
	if err != nil {
		return fmt.Errorf("encode workflows yaml: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write workflows yaml %q: %w", path, err)
	}
	return nil
}

func staticWorkflowDefinitionsFromSpecs(specs []WorkflowDefinitionSpec) staticWorkflowDefinitions {
	if len(specs) == 0 {
		return staticWorkflowDefinitions{}
	}
	out := staticWorkflowDefinitions{Definitions: make([]staticWorkflowDefinition, 0, len(specs))}
	for _, spec := range specs {
		steps := staticWorkflowAppCallsFromTarget(spec.Target)
		if len(steps) == 0 {
			continue
		}
		out.Definitions = append(out.Definitions, staticWorkflowDefinition{
			ID:    strings.TrimSpace(spec.ID),
			Steps: steps,
		})
	}
	return out
}

func staticWorkflowAppCallsFromTarget(target *BoundWorkflowTarget) []staticWorkflowAppCall {
	if target == nil || len(target.Steps) == 0 {
		return nil
	}
	out := make([]staticWorkflowAppCall, 0, len(target.Steps))
	for _, step := range target.Steps {
		if step.App == nil {
			continue
		}
		appName := strings.TrimSpace(step.App.Name)
		if appName == "" {
			continue
		}
		out = append(out, staticWorkflowAppCall{
			App:       appName,
			Operation: strings.TrimSpace(step.App.Operation),
		})
	}
	return out
}
