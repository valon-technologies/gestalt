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
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type desiredWorkflowConfigDefinition struct {
	DefinitionKey string
	ProviderName  string
	DefinitionID  string
	FromApp       string
	Spec          coreworkflow.DefinitionSpec
}

type workflowConfigProviderFilter func(providerName string) bool

func reconcileWorkflowConfigDefinitions(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, appDecls *appWorkflowDeclarations, includeProvider workflowConfigProviderFilter) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	ctx = principal.WithPrincipal(ctx, &principal.Principal{SubjectID: workflowConfigOwnerSubjectID()})
	cfgDesired, err := desiredWorkflowConfigDefinitions(cfg)
	if err != nil {
		return err
	}
	var reported map[string][]*proto.WorkflowDefinitionSpec
	if appDecls != nil {
		reported = appDecls.Snapshot()
	}
	appDesired, err := desiredAppWorkflowDefinitions(cfg, reported)
	if err != nil {
		return err
	}
	desired, err := mergeDesiredWorkflowDefinitions(cfgDesired, appDesired)
	if err != nil {
		return err
	}

	for _, definitionID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[definitionID]
		if !workflowConfigProviderIncluded(includeProvider, desiredEntry.ProviderName) {
			continue
		}
		spec := desiredEntry.Spec
		target := spec.Target
		appName := workflowDefinitionTargetLabel(desiredEntry, target)
		_, provider, err := runtime.ResolveProvider(ctx, desiredEntry.ProviderName)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existingProto, err := provider.GetDefinition(providerCtx, &proto.GetWorkflowProviderDefinitionRequest{Provider: desiredEntry.ProviderName, DefinitionId: desiredEntry.DefinitionID})
		switch {
		case err == nil:
			existing, existingErr := workflowwire.DefinitionFromProto(existingProto)
			if existingErr != nil {
				return fmt.Errorf("bootstrap: decode workflow definition %q for app %q: %w", desiredEntry.DefinitionID, appName, existingErr)
			}
			if !isManagedWorkflowDefinitionOwned(existing, desiredEntry.DefinitionID) {
				return fmt.Errorf("bootstrap: workflow definition %q for app %q conflicts with existing unmanaged definition id %q", desiredEntry.DefinitionKey, appName, desiredEntry.DefinitionID)
			}
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow definition %q for app %q: %w", desiredEntry.DefinitionID, appName, err)
		}
		runAs := strings.TrimSpace(spec.RunAs)
		if err := workflowConfigValidateExecutionTarget(cfg, target, runAs); err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		specProto, err := workflowwire.DefinitionSpecToProto(&spec)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
		if _, err := provider.ApplyDefinition(providerCtx, &proto.ApplyWorkflowProviderDefinitionRequest{
			Provider: desiredEntry.ProviderName,
			Spec:     specProto,
			Context: &proto.RequestContext{
				Subject: &proto.SubjectContext{
					Id: workflowConfigOwnerSubjectID(),
				},
			},
		}); err != nil {
			return fmt.Errorf("bootstrap: workflow definition %q for app %q: %w", desiredEntry.DefinitionKey, appName, err)
		}
	}

	if err := cleanupRemovedWorkflowConfigDefinitions(ctx, cfg, runtime, reported, desired, includeProvider); err != nil {
		return err
	}
	return nil
}

func workflowDefinitionTargetLabel(entry desiredWorkflowConfigDefinition, target coreworkflow.Target) string {
	if appName := strings.TrimSpace(entry.FromApp); appName != "" {
		return appName
	}
	return workflowConfigTargetLabel(target)
}

func isManagedWorkflowDefinitionOwned(existing *coreworkflow.Definition, definitionID string) bool {
	return isWorkflowConfigOwnedDefinition(existing, definitionID) || isAppWorkflowOwnedDefinition(existing, definitionID)
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
		definitionID := workflowConfigDefinitionID(definitionKey)
		target := workflowConfigTarget(definition.Steps)
		runAs := strings.TrimSpace(definition.RunAs)
		desired[definitionID] = desiredWorkflowConfigDefinition{
			DefinitionKey: definitionKey,
			ProviderName:  providerName,
			DefinitionID:  definitionID,
			Spec: coreworkflow.DefinitionSpec{
				ID:          definitionID,
				Target:      target,
				Activations: workflowConfigActivations(definition.On),
				Paused:      definition.Paused,
				RunAs:       runAs,
			},
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

func cleanupRemovedWorkflowConfigDefinitions(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, reported map[string][]*proto.WorkflowDefinitionSpec, desired map[string]desiredWorkflowConfigDefinition, includeProvider workflowConfigProviderFilter) error {
	desiredByProviderDefinition := make(map[string]struct{}, len(desired))
	for definitionID := range desired {
		entry := desired[definitionID]
		if !workflowConfigProviderIncluded(includeProvider, entry.ProviderName) {
			continue
		}
		desiredByProviderDefinition[workflowConfigProviderObjectKey(entry.ProviderName, entry.DefinitionID)] = struct{}{}
	}
	protectedPrefixes := appWorkflowProtectedPrefixes(cfg, reported)
	for _, providerName := range runtime.ConfiguredProviderNames() {
		if !workflowConfigProviderIncluded(includeProvider, providerName) {
			continue
		}
		_, provider, err := runtime.ResolveProvider(ctx, providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow definitions requires provider %q: %w", providerName, err)
		}
		resp, err := provider.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{Provider: providerName})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "definitions", providerName, err)
			continue
		}
		for _, definitionProto := range resp.GetDefinitions() {
			definition, err := workflowwire.DefinitionFromProto(definitionProto)
			if err != nil {
				return fmt.Errorf("bootstrap: decode workflow definition from provider %q: %w", providerName, err)
			}
			if definition == nil || !isManagedWorkflowDefinitionOwned(definition, definition.ID) {
				continue
			}
			if _, ok := desiredByProviderDefinition[workflowConfigProviderObjectKey(providerName, definition.ID)]; ok {
				continue
			}
			if definitionMatchesProtectedPrefix(definition.ID, protectedPrefixes) {
				continue
			}
			if err := provider.DeleteDefinition(ctx, &proto.DeleteWorkflowProviderDefinitionRequest{Provider: providerName, DefinitionId: definition.ID}); err != nil && !isWorkflowObjectNotFound(err) {
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
		strings.HasPrefix(existing.ID, coreworkflow.ConfigManagedDefinitionPrefix)
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
	return coreworkflow.ConfigManagedDefinitionPrefix + strings.TrimSpace(definitionKey)
}

func workflowConfigValidateExecutionTarget(cfg *config.Config, target coreworkflow.Target, runAs string) error {
	hasRunAs := strings.TrimSpace(runAs) != ""
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

func deferredAppWorkflowReconcileTask(deferred *deferredProviders, runtime *workflowRuntime, defaultProvider string, reconcile func(context.Context, workflowConfigProviderFilter) error) workflowConfigReconcileTask {
	return workflowConfigReconcileTask{
		name: "app workflow declarations (deferred apps)",
		reconcile: func(ctx context.Context) error {
			if deferred != nil {
				select {
				case <-deferred.ready():
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err := waitRuntimeWorkflowProviderReady(ctx, runtime, defaultProvider); err != nil {
				return err
			}
			return reconcile(ctx, workflowConfigOnlyProvider(defaultProvider))
		},
	}
}
