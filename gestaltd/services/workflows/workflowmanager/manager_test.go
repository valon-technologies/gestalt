package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type recordingWorkflowManagerInvoker struct {
	requireNone bool
	modes       []core.ConnectionMode
}

func (i *recordingWorkflowManagerInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return &core.OperationResult{}, nil
}

func (i *recordingWorkflowManagerInvoker) ResolveToken(ctx context.Context, _ *principal.Principal, _, _, _ string) (context.Context, string, error) {
	mode := invocation.CredentialModeOverrideFromContext(ctx)
	i.modes = append(i.modes, mode)
	if i.requireNone && mode != core.ConnectionModeNone {
		return ctx, "", invocation.ErrNoCredential
	}
	return ctx, "token", nil
}

func workflowManagerStepsPluginTarget() coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Plugin: &coreworkflow.PluginCall{
			Name:      "github",
			Operation: "issues.triage",
			Input:     coreworkflow.Value{Object: map[string]coreworkflow.Value{}},
		},
	}}}
}

func testWorkflowPluginTarget(pluginName, operation string, input map[string]any, credentialMode ...core.ConnectionMode) coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:     "run",
		Plugin: testWorkflowPluginCall(pluginName, operation, input, credentialMode...),
	}}}
}

func testWorkflowPluginCall(pluginName, operation string, input map[string]any, credentialMode ...core.ConnectionMode) *coreworkflow.PluginCall {
	call := &coreworkflow.PluginCall{
		Name:      pluginName,
		Operation: operation,
	}
	if len(credentialMode) > 0 {
		call.CredentialMode = credentialMode[0]
	}
	if input != nil {
		call.Input = coreworkflow.Value{Object: map[string]coreworkflow.Value{}}
		for key, value := range input {
			call.Input.Object[key] = coreworkflow.Value{Literal: value, LiteralSet: true}
		}
	}
	return call
}

func testWorkflowAgentTarget(agent coreworkflow.AgentTurn) coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:    "run",
		Agent: &agent,
	}}}
}

func applyTestDefinition(t *testing.T, manager *Manager, caller *principal.Principal, providerName, callerPluginName, idempotencyKey string, target coreworkflow.Target) *ManagedDefinition {
	t.Helper()
	definition, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     providerName,
		CallerPluginName: callerPluginName,
		IdempotencyKey:   idempotencyKey,
		Spec: coreworkflow.DefinitionSpec{
			Target: target,
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	return definition
}

func applyTestDefinitionSpec(t *testing.T, manager *Manager, caller *principal.Principal, providerName, callerPluginName, idempotencyKey string, spec coreworkflow.DefinitionSpec) *ManagedDefinition {
	t.Helper()
	definition, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     providerName,
		CallerPluginName: callerPluginName,
		IdempotencyKey:   idempotencyKey,
		Spec:             spec,
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	return definition
}

func testManagedDefinitionID(t *testing.T, definition *ManagedDefinition) string {
	t.Helper()
	if definition == nil || definition.Definition == nil {
		t.Fatalf("definition = %#v, want workflow definition", definition)
	}
	return strings.TrimSpace(definition.Definition.Spec.ID)
}

func requireWorkflowPluginStep(t *testing.T, target coreworkflow.Target, stepIndex int) *coreworkflow.PluginCall {
	t.Helper()
	if len(target.Steps) <= stepIndex || target.Steps[stepIndex].Plugin == nil {
		t.Fatalf("target steps = %#v, want plugin step at index %d", target.Steps, stepIndex)
	}
	return target.Steps[stepIndex].Plugin
}

func TestDefinitionCanStartRunFromStoredTargetSnapshot(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	definition := applyTestDefinition(t, manager, caller, "local", "github", "triage-definition", testWorkflowPluginTarget("github", "issues.triage", map[string]any{"mode": "full"}))
	definitionID := testManagedDefinitionID(t, definition)

	run, err := manager.StartRun(context.Background(), caller, RunStart{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		WorkflowKey:      "github:issues:triage",
	})
	if err != nil {
		t.Fatalf("StartRun by definition: %v", err)
	}
	if run == nil || run.Run == nil {
		t.Fatalf("run = %#v, want plugin target", run)
	}
	runPlugin := requireWorkflowPluginStep(t, run.Run.Target, 0)
	if got := runPlugin.Operation; got != "issues.triage" {
		t.Fatalf("run target operation = %q, want issues.triage", got)
	}
	if got := runPlugin.Input.Object["mode"].Literal; got != "full" {
		t.Fatalf("run target input mode = %v, want full", got)
	}
	if run.ExecutionRef == nil || run.ExecutionRef.ID != definition.ExecutionRef.ID {
		t.Fatalf("run execution ref = %#v, want definition ref %q", run.ExecutionRef, definition.ExecutionRef.ID)
	}
	if run.ExecutionRef.SourceDefinitionID != definitionID {
		t.Fatalf("run source definition id = %q, want %q", run.ExecutionRef.SourceDefinitionID, definitionID)
	}
}

func TestPublishEventPreservesCallerPluginName(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow: testWorkflowControl{provider: provider},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: principal.UserSubjectID("ada"),
		UserID:    "ada",
		Kind:      principal.KindUser,
	})

	if _, err := manager.PublishEvent(context.Background(), caller, EventPublish{
		ProviderName: "local",
		PluginName:   " github ",
		Event:        coreworkflow.Event{Type: "issue.created"},
	}); err != nil {
		t.Fatalf("PublishEvent selected provider: %v", err)
	}
	if _, err := manager.PublishEvent(context.Background(), caller, EventPublish{
		PluginName: " github ",
		Event:      coreworkflow.Event{Type: "issue.updated"},
	}); err != nil {
		t.Fatalf("PublishEvent fan-out: %v", err)
	}
	if len(provider.publishedEvents) != 2 {
		t.Fatalf("published events = %d, want 2", len(provider.publishedEvents))
	}
	for i, req := range provider.publishedEvents {
		if req.PluginName != "github" {
			t.Fatalf("publishedEvents[%d].PluginName = %q, want github", i, req.PluginName)
		}
	}
}

func TestDefinitionCanCreateScheduleAndEventTriggerFromStoredTargetSnapshot(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	definition := applyTestDefinition(t, manager, caller, "local", "github", "", testWorkflowPluginTarget("github", "issues.triage", map[string]any{"mode": "full"}))
	definitionID := testManagedDefinitionID(t, definition)

	schedule, err := manager.CreateSchedule(context.Background(), caller, ScheduleUpsert{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		Cron:             "*/5 * * * *",
		Timezone:         "UTC",
	})
	if err != nil {
		t.Fatalf("CreateSchedule by definition: %v", err)
	}
	if schedule == nil || schedule.Schedule == nil {
		t.Fatalf("schedule = %#v, want plugin target", schedule)
	}
	if got := requireWorkflowPluginStep(t, schedule.Schedule.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("schedule target operation = %q, want issues.triage", got)
	}
	if schedule.ExecutionRef == nil || !strings.HasPrefix(schedule.ExecutionRef.ID, workflowScheduleExecutionRefBasePrefix) {
		t.Fatalf("schedule execution ref = %#v, want schedule snapshot ref", schedule.ExecutionRef)
	}
	if schedule.ExecutionRef.ID == definitionID {
		t.Fatalf("schedule execution ref reused definition id %q, want snapshot ref", schedule.ExecutionRef.ID)
	}
	if schedule.ExecutionRef.SourceDefinitionID != definitionID {
		t.Fatalf("schedule source definition id = %q, want %q", schedule.ExecutionRef.SourceDefinitionID, definitionID)
	}

	trigger, err := manager.CreateEventTrigger(context.Background(), caller, EventTriggerUpsert{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		Match:            coreworkflow.EventMatch{Type: "github.issue.opened"},
	})
	if err != nil {
		t.Fatalf("CreateEventTrigger by definition: %v", err)
	}
	if trigger == nil || trigger.Trigger == nil {
		t.Fatalf("trigger = %#v, want plugin target", trigger)
	}
	if got := requireWorkflowPluginStep(t, trigger.Trigger.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("trigger target operation = %q, want issues.triage", got)
	}
	if trigger.ExecutionRef == nil || !strings.HasPrefix(trigger.ExecutionRef.ID, workflowEventTriggerExecutionRefBasePrefix) {
		t.Fatalf("trigger execution ref = %#v, want trigger snapshot ref", trigger.ExecutionRef)
	}
	if trigger.ExecutionRef.ID == definitionID {
		t.Fatalf("trigger execution ref reused definition id %q, want snapshot ref", trigger.ExecutionRef.ID)
	}
	if trigger.ExecutionRef.SourceDefinitionID != definitionID {
		t.Fatalf("trigger source definition id = %q, want %q", trigger.ExecutionRef.SourceDefinitionID, definitionID)
	}
}

