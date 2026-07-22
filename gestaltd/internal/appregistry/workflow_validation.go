package appregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// Workflows documents app-call steps declared by packaged workflow definitions.
type Workflows struct {
	Definitions []WorkflowDefinitionRef `json:"definitions,omitempty"`
}

// WorkflowDefinitionRef is one workflow definition and its app-call steps.
type WorkflowDefinitionRef struct {
	ID    string               `json:"id,omitempty"`
	Steps []WorkflowAppCallRef `json:"steps,omitempty"`
}

// WorkflowAppCallRef names a workflow app-call target.
type WorkflowAppCallRef struct {
	App       string `json:"app"`
	Operation string `json:"operation,omitempty"`
}

// WorkflowsFromDefinitionSpecs extracts app-call steps from provider workflow specs.
func WorkflowsFromDefinitionSpecs(specs []*proto.WorkflowDefinitionSpec) (Workflows, error) {
	if len(specs) == 0 {
		return Workflows{}, nil
	}
	out := Workflows{Definitions: make([]WorkflowDefinitionRef, 0, len(specs))}
	for i, specProto := range specs {
		if specProto == nil {
			return Workflows{}, fmt.Errorf("workflow definition[%d] is required", i)
		}
		spec, err := workflowwire.DefinitionSpecFromProto(specProto)
		if err != nil {
			return Workflows{}, fmt.Errorf("workflow definition %q: %w", strings.TrimSpace(specProto.GetId()), err)
		}
		if spec == nil {
			continue
		}
		steps := workflowAppCallRefsFromTarget(spec.Target)
		if len(steps) == 0 {
			continue
		}
		out.Definitions = append(out.Definitions, WorkflowDefinitionRef{
			ID:    strings.TrimSpace(specProto.GetId()),
			Steps: steps,
		})
	}
	return out, nil
}

// WorkflowsFromProviderRelease copies workflow metadata from provider release static validation.
func WorkflowsFromProviderRelease(release *providerrelease.Metadata) Workflows {
	if release == nil || release.StaticValidation == nil || release.StaticValidation.Workflows == nil {
		return Workflows{}
	}
	defs := release.StaticValidation.Workflows.Definitions
	if len(defs) == 0 {
		return Workflows{}
	}
	out := Workflows{Definitions: make([]WorkflowDefinitionRef, 0, len(defs))}
	for _, definition := range defs {
		if len(definition.Steps) == 0 {
			continue
		}
		steps := make([]WorkflowAppCallRef, 0, len(definition.Steps))
		for _, step := range definition.Steps {
			appName := strings.TrimSpace(step.App)
			if appName == "" {
				continue
			}
			steps = append(steps, WorkflowAppCallRef{
				App:       appName,
				Operation: strings.TrimSpace(step.Operation),
			})
		}
		if len(steps) == 0 {
			continue
		}
		out.Definitions = append(out.Definitions, WorkflowDefinitionRef{
			ID:    strings.TrimSpace(definition.ID),
			Steps: steps,
		})
	}
	return out
}

func workflowAppCallRefsFromTarget(target coreworkflow.Target) []WorkflowAppCallRef {
	if len(target.Steps) == 0 {
		return nil
	}
	out := make([]WorkflowAppCallRef, 0, len(target.Steps))
	for _, step := range target.Steps {
		if step.App == nil {
			continue
		}
		appName := strings.TrimSpace(step.App.Name)
		if appName == "" {
			continue
		}
		out = append(out, WorkflowAppCallRef{
			App:       appName,
			Operation: strings.TrimSpace(step.App.Operation),
		})
	}
	return out
}

func validateWorkflowDefinitions(
	ctx context.Context,
	v *InstallValidator,
	workflows Workflows,
	knownByApp map[string]*core.AppInstallation,
) error {
	if len(workflows.Definitions) == 0 {
		return nil
	}
	for _, definition := range workflows.Definitions {
		definitionID := strings.TrimSpace(definition.ID)
		for _, step := range definition.Steps {
			targetApp := strings.TrimSpace(step.App)
			if targetApp == "" {
				continue
			}
			subject := workflowValidationSubject(definitionID, targetApp)
			if !deployConfigAppConfigured(v.ConfigApps, targetApp) {
				return installValidationError(
					InstallValidationWorkflowTargetAppMissing,
					fmt.Sprintf("%s: workflow target app %q is not configured", subject, targetApp),
				)
			}
			operation := strings.TrimSpace(step.Operation)
			if operation == "" {
				continue
			}
			installation := knownByApp[targetApp]
			if installation == nil {
				continue
			}
			targetEntry, err := v.fetchPublishedEntry(ctx, installation.Registry, targetApp, installation.Version)
			if err != nil {
				return validationFetchError(InstallValidationWorkflowTargetOperationMetadataMissing, subject, err)
			}
			if _, ok := targetEntry.Interface.Operations[operation]; !ok {
				return installValidationError(
					InstallValidationWorkflowTargetOperationMissing,
					fmt.Sprintf("%s: operation %s is not published", subject, operation),
				)
			}
		}
	}
	return nil
}

func deployConfigAppConfigured(configApps map[string]*config.ProviderEntry, appName string) bool {
	if configApps == nil {
		return false
	}
	_, ok := configApps[appName]
	return ok
}

func workflowValidationSubject(definitionID, targetApp string) string {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return fmt.Sprintf("workflow target %s", targetApp)
	}
	return fmt.Sprintf("workflow %s", definitionID)
}
