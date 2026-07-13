package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type desiredWorkflowConfigDefinition struct {
	DefinitionKey string
	ProviderName  string
	DefinitionID  string
	definition    config.WorkflowDefinitionConfig
}

type workflowConfigProviderFilter func(providerName string) bool

func reconcileWorkflowConfigDefinitions(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, includeProvider workflowConfigProviderFilter) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigDefinitions(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, desiredEntry.ProviderName) {
			continue
		}
		definition := desiredEntry.definition
		target := workflowConfigTarget(definition.Steps)
		appName := workflowConfigTargetLabel(target)
		_, provider, err := runtime.ResolveProvider(ctx, definition.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existingProto, err := provider.GetDefinition(providerCtx, &proto.GetWorkflowProviderDefinitionRequest{ProviderName: desiredEntry.ProviderName, DefinitionId: desiredEntry.DefinitionID})
		switch {
		case err == nil:
			existing, existingErr := workflowwire.DefinitionFromProto(existingProto)
			if existingErr != nil {
				return fmt.Errorf("bootstrap: decode workflow definition %q for app %q: %w", desiredEntry.DefinitionID, appName, existingErr)
			}
			if !isWorkflowConfigOwnedDefinition(existing, desiredEntry.DefinitionID) {
				return fmt.Errorf("bootstrap: workflow definition %q for app %q conflicts with existing unmanaged definition id %q", desiredEntry.DefinitionKey, appName, desiredEntry.DefinitionID)
			}
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow definition %q for app %q: %w", desiredEntry.DefinitionID, appName, err)
		}
		runAs := definition.RunAs.SubjectRef()
		if err := workflowConfigValidateExecutionTarget(cfg, target, runAs); err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		spec := coreworkflow.DefinitionSpec{
			ID:          desiredEntry.DefinitionID,
			Target:      target,
			Activations: workflowConfigActivations(definition.On),
			Paused:      definition.Paused,
			RunAs:       runAs,
		}
		specProto, err := workflowwire.DefinitionSpecToProto(&spec)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		if _, err := provider.ApplyDefinition(providerCtx, &proto.ApplyWorkflowProviderDefinitionRequest{
			ProviderName: desiredEntry.ProviderName,
			Spec:         specProto,
			Context: &proto.RequestContext{
				Subject: &proto.SubjectContext{
					Id: workflowConfigOwnerSubjectID(),
				},
			},
		}); err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
	}

	if err := cleanupRemovedWorkflowConfigDefinitions(ctx, runtime, desired, includeProvider); err != nil {
		return err
	}
	return nil
}

func workflowConfigProviderIncluded(includeProvider workflowConfigProviderFilter, providerName string) bool {
	if includeProvider == nil {
		return true
	}
	return includeProvider(strings.TrimSpace(providerName))
}

func desiredWorkflowConfigDefinitions(cfg *config.Config) (map[string]desiredWorkflowConfigDefinition, error) {
	desired := make(map[string]desiredWorkflowConfigDefinition)
	if cfg == nil {
		return desired, nil
	}
	for _, definitionKey := range slices.Sorted(maps.Keys(cfg.Workflows.Definitions)) {
		definition := cfg.Workflows.Definitions[definitionKey]
		providerName, _, err := cfg.EffectiveWorkflowProvider(definition.Provider)
		if err != nil {
			return nil, err
		}
		rowID := strings.TrimSpace(definitionKey)
		desired[rowID] = desiredWorkflowConfigDefinition{
			DefinitionKey: definitionKey,
			ProviderName:  providerName,
			DefinitionID:  workflowConfigDefinitionID(definitionKey),
			definition:    definition,
		}
	}
	return desired, nil
}

func workflowConfigActivations(values map[string]config.WorkflowActivationConfig) []coreworkflow.Activation {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreworkflow.Activation, 0, len(values))
	for _, activationID := range slices.Sorted(maps.Keys(values)) {
		value := values[activationID]
		activation := coreworkflow.Activation{
			ID:     strings.TrimSpace(activationID),
			Input:  config.WorkflowValueToCore(value.Input),
			Paused: value.Paused,
		}
		if value.Schedule != nil {
			activation.Schedule = &coreworkflow.ScheduleActivation{
				Cron:     strings.TrimSpace(value.Schedule.Cron),
				Timezone: strings.TrimSpace(value.Schedule.Timezone),
			}
		}
		if value.Event != nil {
			activation.Event = &coreworkflow.EventActivation{
				Match: coreworkflow.EventMatch{
					Type:    strings.TrimSpace(value.Event.Type),
					Source:  strings.TrimSpace(value.Event.Source),
					Subject: strings.TrimSpace(value.Event.Subject),
				},
			}
		}
		out = append(out, activation)
	}
	return out
}

func isWorkflowObjectNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func cleanupRemovedWorkflowConfigDefinitions(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigDefinition, includeProvider workflowConfigProviderFilter) error {
	desiredByProviderDefinition := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, entry.ProviderName) {
			continue
		}
		desiredByProviderDefinition[workflowConfigProviderObjectKey(entry.ProviderName, entry.DefinitionID)] = struct{}{}
	}
	for _, providerName := range runtime.ConfiguredProviderNames() {
		if !workflowConfigProviderIncluded(includeProvider, providerName) {
			continue
		}
		_, provider, err := runtime.ResolveProvider(ctx, providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow definitions requires provider %q: %w", providerName, err)
		}
		resp, err := provider.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{ProviderName: providerName})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "definitions", providerName, err)
			continue
		}
		for _, definitionProto := range resp.GetDefinitions() {
			definition, err := workflowwire.DefinitionFromProto(definitionProto)
			if err != nil {
				return fmt.Errorf("bootstrap: decode workflow definition from provider %q: %w", providerName, err)
			}
			if definition == nil || !isWorkflowConfigOwnedDefinition(definition, definition.ID) {
				continue
			}
			if _, ok := desiredByProviderDefinition[workflowConfigProviderObjectKey(providerName, definition.ID)]; ok {
				continue
			}
			if err := provider.DeleteDefinition(ctx, &proto.DeleteWorkflowProviderDefinitionRequest{ProviderName: providerName, DefinitionId: definition.ID}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow definition %q: %w", definition.ID, err)
			}
		}
	}
	return nil
}

func workflowConfigProviderObjectKey(providerName, objectID string) string {
	return strings.TrimSpace(providerName) + "\x00" + strings.TrimSpace(objectID)
}

func workflowLogSkippedConfigWorkflowCleanup(ctx context.Context, objectType, providerName string, err error) {
	slog.WarnContext(ctx, "skipping config-managed workflow cleanup after provider list failed",
		"object_type", objectType,
		"provider", providerName,
		"error", err,
	)
}

func isWorkflowConfigOwnedDefinition(existing *coreworkflow.Definition, definitionID string) bool {
	if existing == nil {
		return false
	}
	return existing.ID == definitionID &&
		strings.HasPrefix(existing.ID, "cfg_")
}

func workflowConfigTargetLabel(target coreworkflow.Target) string {
	for i := range target.Steps {
		step := target.Steps[i]
		if step.App != nil {
			return strings.TrimSpace(step.App.Name)
		}
		if step.Agent != nil {
			providerName := strings.TrimSpace(step.Agent.ProviderName)
			if providerName == "" {
				providerName = "default"
			}
			return "agent:" + providerName
		}
	}
	return ""
}

func workflowConfigTarget(steps []config.WorkflowStepConfig) coreworkflow.Target {
	return config.WorkflowStepsToCore(steps)
}

func workflowConfigDefinitionID(definitionKey string) string {
	return "cfg_" + strings.TrimSpace(definitionKey)
}