func TestDefinitionIdempotentCreateRetriesUseExistingSnapshots(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
					{ID: "issues.updated", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage", "issues.updated"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	definition := applyTestDefinition(t, manager, caller, "local", "github", "", testWorkflowPluginTarget("github", "issues.triage", map[string]any{"mode": "initial"}))
	definitionID := testManagedDefinitionID(t, definition)

	scheduleReq := ScheduleUpsert{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		IdempotencyKey:   "triage-schedule",
		Cron:             "*/5 * * * *",
		Timezone:         "UTC",
	}
	schedule, err := manager.CreateSchedule(context.Background(), caller, scheduleReq)
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	applyTestDefinitionSpec(t, manager, caller, "local", "github", "", coreworkflow.DefinitionSpec{
		ID:         definitionID,
		Generation: 2,
		Target:     testWorkflowPluginTarget("github", "issues.updated", map[string]any{"mode": "updated"}),
	})

	replayedSchedule, err := manager.CreateSchedule(context.Background(), caller, scheduleReq)
	if err != nil {
		t.Fatalf("CreateSchedule replay after definition update: %v", err)
	}
	if replayedSchedule.Schedule.ID != schedule.Schedule.ID {
		t.Fatalf("replayed schedule ID = %q, want %q", replayedSchedule.Schedule.ID, schedule.Schedule.ID)
	}
	if got := requireWorkflowPluginStep(t, replayedSchedule.Schedule.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("replayed schedule target operation = %q, want original snapshot", got)
	}
	if len(provider.upsertedSchedules) != 1 {
		t.Fatalf("provider upserted schedules = %d, want 1", len(provider.upsertedSchedules))
	}

	otherDefinition := applyTestDefinition(t, manager, caller, "local", "github", "", testWorkflowPluginTarget("github", "issues.updated", map[string]any{"mode": "other"}))
	otherDefinitionID := testManagedDefinitionID(t, otherDefinition)
	otherScheduleReq := scheduleReq
	otherScheduleReq.DefinitionID = otherDefinitionID
	_, err = manager.CreateSchedule(context.Background(), caller, otherScheduleReq)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("CreateSchedule replay with different definition error = %v, want invalid invocation", err)
	}

	triggerReq := EventTriggerUpsert{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		IdempotencyKey:   "triage-trigger",
		Match:            coreworkflow.EventMatch{Type: "github.issue.opened"},
	}
	trigger, err := manager.CreateEventTrigger(context.Background(), caller, triggerReq)
	if err != nil {
		t.Fatalf("CreateEventTrigger: %v", err)
	}
	otherTriggerReq := triggerReq
	otherTriggerReq.DefinitionID = otherDefinitionID
	_, err = manager.CreateEventTrigger(context.Background(), caller, otherTriggerReq)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("CreateEventTrigger replay with different definition error = %v, want invalid invocation", err)
	}

	if err := manager.DeleteDefinition(context.Background(), caller, definitionID); err != nil {
		t.Fatalf("DeleteDefinition: %v", err)
	}
	replayedTrigger, err := manager.CreateEventTrigger(context.Background(), caller, triggerReq)
	if err != nil {
		t.Fatalf("CreateEventTrigger replay after definition delete: %v", err)
	}
	if replayedTrigger.Trigger.ID != trigger.Trigger.ID {
		t.Fatalf("replayed trigger ID = %q, want %q", replayedTrigger.Trigger.ID, trigger.Trigger.ID)
	}
	if got := requireWorkflowPluginStep(t, replayedTrigger.Trigger.Target, 0).Operation; got != "issues.updated" {
		t.Fatalf("replayed trigger target operation = %q, want stored snapshot", got)
	}
	if len(provider.upsertedEventTriggers) != 1 {
		t.Fatalf("provider upserted event triggers = %d, want 1", len(provider.upsertedEventTriggers))
	}
}

func TestDefinitionRunsUseDefinitionProvider(t *testing.T) {
	t.Parallel()

	localProvider := newTestWorkflowProvider()
	remoteProvider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: namedTestWorkflowControl{
			defaultName: "local",
			names:       []string{"local", "remote"},
			providers: map[string]coreworkflow.Provider{
				"local":  localProvider,
				"remote": remoteProvider,
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	definition := applyTestDefinition(t, manager, caller, "remote", "github", "", testWorkflowPluginTarget("github", "issues.triage", nil))
	definitionID := testManagedDefinitionID(t, definition)

	run, err := manager.StartRun(context.Background(), caller, RunStart{
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		WorkflowKey:      "github:issues:triage",
	})
	if err != nil {
		t.Fatalf("StartRun by definition: %v", err)
	}
	if run.ProviderName != "remote" {
		t.Fatalf("run provider = %q, want remote", run.ProviderName)
	}
	if len(remoteProvider.runs) != 1 {
		t.Fatalf("remote provider runs = %d, want 1", len(remoteProvider.runs))
	}
	if len(localProvider.runs) != 0 {
		t.Fatalf("local provider runs = %d, want 0", len(localProvider.runs))
	}

	_, err = manager.StartRun(context.Background(), caller, RunStart{
		ProviderName:     "local",
		CallerPluginName: "github",
		DefinitionID:     definitionID,
		WorkflowKey:      "github:issues:triage:local",
	})
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("StartRun with mismatched provider error = %v, want invalid invocation", err)
	}
}

func TestListRunsResumesTokenlessProviderOverrun(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})
	target := testWorkflowPluginTarget("github", "issues.triage", nil)
	for _, id := range []string{"1", "2", "3"} {
		refID := "ref-" + id
		provider.refs[refID] = &coreworkflow.ExecutionReference{
			ID:           refID,
			ProviderName: "local",
			Target:       target,
			SubjectID:    principal.UserSubjectID("ada"),
			SubjectKind:  string(principal.KindUser),
		}
		provider.runs["run-"+id] = &coreworkflow.Run{
			ID:           "run-" + id,
			Status:       coreworkflow.RunStatusRunning,
			Target:       target,
			ExecutionRef: refID,
		}
	}

	first, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("ListRuns(first): %v", err)
	}
	if got := workflowManagerRunIDs(first.Runs); !reflect.DeepEqual(got, []string{"run-1", "run-2"}) || first.NextPageToken == "" {
		t.Fatalf("first page ids=%v next=%q, want first two runs and token", got, first.NextPageToken)
	}

	second, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 2, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatalf("ListRuns(second): %v", err)
	}
	if got := workflowManagerRunIDs(second.Runs); !reflect.DeepEqual(got, []string{"run-3"}) || second.NextPageToken != "" {
		t.Fatalf("second page ids=%v next=%q, want final run", got, second.NextPageToken)
	}
}

func TestListRunsSkipsFilteredProviderPages(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	target := testWorkflowPluginTarget("github", "issues.triage", nil)
	provider.refs["ref-hidden"] = &coreworkflow.ExecutionReference{
		ID:           "ref-hidden",
		ProviderName: "local",
		Target:       target,
		SubjectID:    principal.UserSubjectID("grace"),
		SubjectKind:  string(principal.KindUser),
	}
	provider.refs["ref-visible"] = &coreworkflow.ExecutionReference{
		ID:           "ref-visible",
		ProviderName: "local",
		Target:       target,
		SubjectID:    principal.UserSubjectID("ada"),
		SubjectKind:  string(principal.KindUser),
	}
	provider.listRunsHook = func(req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		switch strings.TrimSpace(req.PageToken) {
		case "":
			return &coreworkflow.ListRunsResponse{
				Runs: []*coreworkflow.Run{{
					ID:           "run-hidden",
					Status:       coreworkflow.RunStatusRunning,
					Target:       target,
					ExecutionRef: "ref-hidden",
				}},
				NextPageToken: "page-2",
			}, nil
		case "page-2":
			return &coreworkflow.ListRunsResponse{
				Runs: []*coreworkflow.Run{{
					ID:           "run-visible",
					Status:       coreworkflow.RunStatusRunning,
					Target:       target,
					ExecutionRef: "ref-visible",
				}},
			}, nil
		default:
			t.Fatalf("unexpected provider page token %q", req.PageToken)
			return nil, nil
		}
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	resp, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 1})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if got := workflowManagerRunIDs(resp.Runs); !reflect.DeepEqual(got, []string{"run-visible"}) || resp.NextPageToken != "" {
		t.Fatalf("runs=%v next=%q, want visible run without an empty-page token", got, resp.NextPageToken)
	}
}

