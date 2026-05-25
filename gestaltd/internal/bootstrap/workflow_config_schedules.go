package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type desiredWorkflowConfigSchedule struct {
	ScheduleKey  string
	ProviderName string
	ScheduleID   string
	schedule     config.WorkflowScheduleConfig
}

type workflowConfigProviderFilter func(providerName string) bool

func reconcileWorkflowConfigSchedules(ctx context.Context, cfg *config.Config, runtime *workflowRuntime, tokens *appaccessservice.InvocationTokenManager, includeProvider workflowConfigProviderFilter) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	desired, err := desiredWorkflowConfigSchedules(cfg)
	if err != nil {
		return err
	}

	for _, rowID := range slices.Sorted(maps.Keys(desired)) {
		desiredEntry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, desiredEntry.ProviderName) {
			continue
		}
		schedule := desiredEntry.schedule
		target := workflowConfigTarget(schedule.Target)
		appName := workflowConfigTargetLabel(target)
		_, provider, err := runtime.ResolveProviderSelection(schedule.Provider)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
		providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
		existing, err := provider.GetSchedule(providerCtx, coreworkflow.GetScheduleRequest{
			ScheduleID: desiredEntry.ScheduleID,
		})
		switch {
		case err == nil:
			if !isWorkflowConfigOwnedSchedule(existing, appName, desiredEntry.ScheduleID) {
				return fmt.Errorf("bootstrap: workflow schedule %q for app %q conflicts with existing unmanaged schedule id %q", desiredEntry.ScheduleKey, appName, desiredEntry.ScheduleID)
			}
		case isWorkflowObjectNotFound(err):
		default:
			return fmt.Errorf("bootstrap: get workflow schedule %q for app %q: %w", desiredEntry.ScheduleID, appName, err)
		}
		runAs, err := workflowConfigRunAsSubject("workflows.schedules."+desiredEntry.ScheduleKey+".runAs", schedule.RunAs)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
		permissions, err := workflowConfigExecutionPermissions(cfg, "workflows.schedules."+desiredEntry.ScheduleKey, schedule.Invokes)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
		if err := workflowConfigValidateExecutionTarget(cfg, target, runAs, permissions); err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
		executionPermissions := workflowConfigExecutionPermissionsForTarget(target, permissions)
		providerCtx, err = workflowConfigInvocationContext(providerCtx, tokens, runAs, executionPermissions)
		if err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
		if _, err := provider.UpsertSchedule(providerCtx, coreworkflow.UpsertScheduleRequest{
			ScheduleID:  desiredEntry.ScheduleID,
			Cron:        schedule.Cron,
			Timezone:    schedule.Timezone,
			Target:      target,
			Paused:      schedule.Paused,
			RequestedBy: workflowConfigActor(),
		}); err != nil {
			return fmt.Errorf("bootstrap: workflow schedule %q for app %q: %w", desiredEntry.ScheduleKey, appName, err)
		}
	}

	if err := cleanupRemovedWorkflowConfigSchedules(ctx, runtime, desired, includeProvider); err != nil {
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

func desiredWorkflowConfigSchedules(cfg *config.Config) (map[string]desiredWorkflowConfigSchedule, error) {
	desired := make(map[string]desiredWorkflowConfigSchedule)
	if cfg == nil {
		return desired, nil
	}
	for _, scheduleKey := range slices.Sorted(maps.Keys(cfg.Workflows.Schedules)) {
		schedule := cfg.Workflows.Schedules[scheduleKey]
		providerName, _, err := cfg.EffectiveWorkflowProvider(schedule.Provider)
		if err != nil {
			return nil, err
		}
		rowID := strings.TrimSpace(scheduleKey)
		desired[rowID] = desiredWorkflowConfigSchedule{
			ScheduleKey:  scheduleKey,
			ProviderName: providerName,
			ScheduleID:   workflowConfigScheduleID(scheduleKey),
			schedule:     schedule,
		}
	}
	return desired, nil
}

func isWorkflowObjectNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func cleanupRemovedWorkflowConfigSchedules(ctx context.Context, runtime *workflowRuntime, desired map[string]desiredWorkflowConfigSchedule, includeProvider workflowConfigProviderFilter) error {
	desiredByProviderSchedule := make(map[string]struct{}, len(desired))
	for rowID := range desired {
		entry := desired[rowID]
		if !workflowConfigProviderIncluded(includeProvider, entry.ProviderName) {
			continue
		}
		desiredByProviderSchedule[workflowConfigProviderObjectKey(entry.ProviderName, entry.ScheduleID)] = struct{}{}
	}
	for _, providerName := range runtime.ProviderNames() {
		if !workflowConfigProviderIncluded(includeProvider, providerName) {
			continue
		}
		provider, err := runtime.ResolveProvider(providerName)
		if err != nil {
			return fmt.Errorf("bootstrap: cleanup workflow schedules requires provider %q: %w", providerName, err)
		}
		schedules, err := provider.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{})
		if err != nil {
			workflowLogSkippedConfigWorkflowCleanup(ctx, "schedules", providerName, err)
			continue
		}
		for _, schedule := range schedules {
			if schedule == nil || !isWorkflowConfigOwnedSchedule(schedule, workflowConfigTargetLabel(schedule.Target), schedule.ID) {
				continue
			}
			if _, ok := desiredByProviderSchedule[workflowConfigProviderObjectKey(providerName, schedule.ID)]; ok {
				continue
			}
			appName := workflowConfigTargetLabel(schedule.Target)
			providerCtx := invocation.WithWorkflowContextString(ctx, "app", appName)
			if err := provider.DeleteSchedule(providerCtx, coreworkflow.DeleteScheduleRequest{ScheduleID: schedule.ID}); err != nil && !isWorkflowObjectNotFound(err) {
				return fmt.Errorf("bootstrap: delete workflow schedule %q for app %q: %w", schedule.ID, appName, err)
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

func isWorkflowConfigOwnedSchedule(existing *coreworkflow.Schedule, appName, scheduleID string) bool {
	if existing == nil {
		return false
	}
	actor := workflowConfigActor()
	return existing.ID == scheduleID &&
		workflowConfigTargetLabel(existing.Target) == appName &&
		existing.CreatedBy.SubjectID == actor.SubjectID &&
		existing.CreatedBy.SubjectKind == actor.SubjectKind &&
		existing.CreatedBy.AuthSource == actor.AuthSource
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

func workflowConfigTarget(target *config.WorkflowTargetConfig) coreworkflow.Target {
	return config.WorkflowTargetToCore(target)
}

func workflowConfigRunAsSubject(path string, runAs *config.WorkflowRunAsConfig) (*core.RunAsSubject, error) {
	if runAs == nil {
		return nil, nil
	}
	subject := runAs.SubjectRef()
	if subject == nil {
		return nil, fmt.Errorf("config validation: %s.subject is required", path)
	}
	kind, _, ok := core.ParseSubjectID(subject.SubjectID)
	if !ok {
		return nil, fmt.Errorf("config validation: %s.subject.id %q must be a fully-qualified service_account subject", path, subject.SubjectID)
	}
	if kind != "service_account" {
		return nil, fmt.Errorf("config validation: %s.subject.id %q must identify a service_account subject", path, subject.SubjectID)
	}
	if subject.SubjectKind != kind {
		return nil, fmt.Errorf("config validation: %s.subject.kind %q must match subject.id kind %q", path, subject.SubjectKind, kind)
	}
	if subject.AuthSource == "" {
		subject.AuthSource = "config"
	}
	return subject, nil
}

func workflowConfigExecutionPermissions(cfg *config.Config, path string, invokes []config.WorkflowInvokeConfig) ([]core.AccessPermission, error) {
	if len(invokes) == 0 {
		return nil, nil
	}
	out := make([]core.AccessPermission, 0, len(invokes))
	appIndexes := make(map[string]int, len(invokes))
	seenOperations := make(map[string]map[string]struct{}, len(invokes))
	for i, invoke := range invokes {
		app := strings.TrimSpace(invoke.App)
		if app == "" {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].app is required", path, i)
		}
		if cfg == nil || cfg.Apps[app] == nil {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].app references unknown app %q", path, i, app)
		}
		operation := strings.TrimSpace(invoke.Operation)
		if operation == "" {
			return nil, fmt.Errorf("config validation: %s.invokes[%d].operation is required", path, i)
		}
		if seenOperations[app] == nil {
			seenOperations[app] = map[string]struct{}{}
		}
		if _, exists := seenOperations[app][operation]; exists {
			continue
		}
		seenOperations[app][operation] = struct{}{}
		idx, ok := appIndexes[app]
		if !ok {
			idx = len(out)
			appIndexes[app] = idx
			out = append(out, core.AccessPermission{App: app})
		}
		out[idx].Operations = append(out[idx].Operations, operation)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func workflowConfigExecutionPermissionsForTarget(target coreworkflow.Target, explicit ...[]core.AccessPermission) []core.AccessPermission {
	base := make([]core.AccessPermission, 0)
	for i := range target.Steps {
		step := target.Steps[i]
		if step.App != nil {
			appName := strings.TrimSpace(step.App.Name)
			operation := strings.TrimSpace(step.App.Operation)
			if appName != "" && operation != "" {
				base = append(base, core.AccessPermission{App: appName, Operations: []string{operation}})
			}
		}
		if step.Agent != nil {
			providerName := strings.TrimSpace(step.Agent.ProviderName)
			if providerName != "" {
				base = append(base, core.AccessPermission{App: providerName})
			}
			for j := range step.Agent.ToolRefs {
				tool := step.Agent.ToolRefs[j]
				appName := strings.TrimSpace(tool.App)
				operation := strings.TrimSpace(tool.Operation)
				if appName == "" || operation == "" {
					continue
				}
				base = append(base, core.AccessPermission{App: appName, Operations: []string{operation}})
			}
		}
	}
	return workflowMergeExecutionPermissions(append([][]core.AccessPermission{base}, explicit...)...)
}

func workflowMergeExecutionPermissions(groups ...[]core.AccessPermission) []core.AccessPermission {
	out := make([]core.AccessPermission, 0)
	appIndexes := map[string]int{}
	seenOperations := map[string]map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			app := strings.TrimSpace(value.App)
			if app == "" {
				continue
			}
			operations := make([]string, 0, len(value.Operations))
			for _, operation := range value.Operations {
				operation = strings.TrimSpace(operation)
				if operation != "" {
					operations = append(operations, operation)
				}
			}
			if len(operations) == 0 {
				if _, ok := appIndexes[app]; !ok {
					appIndexes[app] = len(out)
					out = append(out, core.AccessPermission{App: app})
				}
				continue
			}
			idx, ok := appIndexes[app]
			if !ok {
				idx = len(out)
				appIndexes[app] = idx
				seenOperations[app] = map[string]struct{}{}
				out = append(out, core.AccessPermission{App: app})
			} else if seenOperations[app] == nil {
				seenOperations[app] = map[string]struct{}{}
			}
			for _, operation := range operations {
				if _, exists := seenOperations[app][operation]; exists {
					continue
				}
				seenOperations[app][operation] = struct{}{}
				out[idx].Operations = append(out[idx].Operations, operation)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func workflowConfigInvocationContext(ctx context.Context, tokens *appaccessservice.InvocationTokenManager, runAs *core.RunAsSubject, permissions []core.AccessPermission) (context.Context, error) {
	if tokens == nil {
		return ctx, nil
	}
	tokenCtx := principal.WithPrincipal(ctx, workflowConfigPrincipal(runAs, permissions))
	token, err := tokens.MintRootToken(tokenCtx, workflowConfigOwnerSubjectID(), workflowConfigInvocationGrants(permissions))
	if err != nil {
		return nil, fmt.Errorf("workflow config invocation token: %w", err)
	}
	return appaccessservice.WithInvocationToken(ctx, token), nil
}

func workflowConfigPrincipal(runAs *core.RunAsSubject, permissions []core.AccessPermission) *principal.Principal {
	actor := workflowConfigActor()
	subjectID := strings.TrimSpace(actor.SubjectID)
	subjectKind := strings.TrimSpace(actor.SubjectKind)
	credentialSubjectID := subjectID
	displayName := strings.TrimSpace(actor.DisplayName)
	authSource := strings.TrimSpace(actor.AuthSource)
	if runAs := core.NormalizeRunAsSubject(runAs); runAs != nil {
		subjectID = strings.TrimSpace(runAs.SubjectID)
		subjectKind = strings.TrimSpace(runAs.SubjectKind)
		credentialSubjectID = strings.TrimSpace(runAs.CredentialSubjectID)
		displayName = strings.TrimSpace(runAs.DisplayName)
		authSource = strings.TrimSpace(runAs.AuthSource)
	}
	compiled := principal.CompilePermissions(permissions)
	value := &principal.Principal{
		SubjectID:           subjectID,
		CredentialSubjectID: credentialSubjectID,
		DisplayName:         displayName,
		Kind:                principal.Kind(subjectKind),
		Scopes:              principal.PermissionApps(compiled),
		TokenPermissions:    compiled,
	}
	principal.SetAuthSource(value, authSource)
	if value.CredentialSubjectID == "" && principal.IsSystemSubjectID(value.SubjectID) {
		value.CredentialSubjectID = value.SubjectID
	}
	return principal.Canonicalize(value)
}

func workflowConfigInvocationGrants(permissions []core.AccessPermission) appaccessservice.InvocationGrants {
	if len(permissions) == 0 {
		return nil
	}
	grants := make(appaccessservice.InvocationGrants, len(permissions))
	for _, permission := range permissions {
		appName := strings.TrimSpace(permission.App)
		if appName == "" {
			continue
		}
		grant := grants[appName]
		operations := make(map[string]core.ConnectionMode)
		for _, operation := range permission.Operations {
			operation = strings.TrimSpace(operation)
			if operation != "" {
				operations[operation] = ""
			}
		}
		if len(operations) == 0 {
			grant.AllOperations = true
		} else {
			grant.Operations = operations
		}
		grants[appName] = grant
	}
	if len(grants) == 0 {
		return nil
	}
	return grants
}

func workflowConfigActor() coreworkflow.Actor {
	return coreworkflow.Actor{
		SubjectID:   workflowConfigOwnerSubjectID(),
		SubjectKind: "system",
		DisplayName: "Workflow Config",
		AuthSource:  "config",
	}
}

func workflowConfigScheduleID(scheduleKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(scheduleKey)))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigValidateExecutionTarget(cfg *config.Config, target coreworkflow.Target, runAs *core.RunAsSubject, _ []core.AccessPermission) error {
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
					Name:       strings.TrimSpace(tool.App),
					Operation:  strings.TrimSpace(tool.Operation),
					Connection: strings.TrimSpace(tool.Connection),
					Instance:   strings.TrimSpace(tool.Instance),
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
	case core.ConnectionModeUser:
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
	case core.ConnectionModeUser:
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
		if mode == core.ConnectionModeUser {
			return core.ConnectionModeUser, nil
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