func workflowConfigValidateExecutionTarget(cfg *config.Config, target coreworkflow.Target, runAs *core.RunAsSubject) error {
	runAs = core.NormalizeRunAsSubject(runAs)
	hasRunAs := runAs != nil
	for i := range target.Steps {
		step := target.Steps[i]
		if step.App != nil {
			if err := workflowConfigValidateNoUserCredentialTarget(cfg, *step.App, hasRunAs); err != nil {
				return err
			}
		}
		if step.Agent != nil {
			for j := range step.Agent.ToolRefs {
				tool := step.Agent.ToolRefs[j]
				if strings.TrimSpace(tool.System) != "" {
					continue
				}
				if err := workflowConfigValidateNoUserCredentialTarget(cfg, coreworkflow.AppCall{
					Name:           strings.TrimSpace(tool.App),
					Operation:      strings.TrimSpace(tool.Operation),
					Connection:     strings.TrimSpace(tool.Connection),
					Instance:       strings.TrimSpace(tool.Instance),
					CredentialMode: core.NormalizeOptionalConnectionMode(tool.CredentialMode),
				}, hasRunAs); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func workflowConfigValidateNoUserCredentialTarget(cfg *config.Config, target coreworkflow.AppCall, hasRunAs bool) error {
	modeOverride := core.NormalizeOptionalConnectionMode(target.CredentialMode)
	switch modeOverride {
	case "":
	case core.ConnectionModeNone:
		return nil
	case core.ConnectionModeSubject:
		if hasRunAs {
			return nil
		}
		return fmt.Errorf("config-managed workflows do not support user-credentialed app %q", strings.TrimSpace(target.Name))
	default:
		return fmt.Errorf("unsupported credential mode %q for config-managed workflow target %q", modeOverride, strings.TrimSpace(target.Name))
	}
	mode, err := workflowConfigTargetConnectionMode(cfg, target)
	if err != nil {
		return err
	}
	appName := strings.TrimSpace(target.Name)
	switch mode {
	case core.ConnectionModeNone:
		return nil
	case core.ConnectionModeSubject:
		if hasRunAs {
			return nil
		}
		return fmt.Errorf("config-managed workflows do not support user-credentialed app %q", appName)
	default:
		return fmt.Errorf("unsupported connection mode %q for config-managed workflow target %q", mode, appName)
	}
}

func workflowConfigTargetConnectionMode(cfg *config.Config, target coreworkflow.AppCall) (core.ConnectionMode, error) {
	if cfg == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow config is not available")
	}
	appName := strings.TrimSpace(target.Name)
	entry := cfg.Apps[appName]
	if entry == nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow target app %q is not configured", appName)
	}
	plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return core.ConnectionModeNone, fmt.Errorf("workflow target app %q connection plan: %w", appName, err)
	}

	if connection := strings.TrimSpace(target.Connection); connection != "" {
		return workflowConfigConnectionModeForName(plan, appName, connection)
	}
	if operation := strings.TrimSpace(target.Operation); operation != "" {
		mode, ok, err := workflowConfigOperationConnectionMode(plan, entry.ManifestSpec(), target)
		if err != nil {
			return core.ConnectionModeNone, fmt.Errorf("workflow target app %q operation %q connection plan: %w", appName, operation, err)
		}
		if ok {
			return mode, nil
		}
	}
	return core.NormalizeConnectionMode(core.ConnectionMode(entry.ConnectionMode)), nil
}

func workflowConfigOperationConnectionMode(plan config.StaticConnectionPlan, manifestApp *providermanifestv1.Spec, target coreworkflow.AppCall) (core.ConnectionMode, bool, error) {
	connections, selectors, _, err := plan.RESTOperationConnectionBindings(manifestApp)
	if err != nil {
		return core.ConnectionModeNone, false, err
	}
	operation := strings.TrimSpace(target.Operation)
	if selector, ok := selectors[operation]; ok {
		connectionName, resolved := workflowConfigConnectionSelectorTargetConnection(selector, workflowConfigValueObjectMap(target.Input))
		if resolved {
			mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
			return mode, true, err
		}
		if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
			mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
			return mode, true, err
		}
		mode, err := workflowConfigConnectionSelectorMode(plan, target.Name, selector)
		return mode, true, err
	}
	if connectionName := strings.TrimSpace(connections[operation]); connectionName != "" {
		mode, err := workflowConfigConnectionModeForName(plan, target.Name, connectionName)
		return mode, true, err
	}
	return core.ConnectionModeNone, false, nil
}

func workflowConfigValueObjectMap(value coreworkflow.Value) map[string]any {
	if len(value.Object) == 0 {
		return nil
	}
	out := make(map[string]any, len(value.Object))
	for key := range value.Object {
		nested := value.Object[key]
		if nested.LiteralSet {
			out[key] = nested.Literal
		}
	}
	return out
}

func workflowConfigConnectionSelectorTargetConnection(selector core.OperationConnectionSelector, input map[string]any) (string, bool) {
	parameter := strings.TrimSpace(selector.Parameter)
	if parameter == "" || len(input) == 0 {
		return "", false
	}
	value, ok := input[parameter]
	if !ok {
		return "", false
	}
	connection, ok := value.(string)
	if !ok {
		return "", false
	}
	connectionName := selector.Values[strings.TrimSpace(connection)]
	return strings.TrimSpace(connectionName), strings.TrimSpace(connectionName) != ""
}

func workflowConfigConnectionSelectorMode(plan config.StaticConnectionPlan, appName string, selector core.OperationConnectionSelector) (core.ConnectionMode, error) {
	for _, connectionName := range selector.Values {
		mode, err := workflowConfigConnectionModeForName(plan, appName, connectionName)
		if err != nil {
			return core.ConnectionModeNone, err
		}
		if mode == core.ConnectionModeSubject {
			return core.ConnectionModeSubject, nil
		}
	}
	return core.ConnectionModeNone, nil
}

func workflowConfigConnectionModeForName(plan config.StaticConnectionPlan, appName, connectionName string) (core.ConnectionMode, error) {
	conn, ok := plan.LookupConnection(connectionName)
	if !ok {
		return core.ConnectionModeNone, fmt.Errorf("workflow target app %q connection %q is not configured", strings.TrimSpace(appName), strings.TrimSpace(connectionName))
	}
	return config.ConnectionModeForConnection(conn), nil
}

func workflowConfigOwnerSubjectID() string {
	return "system:config"
}