func TestListRunsOrdersCandidatesAcrossProvidersNewestFirst(t *testing.T) {
	t.Parallel()

	localProvider := newTestWorkflowProvider()
	remoteProvider := newTestWorkflowProvider()
	target := testWorkflowPluginTarget("github", "issues.triage", nil)
	addRun := func(provider *testWorkflowProvider, providerName, runID string, createdAt time.Time) {
		refID := "ref-" + runID
		provider.refs[refID] = &coreworkflow.ExecutionReference{
			ID:           refID,
			ProviderName: providerName,
			Target:       target,
			SubjectID:    principal.UserSubjectID("ada"),
			SubjectKind:  string(principal.KindUser),
		}
		if provider != localProvider {
			copied := *provider.refs[refID]
			localProvider.refs[refID] = &copied
		}
		provider.runs[runID] = &coreworkflow.Run{
			ID:           runID,
			Status:       coreworkflow.RunStatusRunning,
			Target:       target,
			ExecutionRef: refID,
			CreatedAt:    &createdAt,
		}
	}
	addRun(localProvider, "local", "run-old", time.Unix(100, 0).UTC())
	addRun(localProvider, "local", "run-mid", time.Unix(200, 0).UTC())
	addRun(remoteProvider, "remote", "run-new", time.Unix(300, 0).UTC())
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: namedTestWorkflowControl{
			defaultName: "local",
			names:       []string{"local", "remote"},
			providers: map[string]coreworkflow.Provider{
				"local":  localProvider,
				"remote": remoteProvider,
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	first, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("ListRuns(first): %v", err)
	}
	if got := workflowManagerRunIDs(first.Runs); !reflect.DeepEqual(got, []string{"run-new", "run-mid"}) || first.NextPageToken == "" {
		t.Fatalf("first page ids=%v next=%q, want newest runs and token", got, first.NextPageToken)
	}

	second, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 2, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatalf("ListRuns(second): %v", err)
	}
	if got := workflowManagerRunIDs(second.Runs); !reflect.DeepEqual(got, []string{"run-old"}) || second.NextPageToken != "" {
		t.Fatalf("second page ids=%v next=%q, want final older run", got, second.NextPageToken)
	}
}

func TestListRunsFiltersTargetPluginInManager(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	githubTarget := testWorkflowPluginTarget("github", "issues.triage", nil)
	slackTarget := testWorkflowPluginTarget("slack", "chat.postMessage", nil)
	multiTarget := coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			ID:     "triage",
			Plugin: testWorkflowPluginCall("github", "issues.triage", nil),
		},
		{
			ID:     "notify",
			Plugin: testWorkflowPluginCall("slack", "chat.postMessage", nil),
		},
	}}
	provider.refs["ref-github"] = &coreworkflow.ExecutionReference{
		ID:           "ref-github",
		ProviderName: "local",
		Target:       githubTarget,
		SubjectID:    principal.UserSubjectID("ada"),
		SubjectKind:  string(principal.KindUser),
	}
	provider.refs["ref-slack"] = &coreworkflow.ExecutionReference{
		ID:           "ref-slack",
		ProviderName: "local",
		Target:       slackTarget,
		SubjectID:    principal.UserSubjectID("ada"),
		SubjectKind:  string(principal.KindUser),
	}
	provider.refs["ref-multi"] = &coreworkflow.ExecutionReference{
		ID:           "ref-multi",
		ProviderName: "local",
		Target:       multiTarget,
		SubjectID:    principal.UserSubjectID("ada"),
		SubjectKind:  string(principal.KindUser),
	}
	provider.listRunsHook = func(req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		if req.TargetPlugin != "" {
			t.Fatalf("provider TargetPlugin = %q, want manager-owned filter", req.TargetPlugin)
		}
		return &coreworkflow.ListRunsResponse{Runs: []*coreworkflow.Run{
			{
				ID:           "run-github",
				Status:       coreworkflow.RunStatusRunning,
				Target:       githubTarget,
				ExecutionRef: "ref-github",
			},
			{
				ID:           "run-slack",
				Status:       coreworkflow.RunStatusRunning,
				Target:       slackTarget,
				ExecutionRef: "ref-slack",
			},
			{
				ID:           "run-multi",
				Status:       coreworkflow.RunStatusRunning,
				Target:       multiTarget,
				ExecutionRef: "ref-multi",
			},
		}}, nil
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "github",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "github",
					Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
				},
			},
			&coretesting.StubIntegration{
				N:        "slack",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "slack",
					Operations: []catalog.CatalogOperation{{ID: "chat.postMessage", Method: "POST"}},
				},
			},
		),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "github", Operations: []string{"issues.triage"}},
		{Plugin: "slack", Operations: []string{"chat.postMessage"}},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	resp, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{TargetPlugin: "slack"})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if got := workflowManagerRunIDs(resp.Runs); !reflect.DeepEqual(got, []string{"run-multi", "run-slack"}) {
		t.Fatalf("ListRuns ids = %v, want runs with any slack step", got)
	}
}

func TestListRunsKeepsUnorderedTokenlessProviderRunsReachable(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	target := testWorkflowPluginTarget("github", "issues.triage", nil)
	for _, id := range []string{"old", "new"} {
		refID := "ref-" + id
		provider.refs[refID] = &coreworkflow.ExecutionReference{
			ID:           refID,
			ProviderName: "local",
			Target:       target,
			SubjectID:    principal.UserSubjectID("ada"),
			SubjectKind:  string(principal.KindUser),
		}
	}
	oldCreatedAt := time.Unix(100, 0).UTC()
	newCreatedAt := time.Unix(200, 0).UTC()
	provider.listRunsHook = func(coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		return &coreworkflow.ListRunsResponse{Runs: []*coreworkflow.Run{
			{
				ID:           "run-old",
				Status:       coreworkflow.RunStatusRunning,
				Target:       target,
				ExecutionRef: "ref-old",
				CreatedAt:    &oldCreatedAt,
			},
			{
				ID:           "run-new",
				Status:       coreworkflow.RunStatusRunning,
				Target:       target,
				ExecutionRef: "ref-new",
				CreatedAt:    &newCreatedAt,
			},
		}}, nil
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	first, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 1})
	if err != nil {
		t.Fatalf("ListRuns(first): %v", err)
	}
	if got := workflowManagerRunIDs(first.Runs); !reflect.DeepEqual(got, []string{"run-new"}) || first.NextPageToken == "" {
		t.Fatalf("first page ids=%v next=%q, want newest run and token", got, first.NextPageToken)
	}

	second, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{PageSize: 1, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatalf("ListRuns(second): %v", err)
	}
	if got := workflowManagerRunIDs(second.Runs); !reflect.DeepEqual(got, []string{"run-old"}) || second.NextPageToken != "" {
		t.Fatalf("second page ids=%v next=%q, want older run without token", got, second.NextPageToken)
	}
}

func TestListRunsPageTokenRejectsChangedFiltersOrProviders(t *testing.T) {
	t.Parallel()

	req := coreworkflow.ListRunsRequest{
		PageSize:     10,
		TargetPlugin: "github",
		Status:       coreworkflow.RunStatusRunning,
	}
	pageSize, err := effectiveWorkflowRunListPageSize(req.PageSize)
	if err != nil {
		t.Fatalf("effective page size: %v", err)
	}
	token := workflowRunListNextPageToken([]string{"local", "remote"}, req, pageSize, []workflowRunProviderPageState{
		{ProviderName: "local", ProviderToken: "provider-page-2"},
		{ProviderName: "remote", Exhausted: true},
	})

	if _, err := decodeWorkflowRunListPageToken(token, []string{"local", "remote"}, coreworkflow.ListRunsRequest{
		PageSize:     10,
		TargetPlugin: "slack",
		Status:       coreworkflow.RunStatusRunning,
	}, pageSize); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with changed filter error = %v, want invalid invocation", err)
	}

	if _, err := decodeWorkflowRunListPageToken(token, []string{"remote", "local"}, req, pageSize); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with reordered providers error = %v, want invalid invocation", err)
	}

	if _, err := decodeWorkflowRunListPageToken(token, []string{"local", "remote"}, coreworkflow.ListRunsRequest{
		PageSize:     20,
		TargetPlugin: "github",
		Status:       coreworkflow.RunStatusRunning,
	}, 20); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with changed page size error = %v, want invalid invocation", err)
	}
}

func TestUpdateDefinitionProviderChangeDoesNotExposeDuplicateActiveRefs(t *testing.T) {
	t.Parallel()

	localProvider := newTestWorkflowProvider()
	remoteProvider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
				},
			},
		}),
		Workflow: namedTestWorkflowControl{
			defaultName: "local",
			names:       []string{"local", "remote"},
			providers: map[string]coreworkflow.Provider{
				"local":  localProvider,
				"remote": remoteProvider,
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	definition := applyTestDefinition(t, manager, caller, "local", "github", "", testWorkflowPluginTarget("github", "issues.triage", nil))
	definitionID := testManagedDefinitionID(t, definition)

	var hookErr error
	remoteProvider.putExecutionReferenceHook = func(ref *coreworkflow.ExecutionReference) {
		if definitionIDFromExecutionRefID(ref.ID) != definitionID || ref.RevokedAt != nil || strings.TrimSpace(ref.ProviderName) != "remote" {
			return
		}
		managed, err := manager.GetDefinition(context.Background(), caller, definitionID)
		if err != nil {
			hookErr = err
			return
		}
		if managed.ProviderName != "remote" {
			hookErr = fmt.Errorf("definition provider during update = %q, want remote", managed.ProviderName)
		}
	}

	updated := applyTestDefinitionSpec(t, manager, caller, "remote", "github", "", coreworkflow.DefinitionSpec{
		ID:         definitionID,
		Generation: definition.Definition.Spec.Generation + 1,
		Target:     testWorkflowPluginTarget("github", "issues.triage", nil),
	})
	if hookErr != nil {
		t.Fatalf("GetDefinition during provider change: %v", hookErr)
	}
	if updated.ProviderName != "remote" {
		t.Fatalf("updated provider = %q, want remote", updated.ProviderName)
	}
	ref := remoteProvider.refs[updated.ExecutionRef.ID]
	if ref == nil || ref.RevokedAt != nil || ref.ProviderName != "remote" {
		t.Fatalf("remote definition ref = %#v, want active remote ref", ref)
	}
}

func TestApplyDefinitionExecutionRefInheritsDeclaredAgentToolInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "bot.commitFiles", Method: "POST"},
					{ID: "bot.commentFinal", Method: "POST"},
					{ID: "bot.commentStarted", Method: "POST"},
					{ID: "bot.openPullRequest", Method: "POST"},
					{ID: "events.handle", Method: "POST"},
				},
			},
		}),
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "bot.commitFiles"},
				{Plugin: "github", Operation: "bot.commentFinal", CredentialMode: core.ConnectionModeNone},
				{Plugin: "github", Operation: "bot.commentStarted", CredentialMode: core.ConnectionModeNone},
				{Plugin: "github", Operation: "bot.openPullRequest"},
			},
		},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	managed, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{
				{
					ID: "run",
					Agent: &coreworkflow.AgentTurn{
						ProviderName: "simple",
						Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
						ToolRefs: []coreagent.ToolRef{
							{Plugin: "github", Operation: "bot.commitFiles"},
							{Plugin: "github", Operation: "bot.openPullRequest"},
						},
					},
					OutputDelivery: &coreworkflow.StepDelivery{
						Plugin: testWorkflowPluginCall("github", "bot.commentFinal", nil, core.ConnectionModeNone),
					},
				},
				{
					ID:     "session_ready",
					Plugin: testWorkflowPluginCall("github", "bot.commentStarted", nil, core.ConnectionModeNone),
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil {
		t.Fatalf("managed signal = %#v, want execution ref", managed)
	}

	wantPermissions := []core.AccessPermission{{
		Plugin: "github",
		Operations: []string{
			"bot.commentFinal",
			"bot.commentStarted",
			"bot.commitFiles",
			"bot.openPullRequest",
			"events.handle",
		},
	}, {
		Plugin: "simple",
	}, {
		Plugin:  coreworkflow.StepActionPermissionPlugin,
		Actions: []string{"step/run/agent-turn", "step/run/delivery", "step/session_ready/plugin"},
	}}
	if !reflect.DeepEqual(managed.ExecutionRef.Permissions, wantPermissions) {
		t.Fatalf("execution ref permissions = %#v, want %#v", managed.ExecutionRef.Permissions, wantPermissions)
	}
	if managed.ExecutionRef.CallerPluginName != "github" {
		t.Fatalf("caller plugin = %q, want github", managed.ExecutionRef.CallerPluginName)
	}
	if got := managed.ExecutionRef.Target.Steps[0].OutputDelivery.Plugin.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("output delivery credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := requireWorkflowPluginStep(t, managed.ExecutionRef.Target, 1).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("session ready plugin credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
}

func TestApplyDefinitionRejectsStepWhenMissingEquals(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{Plugin: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName: "local",
		Spec: coreworkflow.DefinitionSpec{
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{
				{
					ID: "diagnosis",
					Agent: &coreworkflow.AgentTurn{
						ProviderName: "simple",
						Prompt:       coreworkflow.Text{Template: "Diagnose the alert."},
					},
				},
				{
					ID: "pr_fix",
					Agent: &coreworkflow.AgentTurn{
						ProviderName: "simple",
						Prompt:       coreworkflow.Text{Template: "Open a PR."},
					},
					When: &coreworkflow.StepWhen{
						Value: coreworkflow.Value{
							StepOutput: &coreworkflow.StepOutputSource{
								StepID: "diagnosis",
								Path:   "structured_output.actionable_for_pr",
							},
						},
					},
				},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target.steps[1].when.equals is required") {
		t.Fatalf("ApplyDefinition error = %v, want missing when.equals validation", err)
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestApplyDefinitionRejectsStepWhenMissingValue(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{Plugin: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName: "local",
		Spec: coreworkflow.DefinitionSpec{
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{
				{
					ID: "diagnosis",
					Agent: &coreworkflow.AgentTurn{
						ProviderName: "simple",
						Prompt:       coreworkflow.Text{Template: "Diagnose the alert."},
					},
				},
				{
					ID: "pr_fix",
					Agent: &coreworkflow.AgentTurn{
						ProviderName: "simple",
						Prompt:       coreworkflow.Text{Template: "Open a PR."},
					},
					When: &coreworkflow.StepWhen{
						Equals:    nil,
						EqualsSet: true,
					},
				},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target.steps[1].when.value is required") {
		t.Fatalf("ApplyDefinition error = %v, want missing when.value validation", err)
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestResolveTargetRejectsInvalidStepOutputRefsOutsideWhen(t *testing.T) {
	t.Parallel()

	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "github",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "github",
					Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
				},
			},
			&coretesting.StubIntegration{
				N:        "slack",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "slack",
					Operations: []catalog.CatalogOperation{{ID: "chat.postMessage", Method: "POST"}},
				},
			},
		),
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "github", Operations: []string{"issues.triage"}},
		{Plugin: "slack", Operations: []string{"chat.postMessage"}},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})
	stepOutput := func(stepID, path string) coreworkflow.Value {
		return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: stepID, Path: path}}
	}
	baseTarget := func() coreworkflow.Target {
		return coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID:     "diagnosis",
				Plugin: testWorkflowPluginCall("github", "issues.triage", nil),
			},
			{
				ID:     "notify",
				Plugin: testWorkflowPluginCall("slack", "chat.postMessage", nil),
			},
		}}
	}

	tests := []struct {
		name   string
		mutate func(*coreworkflow.Target)
		want   string
	}{
		{
			name: "step inputs",
			mutate: func(target *coreworkflow.Target) {
				target.Steps[1].Inputs = map[string]coreworkflow.Value{"summary": stepOutput("future", "plugin.body")}
			},
			want: `workflow target.steps[1].inputs.summary.step_output.step_id "future" must reference an earlier step`,
		},
		{
			name: "plugin input",
			mutate: func(target *coreworkflow.Target) {
				target.Steps[1].Plugin.Input = coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"text": stepOutput("future", "plugin.body"),
				}}
			},
			want: `workflow target.steps[1].plugin.input.text.step_output.step_id "future" must reference an earlier step`,
		},
		{
			name: "output delivery input",
			mutate: func(target *coreworkflow.Target) {
				target.Steps[1].OutputDelivery = &coreworkflow.StepDelivery{Plugin: testWorkflowPluginCall("slack", "chat.postMessage", map[string]any{})}
				target.Steps[1].OutputDelivery.Plugin.Input = coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"text": stepOutput("future", "plugin.body"),
				}}
			},
			want: `workflow target.steps[1].output_delivery.plugin.input.text.step_output.step_id "future" must reference an earlier step`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := baseTarget()
			tt.mutate(&target)
			_, err := manager.resolveTarget(context.Background(), caller, target, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolveTarget error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestApplyDefinitionRejectsAgentStepWithoutPromptOrMessages(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{Plugin: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName: "local",
		Spec: coreworkflow.DefinitionSpec{
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID: "agent",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
				},
			}}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target agent prompt or messages is required") {
		t.Fatalf("ApplyDefinition error = %v, want missing agent prompt/messages validation", err)
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestApplyDefinitionRejectsOutputDeliveryTargetCredentialMode(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "bot.commentFinal", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle", "bot.commentFinal"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID: "run",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
				},
				OutputDelivery: &coreworkflow.StepDelivery{
					Plugin: testWorkflowPluginCall("github", "bot.commentFinal", nil, core.ConnectionMode("unsupported")),
				},
			}}},
		},
	})
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("ApplyDefinition error = %v, want invalid invocation", err)
	}
}

func TestApplyDefinitionPluginTargetCredentialModeUsesDeclaredInvoke(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	invoker := &recordingWorkflowManagerInvoker{requireNone: true}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "reviewPullRequest", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
		Invoker:  invoker,
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "reviewPullRequest", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle", "reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:github_app_installation:99:repo:acme/widgets",
		Kind:             principal.Kind("service_account"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	managed, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec:             coreworkflow.DefinitionSpec{Target: testWorkflowPluginTarget("github", "reviewPullRequest", nil, core.ConnectionModeNone)},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil {
		t.Fatalf("managed definition = %#v, want plugin execution ref", managed)
	}
	if got := requireWorkflowPluginStep(t, managed.ExecutionRef.Target, 0).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("stored credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != core.ConnectionModeNone {
		t.Fatalf("resolver credential modes = %#v, want final %q", invoker.modes, core.ConnectionModeNone)
	}
}

func TestApplyDefinitionPluginTargetCredentialModeKeepsBlankModeBlank(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	invoker := &recordingWorkflowManagerInvoker{}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "reviewPullRequest", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
		Invoker:  invoker,
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "reviewPullRequest", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle", "reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:github_app_installation:99:repo:acme/widgets",
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	managed, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec:             coreworkflow.DefinitionSpec{Target: testWorkflowPluginTarget("github", "reviewPullRequest", nil)},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if got := requireWorkflowPluginStep(t, managed.ExecutionRef.Target, 0).CredentialMode; got != "" {
		t.Fatalf("stored credential mode = %q, want empty", got)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != "" {
		t.Fatalf("resolver credential modes = %#v, want final empty", invoker.modes)
	}
	blankRefID := managed.ExecutionRef.ID

	explicit, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec:             coreworkflow.DefinitionSpec{Target: testWorkflowPluginTarget("github", "reviewPullRequest", nil, core.ConnectionModeNone)},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition explicit mode: %v", err)
	}
	if explicit.ExecutionRef.ID == blankRefID {
		t.Fatalf("explicit credential mode reused blank execution ref id %q", blankRefID)
	}
}

func TestCreateScheduleRejectsPluginTargetCredentialModeWithoutCaller(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N: "github",
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "reviewPullRequest", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	_, err := manager.CreateSchedule(context.Background(), caller, ScheduleUpsert{
		ProviderName: "local",
		Cron:         "*/5 * * * *",
		Target:       testWorkflowPluginTarget("github", "reviewPullRequest", nil, core.ConnectionModeNone),
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("CreateSchedule error = %v, want authorization denied", err)
	}
}

func TestApplyDefinitionStepsTargetCompilesAndBindsProviderPlan(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "github",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "github",
					Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
				},
			},
			&coretesting.StubIntegration{
				N:        "slack",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name:       "slack",
					Operations: []catalog.CatalogOperation{{ID: "chat.post", Method: "POST"}},
				},
			},
		),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "github", Operations: []string{"issues.triage"}},
		{Plugin: "slack", Operations: []string{"chat.post"}},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})
	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Plugin: &coreworkflow.PluginCall{
			Name:      "github",
			Operation: "issues.triage",
			Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
				"title": {SignalPayload: "event.title"},
			}},
		},
		OutputDelivery: &coreworkflow.StepDelivery{Plugin: &coreworkflow.PluginCall{
			Name:      "slack",
			Operation: "chat.post",
			Input:     coreworkflow.Value{Object: map[string]coreworkflow.Value{}},
		}},
	}}}

	managed, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		IdempotencyKey:   "event-1",
		CallerPluginName: "github",
		Spec:             coreworkflow.DefinitionSpec{Target: target},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil {
		t.Fatalf("managed = %#v, want execution ref", managed)
	}
	if managed.ExecutionRef.TargetDigest == "" || managed.ExecutionRef.ActionTableDigest == "" || managed.ExecutionRef.Generation == 0 {
		t.Fatalf("execution ref digests = %#v", managed.ExecutionRef)
	}
	stored := provider.definitions[managed.Definition.Spec.ID]
	if stored == nil || stored.Binding == nil {
		t.Fatalf("stored definition binding = %#v", stored)
	}
	binding := stored.Binding
	if binding.ExecutionRef != managed.ExecutionRef.ID || binding.TargetDigest != managed.ExecutionRef.TargetDigest || binding.ActionTableDigest != managed.ExecutionRef.ActionTableDigest {
		t.Fatalf("definition binding = %#v, ref = %#v", binding, managed.ExecutionRef)
	}
	if !workflowTestAccessPermissionHasAction(managed.ExecutionRef.Permissions, coreworkflow.StepActionPermissionPlugin, "step/diagnose/plugin") {
		t.Fatalf("execution ref permissions missing plugin action: %#v", managed.ExecutionRef.Permissions)
	}
	if !workflowTestAccessPermissionHasAction(managed.ExecutionRef.Permissions, coreworkflow.StepActionPermissionPlugin, "step/diagnose/delivery") {
		t.Fatalf("execution ref permissions missing delivery action: %#v", managed.ExecutionRef.Permissions)
	}
}

func TestApplyDefinitionUsesGenerationExecutionRefAndRevokesOldGeneration(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	first, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:         "triage",
			Generation: 1,
			Target:     workflowManagerStepsPluginTarget(),
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition(first): %v", err)
	}
	second, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:         "triage",
			Generation: 2,
			Target:     workflowManagerStepsPluginTarget(),
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition(second): %v", err)
	}
	if !strings.HasSuffix(first.ExecutionRef.ID, ":1") {
		t.Fatalf("first execution ref ID = %q, want generation suffix :1", first.ExecutionRef.ID)
	}
	if !strings.HasSuffix(second.ExecutionRef.ID, ":2") {
		t.Fatalf("second execution ref ID = %q, want generation suffix :2", second.ExecutionRef.ID)
	}
	storedFirst := provider.refs[first.ExecutionRef.ID]
	if storedFirst == nil || storedFirst.RevokedAt == nil {
		t.Fatalf("first execution ref after generation update = %#v, want revoked", storedFirst)
	}
	storedSecond := provider.refs[second.ExecutionRef.ID]
	if storedSecond == nil || storedSecond.RevokedAt != nil {
		t.Fatalf("second execution ref after generation update = %#v, want active", storedSecond)
	}
	managed, err := manager.GetDefinition(context.Background(), caller, "triage")
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if managed.ExecutionRef.ID != second.ExecutionRef.ID {
		t.Fatalf("GetDefinition execution ref = %q, want %q", managed.ExecutionRef.ID, second.ExecutionRef.ID)
	}
}

func TestApplyDefinitionValidationFailureDoesNotCreateExecutionRef(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:     "triage",
			Target: testWorkflowPluginTarget("github", "issues.unknown", nil),
		},
	})
	if err == nil {
		t.Fatal("ApplyDefinition error = nil, want validation/authorization failure")
	}
	if provider.putExecutionReferenceCalls != 0 {
		t.Fatalf("PutExecutionReference calls = %d, want 0 before provider apply", provider.putExecutionReferenceCalls)
	}
	if len(provider.refs) != 0 {
		t.Fatalf("execution refs = %d, want 0", len(provider.refs))
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestApplyDefinitionDoesNotCreateExecutionRefWhenProviderApplyFails(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	provider.applyWorkflowDefinitionErr = errors.New("apply failed")
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:     "triage",
			Target: workflowManagerStepsPluginTarget(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "apply failed") {
		t.Fatalf("ApplyDefinition error = %v, want provider apply failure", err)
	}
	if provider.putExecutionReferenceCalls != 0 {
		t.Fatalf("PutExecutionReference calls = %d, want 0 before provider apply succeeds", provider.putExecutionReferenceCalls)
	}
	if len(provider.refs) != 0 {
		t.Fatalf("execution refs = %d, want none after failed apply", len(provider.refs))
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0 after failed apply", len(provider.definitions))
	}
}

func TestApplyDefinitionDoesNotCreateExecutionRefWhenDefinitionTargetMismatch(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	provider.applyWorkflowDefinitionHook = func(req coreworkflow.ApplyDefinitionRequest) (*coreworkflow.Definition, error) {
		deployment := &coreworkflow.Definition{
			Spec: coreworkflow.DefinitionSpec{
				ID:     req.Spec.ID,
				Target: testWorkflowPluginTarget("github", "issues.other", nil),
			},
			Status:            coreworkflow.DefinitionStatusActive,
			AppliedGeneration: req.Spec.Generation,
			Binding:           req.Binding,
		}
		if req.Binding != nil {
			deployment.SpecDigest = req.Binding.SpecDigest
			deployment.TargetDigest = req.Binding.TargetDigest
			deployment.ActionTableDigest = req.Binding.ActionTableDigest
		}
		return deployment, nil
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "issues.triage", Method: "POST"},
					{ID: "issues.other", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage", "issues.other"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:     "triage",
			Target: workflowManagerStepsPluginTarget(),
		},
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("ApplyDefinition error = %v, want not found from deployment/ref mismatch", err)
	}
	for _, ref := range provider.refs {
		if ref == nil || ref.RevokedAt == nil {
			t.Fatalf("execution ref = %#v, want revoked after deployment mismatch", ref)
		}
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0 after mismatch rejection", len(provider.definitions))
	}
}

func TestDefinitionStartRunFailureKeepsDefinitionAndExecutionRefActive(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	startAttempts := 0
	provider.startRunHook = func(req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
		startAttempts++
		if startAttempts == 1 {
			return nil, errors.New("start failed")
		}
		run := &coreworkflow.Run{
			ID:           "run-started",
			Status:       coreworkflow.RunStatusRunning,
			WorkflowKey:  req.WorkflowKey,
			Target:       req.Target,
			ExecutionRef: req.ExecutionRef,
			CreatedBy:    req.CreatedBy,
		}
		provider.runs[run.ID] = run
		copied := *run
		return &copied, nil
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})

	deployment, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			ID:     "triage",
			Target: workflowManagerStepsPluginTarget(),
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	refID := deployment.ExecutionRef.ID

	_, err = manager.StartRun(context.Background(), caller, RunStart{
		CallerPluginName: "github",
		DefinitionID:     "triage",
		WorkflowKey:      "github:issues:triage",
	})
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("StartRun(first) error = %v, want start failure", err)
	}
	if len(provider.runs) != 0 {
		t.Fatalf("runs after failed start = %d, want 0", len(provider.runs))
	}
	storedRef := provider.refs[refID]
	if storedRef == nil || storedRef.RevokedAt != nil {
		t.Fatalf("execution ref after failed start = %#v, want active", storedRef)
	}
	if provider.definitions["triage"] == nil {
		t.Fatal("definition missing after failed start, want stored definition")
	}

	run, err := manager.StartRun(context.Background(), caller, RunStart{
		CallerPluginName: "github",
		DefinitionID:     "triage",
		WorkflowKey:      "github:issues:triage",
	})
	if err != nil {
		t.Fatalf("StartRun(retry): %v", err)
	}
	if run == nil || run.Run == nil {
		t.Fatalf("retry run = %#v, want managed run", run)
	}
	if len(provider.runs) != 1 {
		t.Fatalf("runs after retry = %d, want 1", len(provider.runs))
	}
}

func TestStartRunRequiresDefinition(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "github",
				Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionPlugins(permissions),
	})
	req := RunStart{
		ProviderName:     "local",
		CallerPluginName: "github",
		IdempotencyKey:   "adhoc-start",
		WorkflowKey:      "github:issues:triage",
		Target:           testWorkflowPluginTarget("github", "issues.triage", nil),
	}

	_, err := manager.StartRun(context.Background(), caller, req)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("StartRun error = %v, want invalid invocation", err)
	}
	if len(provider.runs) != 0 || len(provider.refs) != 0 {
		t.Fatalf("provider state runs=%d refs=%d, want none", len(provider.runs), len(provider.refs))
	}
}

func TestRunMatchesExecutionRefAcceptsTargetDigestOnly(t *testing.T) {
	t.Parallel()

	target := workflowManagerStepsPluginTarget()
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	ref := &coreworkflow.ExecutionReference{
		ID:           "workflow_run:run-1",
		ProviderName: "local",
		Target:       target,
		TargetDigest: targetDigest,
	}
	run := &coreworkflow.Run{
		ID:           "run-1",
		ExecutionRef: "workflow_run:run-1",
		TargetDigest: targetDigest,
	}
	if !runMatchesExecutionRef("local", run, ref) {
		t.Fatalf("runMatchesExecutionRef returned false for target-digest-only run")
	}
}

func TestApplyDefinitionRejectsDeniedExecutionRefPermissionsBeforeStore(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: "system:http_binding:github:event",
	})
	req := DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: testWorkflowAgentTarget(coreworkflow.AgentTurn{
				ProviderName: "simple",
				Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
				ToolRefs: []coreagent.ToolRef{
					{Plugin: "github", Operation: "bot.admin"},
				},
			}),
		},
	}

	if _, err := manager.ApplyDefinition(context.Background(), caller, req); err != nil {
		t.Fatalf("ApplyDefinition(unrestricted): %v", err)
	}
	denyAll := principal.Canonicalize(&principal.Principal{
		SubjectID:        caller.SubjectID,
		TokenPermissions: principal.PermissionSet{},
	})
	_, err := manager.ApplyDefinition(context.Background(), denyAll, req)
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ApplyDefinition(deny-all) error = %v, want authorization denied from unauthorized target", err)
	}
	if len(provider.definitions) != 1 {
		t.Fatalf("definitions = %d, want first successful definition only", len(provider.definitions))
	}
}

func TestExecutionRefPermissionsScopeDistinguishesNilAndEmpty(t *testing.T) {
	t.Parallel()

	if executionRefPermissionsScope(nil) == executionRefPermissionsScope([]core.AccessPermission{}) {
		t.Fatal("nil and empty execution ref permissions produced the same scope")
	}
}

func TestApplyDefinitionExecutionRefDoesNotInheritSurfaceInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Surface: "graphql"},
			},
		},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: testWorkflowAgentTarget(coreworkflow.AgentTurn{
				ProviderName: "simple",
				Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
				ToolRefs: []coreagent.ToolRef{
					{Plugin: "github", Operation: "bot.createPullRequest"},
				},
			}),
		},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ApplyDefinition error = %v, want authorization denied from unauthorized target", err)
	}
}

func TestApplyDefinitionRejectsUnauthorizedAgentProvider(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: testWorkflowAgentTarget(coreworkflow.AgentTurn{
				ProviderName: "simple",
				Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
			}),
		},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ApplyDefinition error = %v, want authorization denied from unauthorized agent provider", err)
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestApplyDefinitionRejectsRuntimeDeniedAgentProvider(t *testing.T) {
	t.Parallel()

	authz, err := authorization.New(authorization.StaticConfig{
		Policies: map[string]authorization.StaticSubjectPolicy{
			"agent_policy": {Default: "deny"},
		},
		ProviderPolicies: map[string]string{"simple": "agent_policy"},
	})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		Authorizer:   authz,
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	_, err = manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
		ProviderName:     "local",
		CallerPluginName: "github",
		Spec: coreworkflow.DefinitionSpec{
			Target: testWorkflowAgentTarget(coreworkflow.AgentTurn{
				ProviderName: "simple",
				Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
			}),
		},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ApplyDefinition error = %v, want authorization denied from runtime-denied agent provider", err)
	}
	if len(provider.definitions) != 0 {
		t.Fatalf("definitions = %d, want 0", len(provider.definitions))
	}
}

func TestSignalRunUsesCurrentPrincipalForTargetValidation(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	target := testWorkflowAgentTarget(coreworkflow.AgentTurn{
		ProviderName: "simple",
		Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "github", Operation: "bot.openPullRequest"},
		},
	})
	ref := &coreworkflow.ExecutionReference{
		ID:           "workflow_run:stale-permissions",
		ProviderName: "local",
		Target:       target,
		SubjectID:    "system:http_binding:github:event",
		Permissions: []core.AccessPermission{{
			Plugin:     "github",
			Operations: []string{"events.handle"},
		}, {
			Plugin: "simple",
		}},
	}
	provider.refs[ref.ID] = ref
	provider.runs["run-stale-permissions"] = &coreworkflow.Run{
		ID:           "run-stale-permissions",
		Status:       coreworkflow.RunStatusRunning,
		WorkflowKey:  "github:99:acme/widgets:7",
		Target:       target,
		ExecutionRef: ref.ID,
	}
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "github",
		Operations: []string{"events.handle", "bot.openPullRequest"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionPlugins(callerPermissions),
	})

	managed, err := manager.SignalRun(context.Background(), caller, RunSignal{
		RunID:  "run-stale-permissions",
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalRun: %v", err)
	}
	if managed == nil || managed.Run == nil || managed.Run.ID != "run-stale-permissions" {
		t.Fatalf("managed signal = %#v, want stale run", managed)
	}
}

func TestCreateScheduleIdempotencyKeyIsScopedByCallerPlugin(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: "user:user-123",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	})
	base := ScheduleUpsert{
		ProviderName:   "local",
		Cron:           "*/5 * * * *",
		Timezone:       "UTC",
		Target:         testWorkflowAgentTarget(coreworkflow.AgentTurn{ProviderName: "simple", Prompt: coreworkflow.Text{Template: "Sync roadmap."}}),
		IdempotencyKey: "same-operation-key",
	}

	firstReq := base
	firstReq.CallerPluginName = "github"
	first, err := manager.CreateSchedule(context.Background(), caller, firstReq)
	if err != nil {
		t.Fatalf("CreateSchedule first caller: %v", err)
	}
	replayed, err := manager.CreateSchedule(context.Background(), caller, firstReq)
	if err != nil {
		t.Fatalf("CreateSchedule replay: %v", err)
	}
	if replayed.Schedule.ID != first.Schedule.ID {
		t.Fatalf("replayed schedule id = %q, want %q", replayed.Schedule.ID, first.Schedule.ID)
	}

	secondReq := base
	secondReq.CallerPluginName = "linear"
	second, err := manager.CreateSchedule(context.Background(), caller, secondReq)
	if err != nil {
		t.Fatalf("CreateSchedule second caller: %v", err)
	}
	if second.Schedule.ID == first.Schedule.ID {
		t.Fatalf("second caller schedule id = %q, want a distinct id", second.Schedule.ID)
	}

	conflictingReq := firstReq
	conflictingReq.Cron = "*/10 * * * *"
	_, err = manager.CreateSchedule(context.Background(), caller, conflictingReq)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("conflicting same-caller replay error = %v, want invalid invocation", err)
	}
	if len(provider.upsertedSchedules) != 2 {
		t.Fatalf("provider upserted schedules = %d, want 2", len(provider.upsertedSchedules))
	}
}

type testWorkflowControl struct {
	provider coreworkflow.Provider
}

func (c testWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	return c.provider, nil
}

func (c testWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "local"
	}
	return name, c.provider, nil
}

func (c testWorkflowControl) ProviderNames() []string {
	return []string{"local"}
}

type namedTestWorkflowControl struct {
	defaultName string
	names       []string
	providers   map[string]coreworkflow.Provider
}

func (c namedTestWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	provider := c.providers[strings.TrimSpace(name)]
	if provider == nil {
		return nil, core.ErrNotFound
	}
	return provider, nil
}

func (c namedTestWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.defaultName
	}
	provider, err := c.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (c namedTestWorkflowControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type testAgentControl struct{}

func (testAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "simple"
	}
	return name, nil, nil
}

type testAgentManager struct {
	agentmanager.Service
}

type testWorkflowProvider struct {
	coreworkflow.Provider
	refs                            map[string]*coreworkflow.ExecutionReference
	runs                            map[string]*coreworkflow.Run
	definitions                     map[string]*coreworkflow.Definition
	schedules                       map[string]*coreworkflow.Schedule
	eventTriggers                   map[string]*coreworkflow.EventTrigger
	startRunRequests                []coreworkflow.StartRunRequest
	signalOrStartRequests           []coreworkflow.SignalOrStartRunRequest
	startRunHook                    func(coreworkflow.StartRunRequest) (*coreworkflow.Run, error)
	listRunsHook                    func(coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error)
	upsertedSchedules               []coreworkflow.UpsertScheduleRequest
	upsertedEventTriggers           []coreworkflow.UpsertEventTriggerRequest
	signalOrStartErr                error
	signalOrStartCalls              int
	publishedEvents                 []coreworkflow.PublishEventRequest
	getMissingExecutionReferenceErr error
	putExecutionReferenceHook       func(*coreworkflow.ExecutionReference)
	putExecutionReferenceCalls      int
	applyWorkflowDefinitionErr      error
	applyWorkflowDefinitionHook     func(coreworkflow.ApplyDefinitionRequest) (*coreworkflow.Definition, error)
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		refs:          map[string]*coreworkflow.ExecutionReference{},
		runs:          map[string]*coreworkflow.Run{},
		definitions:   map[string]*coreworkflow.Definition{},
		schedules:     map[string]*coreworkflow.Schedule{},
		eventTriggers: map[string]*coreworkflow.EventTrigger{},
	}
}

func workflowTestAccessPermissionHasAction(permissions []core.AccessPermission, plugin, action string) bool {
	for i := range permissions {
		if strings.TrimSpace(permissions[i].Plugin) != strings.TrimSpace(plugin) {
			continue
		}
		for _, candidate := range permissions[i].Actions {
			if strings.TrimSpace(candidate) == strings.TrimSpace(action) {
				return true
			}
		}
	}
	return false
}

func (p *testWorkflowProvider) StartRun(_ context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	p.startRunRequests = append(p.startRunRequests, req)
	if p.startRunHook != nil {
		return p.startRunHook(req)
	}
	run := &coreworkflow.Run{
		ID:           "run-started",
		Status:       coreworkflow.RunStatusRunning,
		WorkflowKey:  req.WorkflowKey,
		Target:       req.Target,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.CreatedBy,
	}
	p.runs[run.ID] = run
	copied := *run
	return &copied, nil
}

func (p *testWorkflowProvider) SignalOrStartRun(_ context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	p.signalOrStartCalls++
	p.signalOrStartRequests = append(p.signalOrStartRequests, req)
	if p.signalOrStartErr != nil {
		return nil, p.signalOrStartErr
	}
	run := &coreworkflow.Run{
		ID:           "run-signaled",
		Status:       coreworkflow.RunStatusRunning,
		WorkflowKey:  req.WorkflowKey,
		Target:       req.Target,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.CreatedBy,
	}
	p.runs[run.ID] = run
	signal := req.Signal
	if strings.TrimSpace(signal.ID) == "" {
		signal.ID = "signal-1"
	}
	return &coreworkflow.SignalRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  true,
		WorkflowKey: req.WorkflowKey,
	}, nil
}

func (p *testWorkflowProvider) GetRun(_ context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	run := p.runs[strings.TrimSpace(req.RunID)]
	if run == nil {
		return nil, core.ErrNotFound
	}
	copied := *run
	return &copied, nil
}

func (p *testWorkflowProvider) ListRuns(_ context.Context, req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	if p.listRunsHook != nil {
		return p.listRunsHook(req)
	}
	ids := make([]string, 0, len(p.runs))
	for id := range p.runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*coreworkflow.Run, 0, len(ids))
	for _, id := range ids {
		run := p.runs[id]
		if run == nil {
			continue
		}
		if req.TargetPlugin != "" && !workflowTargetHasPlugin(run.Target, req.TargetPlugin) {
			continue
		}
		if req.Status != "" && run.Status != req.Status {
			continue
		}
		copied := *run
		out = append(out, &copied)
	}
	return &coreworkflow.ListRunsResponse{Runs: out}, nil
}

func (p *testWorkflowProvider) SignalRun(_ context.Context, req coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	run := p.runs[strings.TrimSpace(req.RunID)]
	if run == nil {
		return nil, core.ErrNotFound
	}
	copiedRun := *run
	signal := req.Signal
	if strings.TrimSpace(signal.ID) == "" {
		signal.ID = "signal-1"
	}
	return &coreworkflow.SignalRunResponse{
		Run:         &copiedRun,
		Signal:      signal,
		WorkflowKey: copiedRun.WorkflowKey,
	}, nil
}

func (p *testWorkflowProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	schedule := &coreworkflow.Schedule{
		ID:           strings.TrimSpace(req.ScheduleID),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: strings.TrimSpace(req.ExecutionRef),
		CreatedBy:    req.RequestedBy,
	}
	p.schedules[schedule.ID] = schedule
	copied := *schedule
	return &copied, nil
}

func (p *testWorkflowProvider) GetSchedule(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	schedule := p.schedules[strings.TrimSpace(req.ScheduleID)]
	if schedule == nil {
		return nil, core.ErrNotFound
	}
	copied := *schedule
	return &copied, nil
}

func (p *testWorkflowProvider) UpsertEventTrigger(_ context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, req)
	trigger := &coreworkflow.EventTrigger{
		ID:           strings.TrimSpace(req.TriggerID),
		Match:        req.Match,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: strings.TrimSpace(req.ExecutionRef),
		CreatedBy:    req.RequestedBy,
	}
	p.eventTriggers[trigger.ID] = trigger
	copied := *trigger
	return &copied, nil
}

func (p *testWorkflowProvider) GetEventTrigger(_ context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	trigger := p.eventTriggers[strings.TrimSpace(req.TriggerID)]
	if trigger == nil {
		return nil, core.ErrNotFound
	}
	copied := *trigger
	return &copied, nil
}

func (p *testWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	p.publishedEvents = append(p.publishedEvents, req)
	return nil
}

func (p *testWorkflowProvider) ApplyWorkflowDefinition(_ context.Context, req coreworkflow.ApplyDefinitionRequest) (*coreworkflow.Definition, error) {
	if p.applyWorkflowDefinitionHook != nil {
		return p.applyWorkflowDefinitionHook(req)
	}
	if p.applyWorkflowDefinitionErr != nil {
		return nil, p.applyWorkflowDefinitionErr
	}
	var storedRef *coreworkflow.ExecutionReference
	if req.ExecutionRef != nil {
		copiedRef := *req.ExecutionRef
		if copiedRef.CreatedAt == nil {
			now := time.Now()
			copiedRef.CreatedAt = &now
		}
		appliedDefinitionID := definitionIDFromExecutionRefID(copiedRef.ID)
		for id, existing := range p.refs {
			if existing == nil || id == copiedRef.ID {
				continue
			}
			if appliedDefinitionID != "" && definitionIDFromExecutionRefID(id) == appliedDefinitionID {
				now := time.Now()
				existing.RevokedAt = &now
			}
		}
		p.refs[copiedRef.ID] = &copiedRef
		storedRef = &copiedRef
	}
	deployment := &coreworkflow.Definition{
		Spec:               req.Spec,
		Status:             coreworkflow.DefinitionStatusActive,
		AppliedGeneration:  req.Spec.Generation,
		SpecDigest:         "",
		TargetDigest:       "",
		ActionTableDigest:  "",
		ProviderPlanID:     "",
		ProviderPlanDigest: "",
		Binding:            req.Binding,
	}
	if req.Spec.Paused {
		deployment.Status = coreworkflow.DefinitionStatusPaused
	}
	if req.Binding != nil {
		deployment.SpecDigest = req.Binding.SpecDigest
		deployment.TargetDigest = req.Binding.TargetDigest
		deployment.ActionTableDigest = req.Binding.ActionTableDigest
	}
	p.definitions[strings.TrimSpace(req.Spec.ID)] = deployment
	if schedule := workflowScheduleFromDefinition(deployment, storedRef); schedule != nil {
		p.upsertedSchedules = append(p.upsertedSchedules, coreworkflow.UpsertScheduleRequest{
			ScheduleID:        schedule.ID,
			Cron:              schedule.Cron,
			Timezone:          schedule.Timezone,
			Target:            schedule.Target,
			Paused:            schedule.Paused,
			ExecutionRef:      schedule.ExecutionRef,
			DefinitionBinding: req.Binding,
		})
		p.schedules[schedule.ID] = schedule
	}
	if trigger := workflowEventTriggerFromDefinition(deployment, storedRef); trigger != nil {
		p.upsertedEventTriggers = append(p.upsertedEventTriggers, coreworkflow.UpsertEventTriggerRequest{
			TriggerID:         trigger.ID,
			Match:             trigger.Match,
			Target:            trigger.Target,
			Paused:            trigger.Paused,
			ExecutionRef:      trigger.ExecutionRef,
			DefinitionBinding: req.Binding,
		})
		p.eventTriggers[trigger.ID] = trigger
	}
	copied := *deployment
	return &copied, nil
}

func (p *testWorkflowProvider) GetWorkflowDefinition(_ context.Context, req coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	id := strings.TrimSpace(req.DefinitionID)
	deployment := p.definitions[id]
	if deployment == nil {
		if schedule := p.schedules[id]; schedule != nil {
			deployment = &coreworkflow.Definition{
				Spec: coreworkflow.DefinitionSpec{
					ID:     schedule.ID,
					Target: schedule.Target,
					Paused: schedule.Paused,
					Activations: []coreworkflow.Activation{{
						ID:     "schedule",
						Paused: schedule.Paused,
						Mode:   coreworkflow.ActivationModeStart,
						Schedule: &coreworkflow.ScheduleActivation{
							Cron:     schedule.Cron,
							Timezone: schedule.Timezone,
						},
					}},
				},
				Status: coreworkflow.DefinitionStatusActive,
				Binding: &coreworkflow.DefinitionBinding{
					ExecutionRef: schedule.ExecutionRef,
				},
			}
		}
	}
	if deployment == nil {
		if trigger := p.eventTriggers[id]; trigger != nil {
			deployment = &coreworkflow.Definition{
				Spec: coreworkflow.DefinitionSpec{
					ID:     trigger.ID,
					Target: trigger.Target,
					Paused: trigger.Paused,
					Activations: []coreworkflow.Activation{{
						ID:     "event",
						Paused: trigger.Paused,
						Mode:   coreworkflow.ActivationModeSignalOrStart,
						Event: &coreworkflow.EventActivation{
							Match: trigger.Match,
						},
					}},
				},
				Status: coreworkflow.DefinitionStatusActive,
				Binding: &coreworkflow.DefinitionBinding{
					ExecutionRef: trigger.ExecutionRef,
				},
			}
		}
	}
	if deployment == nil {
		return nil, core.ErrNotFound
	}
	copied := *deployment
	return &copied, nil
}

func (p *testWorkflowProvider) ListWorkflowDefinitions(context.Context, coreworkflow.ListDefinitionsRequest) (*coreworkflow.ListDefinitionsResponse, error) {
	out := &coreworkflow.ListDefinitionsResponse{}
	for _, deployment := range p.definitions {
		copied := *deployment
		out.Definitions = append(out.Definitions, &copied)
	}
	return out, nil
}

func (p *testWorkflowProvider) DeleteWorkflowDefinition(_ context.Context, req coreworkflow.DeleteDefinitionRequest) error {
	id := strings.TrimSpace(req.DefinitionID)
	delete(p.definitions, id)
	delete(p.schedules, id)
	delete(p.eventTriggers, id)
	return nil
}

func (p *testWorkflowProvider) SetWorkflowDefinitionPaused(ctx context.Context, req coreworkflow.SetDefinitionPausedRequest) (*coreworkflow.Definition, error) {
	deployment, err := p.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: req.DefinitionID})
	if err != nil {
		return nil, err
	}
	deployment.Spec.Paused = req.Paused
	if req.Paused {
		deployment.Status = coreworkflow.DefinitionStatusPaused
	} else {
		deployment.Status = coreworkflow.DefinitionStatusActive
	}
	p.definitions[strings.TrimSpace(req.DefinitionID)] = deployment
	return deployment, nil
}

func (p *testWorkflowProvider) SetWorkflowActivationPaused(ctx context.Context, req coreworkflow.SetActivationPausedRequest) (*coreworkflow.Definition, error) {
	deployment, err := p.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: req.DefinitionID})
	if err != nil {
		return nil, err
	}
	for i := range deployment.Spec.Activations {
		if strings.TrimSpace(deployment.Spec.Activations[i].ID) == strings.TrimSpace(req.ActivationID) {
			deployment.Spec.Activations[i].Paused = req.Paused
		}
	}
	p.definitions[strings.TrimSpace(req.DefinitionID)] = deployment
	return deployment, nil
}

func (p *testWorkflowProvider) DeliverWorkflowEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.DeliverEventResponse, error) {
	p.publishedEvents = append(p.publishedEvents, req)
	return &coreworkflow.DeliverEventResponse{}, nil
}

func (p *testWorkflowProvider) GetWorkflowRunEvents(context.Context, coreworkflow.GetRunEventsRequest) (*coreworkflow.ListRunEventsResponse, error) {
	return &coreworkflow.ListRunEventsResponse{}, nil
}

func (p *testWorkflowProvider) GetWorkflowRunOutput(context.Context, coreworkflow.GetRunOutputRequest) (*coreworkflow.RunOutput, error) {
	return nil, core.ErrNotFound
}

func (p *testWorkflowProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	p.putExecutionReferenceCalls++
	copied := *ref
	p.refs[copied.ID] = &copied
	if p.putExecutionReferenceHook != nil {
		p.putExecutionReferenceHook(&copied)
	}
	return &copied, nil
}

func (p *testWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	ref := p.refs[strings.TrimSpace(id)]
	if ref == nil {
		if p.getMissingExecutionReferenceErr != nil {
			return nil, p.getMissingExecutionReferenceErr
		}
		return nil, core.ErrNotFound
	}
	copied := *ref
	return &copied, nil
}

func (p *testWorkflowProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	out := []*coreworkflow.ExecutionReference{}
	for _, ref := range p.refs {
		if strings.TrimSpace(ref.SubjectID) != strings.TrimSpace(subjectID) {
			continue
		}
		copied := *ref
		out = append(out, &copied)
	}
	return out, nil
}

func workflowManagerRunIDs(runs []*ManagedRun) []string {
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		if run == nil || run.Run == nil {
			continue
		}
		out = append(out, run.Run.ID)
	}
	return out
}
