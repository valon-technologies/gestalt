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
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	gproto "google.golang.org/protobuf/proto"
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

func testWorkflowAppStepTarget(appName, operation string, input map[string]any, credentialMode ...core.ConnectionMode) coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:  "run",
		App: testWorkflowAppCall(appName, operation, input, credentialMode...),
	}}}
}

func testWorkflowAppCall(appName, operation string, input map[string]any, credentialMode ...core.ConnectionMode) *coreworkflow.AppCall {
	call := &coreworkflow.AppCall{
		Name:      appName,
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

func testWorkflowAgentStepTarget(agent coreworkflow.AgentTurn) coreworkflow.Target {
	return coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:    "run",
		Agent: &agent,
	}}}
}

func testWorkflowAgentTextOutput() coreagent.Output {
	return coreagent.Output{Text: &coreagent.TextOutput{}}
}

func requireWorkflowAppStep(t *testing.T, target coreworkflow.Target, stepIndex int) *coreworkflow.AppCall {
	t.Helper()
	if len(target.Steps) <= stepIndex || target.Steps[stepIndex].App == nil {
		t.Fatalf("target steps = %#v, want app step at index %d", target.Steps, stepIndex)
	}
	return target.Steps[stepIndex].App
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:   "local",
		CallerAppName:  "github",
		IdempotencyKey: "triage-definition",
		Target:         testWorkflowAppStepTarget("github", "issues.triage", map[string]any{"mode": "full"}),
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	if definition == nil || definition.Definition == nil || strings.TrimSpace(definition.Definition.ID) == "" {
		t.Fatalf("definition = %#v, want workflow definition", definition)
	}

	run, err := manager.StartRun(context.Background(), caller, RunStart{
		ProviderName:  "local",
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		WorkflowKey:   "github:issues:triage",
	})
	if err != nil {
		t.Fatalf("StartRun by definition: %v", err)
	}
	if run == nil || run.Run == nil {
		t.Fatalf("run = %#v, want app step target", run)
	}
	runApp := requireWorkflowAppStep(t, run.Run.Target, 0)
	if got := runApp.Operation; got != "issues.triage" {
		t.Fatalf("run target operation = %q, want issues.triage", got)
	}
	if got := runApp.Input.Object["mode"].Literal; got != "full" {
		t.Fatalf("run target input mode = %v, want full", got)
	}
}

func TestPublishEventPreservesCallerAppName(t *testing.T) {
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
		AppName:      " github ",
		Event:        coreworkflow.Event{Type: "issue.created", Source: "slack"},
	}); err != nil {
		t.Fatalf("PublishEvent selected provider: %v", err)
	}
	if _, err := manager.PublishEvent(context.Background(), caller, EventPublish{
		AppName: " github ",
		Event:   coreworkflow.Event{Type: "issue.updated"},
	}); err != nil {
		t.Fatalf("PublishEvent fan-out: %v", err)
	}
	if len(provider.publishedEvents) != 2 {
		t.Fatalf("published events = %d, want 2", len(provider.publishedEvents))
	}
	for i, req := range provider.publishedEvents {
		if req.GetAppName() != "github" {
			t.Fatalf("publishedEvents[%d].AppName = %q, want github", i, req.GetAppName())
		}
		if req.GetEvent().GetSource() != "github" {
			t.Fatalf("publishedEvents[%d].Event.Source = %q, want github", i, req.GetEvent().GetSource())
		}
	}

	if _, err := manager.PublishEvent(context.Background(), caller, EventPublish{
		Event: coreworkflow.Event{Type: "issue.deleted"},
	}); !errors.Is(err, ErrWorkflowEventSourceRequired) {
		t.Fatalf("PublishEvent without caller app error = %v, want ErrWorkflowEventSourceRequired", err)
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.triage", map[string]any{"mode": "full"}),
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	schedule, err := manager.CreateSchedule(context.Background(), caller, ScheduleUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		Cron:          "*/5 * * * *",
		Timezone:      "UTC",
	})
	if err != nil {
		t.Fatalf("CreateSchedule by definition: %v", err)
	}
	if schedule == nil || schedule.Schedule == nil {
		t.Fatalf("schedule = %#v, want app step target", schedule)
	}
	if got := requireWorkflowAppStep(t, schedule.Schedule.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("schedule target operation = %q, want issues.triage", got)
	}
	if got := provider.upsertedSchedules[len(provider.upsertedSchedules)-1].GetDefinitionId(); got != definition.Definition.ID {
		t.Fatalf("schedule definition id = %q, want %q", got, definition.Definition.ID)
	}

	trigger, err := manager.CreateEventTrigger(context.Background(), caller, EventTriggerUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		Match:         coreworkflow.EventMatch{Type: "github.issue.opened"},
	})
	if err != nil {
		t.Fatalf("CreateEventTrigger by definition: %v", err)
	}
	if trigger == nil || trigger.Trigger == nil {
		t.Fatalf("trigger = %#v, want app step target", trigger)
	}
	if got := requireWorkflowAppStep(t, trigger.Trigger.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("trigger target operation = %q, want issues.triage", got)
	}
	if got := provider.upsertedEventTriggers[len(provider.upsertedEventTriggers)-1].GetDefinitionId(); got != definition.Definition.ID {
		t.Fatalf("trigger definition id = %q, want %q", got, definition.Definition.ID)
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
		App:        "github",
		Operations: []string{"issues.triage", "issues.updated"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.triage", map[string]any{"mode": "initial"}),
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	scheduleReq := ScheduleUpsert{
		ProviderName:   "local",
		CallerAppName:  "github",
		DefinitionID:   definition.Definition.ID,
		IdempotencyKey: "triage-schedule",
		Cron:           "*/5 * * * *",
		Timezone:       "UTC",
	}
	schedule, err := manager.CreateSchedule(context.Background(), caller, scheduleReq)
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	if _, err := manager.UpdateDefinition(context.Background(), caller, definition.Definition.ID, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.updated", map[string]any{"mode": "updated"}),
	}); err != nil {
		t.Fatalf("UpdateDefinition: %v", err)
	}

	replayedSchedule, err := manager.CreateSchedule(context.Background(), caller, scheduleReq)
	if err != nil {
		t.Fatalf("CreateSchedule replay after definition update: %v", err)
	}
	if replayedSchedule.Schedule.ID != schedule.Schedule.ID {
		t.Fatalf("replayed schedule ID = %q, want %q", replayedSchedule.Schedule.ID, schedule.Schedule.ID)
	}
	if got := requireWorkflowAppStep(t, replayedSchedule.Schedule.Target, 0).Operation; got != "issues.triage" {
		t.Fatalf("replayed schedule target operation = %q, want original snapshot", got)
	}
	if len(provider.upsertedSchedules) != 1 {
		t.Fatalf("provider upserted schedules = %d, want 1", len(provider.upsertedSchedules))
	}

	otherDefinition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.updated", map[string]any{"mode": "other"}),
	})
	if err != nil {
		t.Fatalf("CreateDefinition(other): %v", err)
	}
	otherScheduleReq := scheduleReq
	otherScheduleReq.DefinitionID = otherDefinition.Definition.ID
	_, err = manager.CreateSchedule(context.Background(), caller, otherScheduleReq)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("CreateSchedule replay with different definition error = %v, want invalid invocation", err)
	}

	triggerReq := EventTriggerUpsert{
		ProviderName:   "local",
		CallerAppName:  "github",
		DefinitionID:   definition.Definition.ID,
		IdempotencyKey: "triage-trigger",
		Match:          coreworkflow.EventMatch{Type: "github.issue.opened"},
	}
	trigger, err := manager.CreateEventTrigger(context.Background(), caller, triggerReq)
	if err != nil {
		t.Fatalf("CreateEventTrigger: %v", err)
	}
	otherTriggerReq := triggerReq
	otherTriggerReq.DefinitionID = otherDefinition.Definition.ID
	_, err = manager.CreateEventTrigger(context.Background(), caller, otherTriggerReq)
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("CreateEventTrigger replay with different definition error = %v, want invalid invocation", err)
	}

	if err := manager.DeleteDefinition(context.Background(), caller, definition.Definition.ID); err != nil {
		t.Fatalf("DeleteDefinition: %v", err)
	}
	replayedTrigger, err := manager.CreateEventTrigger(context.Background(), caller, triggerReq)
	if err != nil {
		t.Fatalf("CreateEventTrigger replay after definition delete: %v", err)
	}
	if replayedTrigger.Trigger.ID != trigger.Trigger.ID {
		t.Fatalf("replayed trigger ID = %q, want %q", replayedTrigger.Trigger.ID, trigger.Trigger.ID)
	}
	if got := requireWorkflowAppStep(t, replayedTrigger.Trigger.Target, 0).Operation; got != "issues.updated" {
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "remote",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.triage", nil),
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	run, err := manager.StartRun(context.Background(), caller, RunStart{
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		WorkflowKey:   "github:issues:triage",
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
		ProviderName:  "local",
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		WorkflowKey:   "github:issues:triage:local",
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("StartRun with mismatched provider error = %v, want not found", err)
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})
	target := testWorkflowAppStepTarget("github", "issues.triage", nil)
	createdBy := principal.UserSubjectID("ada")
	for _, id := range []string{"1", "2", "3"} {
		provider.runs["run-"+id] = &coreworkflow.Run{
			ID:                 "run-" + id,
			Status:             coreworkflow.RunStatusRunning,
			Target:             target,
			CreatedBySubjectID: createdBy,
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

func TestListRunsOrdersCandidatesAcrossProvidersNewestFirst(t *testing.T) {
	t.Parallel()

	localProvider := newTestWorkflowProvider()
	remoteProvider := newTestWorkflowProvider()
	target := testWorkflowAppStepTarget("github", "issues.triage", nil)
	createdBy := principal.UserSubjectID("ada")
	addRun := func(provider *testWorkflowProvider, runID string, createdAt time.Time) {
		provider.runs[runID] = &coreworkflow.Run{
			ID:                 runID,
			Status:             coreworkflow.RunStatusRunning,
			Target:             target,
			CreatedBySubjectID: createdBy,
			CreatedAt:          &createdAt,
		}
	}
	addRun(localProvider, "run-old", time.Unix(100, 0).UTC())
	addRun(localProvider, "run-mid", time.Unix(200, 0).UTC())
	addRun(remoteProvider, "run-new", time.Unix(300, 0).UTC())
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
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

func TestListRunsFiltersTargetAppInManager(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	githubTarget := testWorkflowAppStepTarget("github", "issues.triage", nil)
	slackTarget := testWorkflowAppStepTarget("slack", "chat.postMessage", nil)
	multiTarget := coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			ID:  "triage",
			App: testWorkflowAppCall("github", "issues.triage", nil),
		},
		{
			ID:  "notify",
			App: testWorkflowAppCall("slack", "chat.postMessage", nil),
		},
	}}
	provider.listRunsHook = func(req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		if req.TargetApp != "slack" {
			t.Fatalf("provider TargetApp = %q, want slack", req.TargetApp)
		}
		return &coreworkflow.ListRunsResponse{Runs: []*coreworkflow.Run{
			{
				ID:                 "run-github",
				Status:             coreworkflow.RunStatusRunning,
				Target:             githubTarget,
				CreatedBySubjectID: principal.UserSubjectID("ada"),
			},
			{
				ID:                 "run-slack",
				Status:             coreworkflow.RunStatusRunning,
				Target:             slackTarget,
				CreatedBySubjectID: principal.UserSubjectID("ada"),
			},
			{
				ID:                 "run-multi",
				Status:             coreworkflow.RunStatusRunning,
				Target:             multiTarget,
				CreatedBySubjectID: principal.UserSubjectID("ada"),
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
		{App: "github", Operations: []string{"issues.triage"}},
		{App: "slack", Operations: []string{"chat.postMessage"}},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	resp, err := manager.ListRuns(context.Background(), caller, coreworkflow.ListRunsRequest{TargetApp: "slack"})
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
	target := testWorkflowAppStepTarget("github", "issues.triage", nil)
	oldCreatedAt := time.Unix(100, 0).UTC()
	newCreatedAt := time.Unix(200, 0).UTC()
	provider.listRunsHook = func(coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		return &coreworkflow.ListRunsResponse{Runs: []*coreworkflow.Run{
			{
				ID:                 "run-old",
				Status:             coreworkflow.RunStatusRunning,
				Target:             target,
				CreatedBySubjectID: principal.UserSubjectID("ada"),
				CreatedAt:          &oldCreatedAt,
			},
			{
				ID:                 "run-new",
				Status:             coreworkflow.RunStatusRunning,
				Target:             target,
				CreatedBySubjectID: principal.UserSubjectID("ada"),
				CreatedAt:          &newCreatedAt,
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
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
		PageSize:  10,
		TargetApp: "github",
		Status:    coreworkflow.RunStatusRunning,
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
		PageSize:  10,
		TargetApp: "slack",
		Status:    coreworkflow.RunStatusRunning,
	}, pageSize); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with changed filter error = %v, want invalid invocation", err)
	}

	if _, err := decodeWorkflowRunListPageToken(token, []string{"remote", "local"}, req, pageSize); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with reordered providers error = %v, want invalid invocation", err)
	}

	if _, err := decodeWorkflowRunListPageToken(token, []string{"local", "remote"}, coreworkflow.ListRunsRequest{
		PageSize:  20,
		TargetApp: "github",
		Status:    coreworkflow.RunStatusRunning,
	}, 20); !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("decode token with changed page size error = %v, want invalid invocation", err)
	}
}

func TestUpdateDefinitionRejectsProviderChange(t *testing.T) {
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
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.triage", nil),
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	_, err = manager.UpdateDefinition(context.Background(), caller, definition.Definition.ID, DefinitionUpsert{
		ProviderName:  "remote",
		CallerAppName: "github",
		Target:        testWorkflowAppStepTarget("github", "issues.triage", nil),
	})
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("UpdateDefinition provider change error = %v, want invalid invocation", err)
	}
	if localProvider.definitions[definition.Definition.ID] == nil {
		t.Fatalf("local definition missing after rejected provider change")
	}
	if remoteProvider.definitions[definition.Definition.ID] != nil {
		t.Fatalf("remote definition = %#v, want none", remoteProvider.definitions[definition.Definition.ID])
	}
}

func TestSignalOrStartRunResolvesDeclaredAppCredentialModes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "github",
				Operations: []catalog.CatalogOperation{
					{ID: "bot.commentFinal", Method: "POST"},
					{ID: "bot.commentStarted", Method: "POST"},
				},
			},
		}),
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle"},
	}, {
		App: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID: "run",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
					Output:       testWorkflowAgentTextOutput(),
					ToolRefs: []coreagent.ToolRef{
						{App: "github", Operation: "bot.commitFiles"},
						{App: "github", Operation: "bot.openPullRequest"},
					},
				},
			},
			{
				ID:  "comment_final",
				App: testWorkflowAppCall("github", "bot.commentFinal", nil, core.ConnectionModeNone),
			},
			{
				ID:  "session_ready",
				App: testWorkflowAppCall("github", "bot.commentStarted", nil, core.ConnectionModeNone),
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.Run == nil {
		t.Fatalf("managed signal = %#v, want run", managed)
	}
	if got := requireWorkflowAppStep(t, managed.Run.Target, 1).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("final comment app credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := requireWorkflowAppStep(t, managed.Run.Target, 2).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("session ready app credential mode = %q, want %q", got, core.ConnectionModeNone)
	}

	if _, err := manager.GetRun(context.Background(), caller, managed.Run.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetRun without caller app = %v, want not found", err)
	}
	callerCtx := WithCallerAppName(context.Background(), "github")
	stored, err := manager.GetRun(callerCtx, caller, managed.Run.ID)
	if err != nil {
		t.Fatalf("GetRun with caller app: %v", err)
	}
	if stored == nil || stored.Run == nil || stored.Run.ID != managed.Run.ID {
		t.Fatalf("stored run = %#v, want %q", stored, managed.Run.ID)
	}
	listed, err := manager.ListRuns(callerCtx, caller, coreworkflow.ListRunsRequest{})
	if err != nil {
		t.Fatalf("ListRuns with caller app: %v", err)
	}
	if len(listed.Runs) != 1 || listed.Runs[0].Run == nil || listed.Runs[0].Run.ID != managed.Run.ID {
		t.Fatalf("listed runs = %#v, want %q", listed.Runs, managed.Run.ID)
	}

	definition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target:        managed.Run.Target,
	})
	if err != nil {
		t.Fatalf("CreateDefinition with declared caller app target: %v", err)
	}
	byDefinition, err := manager.StartRun(context.Background(), caller, RunStart{
		ProviderName:  "local",
		CallerAppName: "github",
		DefinitionID:  definition.Definition.ID,
		WorkflowKey:   "github:99:acme/widgets:7:definition",
	})
	if err != nil {
		t.Fatalf("StartRun by definition with declared caller app target: %v", err)
	}
	if byDefinition == nil || byDefinition.Run == nil {
		t.Fatalf("definition run = %#v, want run", byDefinition)
	}
}

func TestSignalOrStartRunRejectsStepWhenMissingEquals(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{App: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
	})

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName: "local",
		WorkflowKey:  "agent:steps:missing-equals",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID: "diagnosis",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Diagnose the alert."},
					Output:       testWorkflowAgentTextOutput(),
				},
			},
			{
				ID: "pr_fix",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Open a PR."},
					Output:       testWorkflowAgentTextOutput(),
				},
				When: &coreworkflow.StepWhen{
					Value: coreworkflow.Value{
						StepOutput: &coreworkflow.StepOutputSource{
							StepID: "diagnosis",
							Path:   "agent.output.structured.value.actionable_for_pr",
						},
					},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target.steps[1].when.equals is required") {
		t.Fatalf("SignalOrStartRun error = %v, want missing when.equals validation", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
	}
}

func TestSignalOrStartRunRejectsStepWhenMissingValue(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{App: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
	})

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName: "local",
		WorkflowKey:  "agent:steps:missing-when-value",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID: "diagnosis",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Diagnose the alert."},
					Output:       testWorkflowAgentTextOutput(),
				},
			},
			{
				ID: "pr_fix",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Open a PR."},
					Output:       testWorkflowAgentTextOutput(),
				},
				When: &coreworkflow.StepWhen{
					Equals:    nil,
					EqualsSet: true,
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target.steps[1].when.value is required") {
		t.Fatalf("SignalOrStartRun error = %v, want missing when.value validation", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
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
		{App: "github", Operations: []string{"issues.triage"}},
		{App: "slack", Operations: []string{"chat.postMessage"}},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})
	stepOutput := func(stepID, path string) coreworkflow.Value {
		return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: stepID, Path: path}}
	}
	baseTarget := func() coreworkflow.Target {
		return coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID:  "diagnosis",
				App: testWorkflowAppCall("github", "issues.triage", nil),
			},
			{
				ID:  "notify",
				App: testWorkflowAppCall("slack", "chat.postMessage", nil),
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
				target.Steps[1].Inputs = map[string]coreworkflow.Value{"summary": stepOutput("future", "app.body")}
			},
			want: `workflow target.steps[1].inputs.summary.step_output.step_id "future" must reference an earlier step`,
		},
		{
			name: "app input",
			mutate: func(target *coreworkflow.Target) {
				target.Steps[1].App.Input = coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"text": stepOutput("future", "app.body"),
				}}
			},
			want: `workflow target.steps[1].app.input.text.step_output.step_id "future" must reference an earlier step`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := baseTarget()
			tt.mutate(&target)
			_, err := manager.resolveTarget(context.Background(), caller, target, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolveTarget error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSignalOrStartRunRejectsAgentStepWithoutPromptOrMessages(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{App: "simple"}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		UserID:           "ada",
		Kind:             principal.KindUser,
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
	})

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName: "local",
		WorkflowKey:  "agent:steps:empty-agent",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "agent",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "simple",
				Output:       testWorkflowAgentTextOutput(),
			},
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow target agent prompt or messages is required") {
		t.Fatalf("SignalOrStartRun error = %v, want missing agent prompt/messages validation", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
	}
}

func TestSignalOrStartRunAppStepCredentialModeUsesDeclaredInvoke(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	invoker := &recordingWorkflowManagerInvoker{requireNone: true}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "target",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "target",
				Operations: []catalog.CatalogOperation{
					{ID: "reviews.create", Method: "POST"},
				},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
		Invoker:  invoker,
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "target",
		Operations: []string{"events.handle", "reviews.create"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:event-handler",
		Kind:             principal.Kind("service_account"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "target:99:acme/widgets:7:policy:review",
		CallerAppName: "target",
		Target:        testWorkflowAppStepTarget("target", "reviews.create", nil, core.ConnectionModeNone),
		Signal:        coreworkflow.Signal{Name: "target.event.received"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.Run == nil {
		t.Fatalf("managed signal = %#v, want run", managed)
	}
	if got := requireWorkflowAppStep(t, managed.Run.Target, 0).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("stored credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != core.ConnectionModeNone {
		t.Fatalf("resolver credential modes = %#v, want final %q", invoker.modes, core.ConnectionModeNone)
	}
}

func TestSignalOrStartRunAppStepCredentialModeKeepsBlankModeBlank(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	invoker := &recordingWorkflowManagerInvoker{}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "target",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name:       "target",
				Operations: []catalog.CatalogOperation{{ID: "reviews.create", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
		Invoker:  invoker,
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "target",
		Operations: []string{"events.handle", "reviews.create"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:event-handler",
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "target:99:acme/widgets:7:policy:review",
		CallerAppName: "target",
		Target:        testWorkflowAppStepTarget("target", "reviews.create", nil),
		Signal:        coreworkflow.Signal{Name: "target.event.received"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.Run == nil {
		t.Fatalf("managed signal = %#v, want run", managed)
	}
	if got := requireWorkflowAppStep(t, managed.Run.Target, 0).CredentialMode; got != "" {
		t.Fatalf("stored credential mode = %q, want empty", got)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != "" {
		t.Fatalf("resolver credential modes = %#v, want final empty", invoker.modes)
	}

	explicit, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "target:99:acme/widgets:7:policy:review",
		CallerAppName: "target",
		Target:        testWorkflowAppStepTarget("target", "reviews.create", nil, core.ConnectionModeNone),
		Signal:        coreworkflow.Signal{Name: "target.event.received"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun explicit mode: %v", err)
	}
	if explicit == nil || explicit.Run == nil {
		t.Fatalf("explicit managed signal = %#v, want run", explicit)
	}
	if got := requireWorkflowAppStep(t, explicit.Run.Target, 0).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("explicit credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
}

func TestCreateScheduleAcceptsExplicitAppStepCredentialMode(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N: "target",
			CatalogVal: &catalog.Catalog{
				Name:       "target",
				Operations: []catalog.CatalogOperation{{ID: "reviews.create", Method: "POST"}},
			},
		}),
		Workflow: testWorkflowControl{provider: provider},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "target",
		Operations: []string{"reviews.create"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	managed, err := manager.CreateSchedule(context.Background(), caller, ScheduleUpsert{
		ProviderName: "local",
		Cron:         "*/5 * * * *",
		Target:       testWorkflowAppStepTarget("target", "reviews.create", nil, core.ConnectionModeNone),
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if got := requireWorkflowAppStep(t, managed.Schedule.Target, 0).CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("stored credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
}

func TestSignalOrStartRunRejectsDeniedTargetPermissionsBeforeEnqueue(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: "system:http_binding:source:event",
	})
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "source:99:acme/widgets:7",
		CallerAppName: "source",
		Target: testWorkflowAgentStepTarget(coreworkflow.AgentTurn{
			ProviderName: "simple",
			Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
			Output:       testWorkflowAgentTextOutput(),
			ToolRefs: []coreagent.ToolRef{
				{App: "target", Operation: "automation.admin"},
			},
		}),
		Signal: coreworkflow.Signal{Name: "source.event.received"},
	}

	if _, err := manager.SignalOrStartRun(context.Background(), caller, req); err != nil {
		t.Fatalf("SignalOrStartRun(unrestricted): %v", err)
	}
	denyAll := principal.Canonicalize(&principal.Principal{
		SubjectID:        caller.SubjectID,
		TokenPermissions: principal.PermissionSet{},
	})
	_, err := manager.SignalOrStartRun(context.Background(), denyAll, req)
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("SignalOrStartRun(deny-all) error = %v, want authorization denied from unauthorized target", err)
	}
	if provider.signalOrStartCalls != 1 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 1", provider.signalOrStartCalls)
	}
}

func TestSignalOrStartRunRejectsAgentToolRunAsDelegationBeforeEnqueue(t *testing.T) {
	t.Parallel()

	runAs := &core.RunAsSubject{
		SubjectID:           "service_account:automation",
		CredentialSubjectID: "service_account:automation",
	}
	for _, tc := range []struct {
		name string
		ref  coreagent.ToolRef
	}{
		{
			name: "runAs",
			ref: coreagent.ToolRef{
				App:       "target",
				Operation: "automation.write",
				RunAs:     runAs,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider := newTestWorkflowProvider()
			manager := New(Config{
				Workflow:     testWorkflowControl{provider: provider},
				Agent:        testAgentControl{},
				AgentManager: testAgentManager{},
			})
			callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
				App:        "source",
				Operations: []string{"events.handle"},
			}, {
				App: "simple",
			}, {
				App:        "target",
				Operations: []string{"automation.write"},
			}})
			caller := principal.Canonicalize(&principal.Principal{
				SubjectID:        "system:http_binding:source:event",
				TokenPermissions: callerPermissions,
				Scopes:           principal.PermissionApps(callerPermissions),
			})

			_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
				ProviderName:  "local",
				WorkflowKey:   "source:99:acme/widgets:7",
				CallerAppName: "source",
				Target: testWorkflowAgentStepTarget(coreworkflow.AgentTurn{
					ProviderName: "simple",
					Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
					ToolRefs:     []coreagent.ToolRef{tc.ref},
				}),
				Signal: coreworkflow.Signal{Name: "source.event.received"},
			})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("SignalOrStartRun error = %v, want authorization denied", err)
			}
			if provider.signalOrStartCalls != 0 {
				t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
			}
		})
	}
}

func TestSignalOrStartRunRejectsUnauthorizedAgentProvider(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
	})

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: testWorkflowAgentStepTarget(coreworkflow.AgentTurn{
			ProviderName: "simple",
			Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
			Output:       testWorkflowAgentTextOutput(),
		}),
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("SignalOrStartRun error = %v, want authorization denied from unauthorized agent provider", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
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
	target := testWorkflowAgentStepTarget(coreworkflow.AgentTurn{
		ProviderName: "simple",
		Prompt:       coreworkflow.Text{Template: "Handle the webhook."},
		Output:       testWorkflowAgentTextOutput(),
		ToolRefs: []coreagent.ToolRef{
			{App: "github", Operation: "bot.openPullRequest"},
		},
	})
	provider.runs["run-stale-permissions"] = &coreworkflow.Run{
		ID:                 "run-stale-permissions",
		Status:             coreworkflow.RunStatusRunning,
		WorkflowKey:        "github:99:acme/widgets:7",
		Target:             target,
		CreatedBySubjectID: "system:http_binding:github:event",
	}
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle", "bot.openPullRequest"},
	}, {
		App: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: callerPermissions,
		Scopes:           principal.PermissionApps(callerPermissions),
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

func TestCreateScheduleIdempotencyKeyIsScopedByCallerApp(t *testing.T) {
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
		Target:         testWorkflowAgentStepTarget(coreworkflow.AgentTurn{ProviderName: "simple", Prompt: coreworkflow.Text{Template: "Sync roadmap."}, Output: testWorkflowAgentTextOutput()}),
		IdempotencyKey: "same-operation-key",
	}

	firstReq := base
	firstReq.CallerAppName = "github"
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
	secondReq.CallerAppName = "linear"
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

func (c testWorkflowControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
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

func (c namedTestWorkflowControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.defaultName
	}
	provider := c.providers[name]
	if provider == nil {
		return "", nil, core.ErrNotFound
	}
	return name, provider, nil
}

func (c namedTestWorkflowControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type testAgentControl struct{}

func (testAgentControl) ResolveProvider(_ context.Context, name string) (string, coreagent.Provider, error) {
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
	definitions           map[string]*coreworkflow.Definition
	runs                  map[string]*coreworkflow.Run
	schedules             map[string]*coreworkflow.Schedule
	eventTriggers         map[string]*coreworkflow.EventTrigger
	listRunsHook          func(coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error)
	upsertedSchedules     []*proto.UpsertWorkflowProviderScheduleRequest
	upsertedEventTriggers []*proto.UpsertWorkflowProviderEventTriggerRequest
	signalOrStartErr      error
	signalOrStartCalls    int
	publishedEvents       []*proto.PublishWorkflowProviderEventRequest
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		definitions:   map[string]*coreworkflow.Definition{},
		runs:          map[string]*coreworkflow.Run{},
		schedules:     map[string]*coreworkflow.Schedule{},
		eventTriggers: map[string]*coreworkflow.EventTrigger{},
	}
}

func (p *testWorkflowProvider) CreateDefinition(_ context.Context, req *proto.CreateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	id := strings.TrimSpace(req.GetIdempotencyKey())
	if id == "" {
		id = fmt.Sprintf("definition-%d", len(p.definitions)+1)
	}
	definition := &coreworkflow.Definition{
		ID:                 id,
		Target:             workflowwire.TargetFromProto(req.GetTarget()),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
	}
	p.definitions[definition.ID] = definition
	copied := *definition
	return workflowwire.DefinitionToProto(&copied)
}

func (p *testWorkflowProvider) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	copied := *definition
	return workflowwire.DefinitionToProto(&copied)
}

func (p *testWorkflowProvider) UpdateDefinition(_ context.Context, req *proto.UpdateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	id := strings.TrimSpace(req.GetDefinitionId())
	if id == "" {
		return nil, core.ErrNotFound
	}
	definition := &coreworkflow.Definition{
		ID:                 id,
		Target:             workflowwire.TargetFromProto(req.GetTarget()),
		CreatedBySubjectID: strings.TrimSpace(req.GetRequestedBySubjectId()),
	}
	p.definitions[id] = definition
	copied := *definition
	return workflowwire.DefinitionToProto(&copied)
}

func (p *testWorkflowProvider) DeleteDefinition(_ context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	id := strings.TrimSpace(req.GetDefinitionId())
	if p.definitions[id] == nil {
		return core.ErrNotFound
	}
	delete(p.definitions, id)
	return nil
}

func (p *testWorkflowProvider) StartRun(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	target := workflowwire.TargetFromProto(req.GetTarget())
	createdBy := strings.TrimSpace(req.GetCreatedBySubjectId())
	run := &coreworkflow.Run{ID: "run-started", Status: coreworkflow.RunStatusRunning, WorkflowKey: strings.TrimSpace(req.GetWorkflowKey()), Target: target, CreatedBySubjectID: createdBy}
	p.runs[run.ID] = run
	copied := *run
	return workflowwire.RunToProto(&copied)
}

func (p *testWorkflowProvider) SignalOrStartRun(_ context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	p.signalOrStartCalls++
	if p.signalOrStartErr != nil {
		return nil, p.signalOrStartErr
	}
	run := &coreworkflow.Run{
		ID:                 "run-signaled",
		Status:             coreworkflow.RunStatusRunning,
		WorkflowKey:        strings.TrimSpace(req.GetWorkflowKey()),
		Target:             workflowwire.TargetFromProto(req.GetTarget()),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
	}
	p.runs[run.ID] = run
	signal := workflowwire.SignalFromProto(req.GetSignal())
	if strings.TrimSpace(signal.ID) == "" {
		signal.ID = "signal-1"
	}
	return workflowwire.SignalRunResponseToProto(&coreworkflow.SignalRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	})
}

func (p *testWorkflowProvider) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	copied := *run
	return workflowwire.RunToProto(&copied)
}

func (p *testWorkflowProvider) ListRuns(_ context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	coreReq := coreworkflow.ListRunsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: strings.TrimSpace(req.GetPageToken()),
		TargetApp: strings.TrimSpace(req.GetTargetApp()),
	}
	status, err := workflowwire.RunStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, err
	}
	coreReq.Status = status
	if p.listRunsHook != nil {
		resp, err := p.listRunsHook(coreReq)
		if err != nil {
			return nil, err
		}
		out := &proto.ListWorkflowProviderRunsResponse{NextPageToken: resp.NextPageToken}
		for _, run := range resp.Runs {
			pbRun, err := workflowwire.RunToProto(run)
			if err != nil {
				return nil, err
			}
			out.Runs = append(out.Runs, pbRun)
		}
		return out, nil
	}
	ids := make([]string, 0, len(p.runs))
	for id := range p.runs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*proto.BoundWorkflowRun, 0, len(ids))
	for _, id := range ids {
		run := p.runs[id]
		if run == nil {
			continue
		}
		if coreReq.TargetApp != "" && !workflowTargetHasApp(run.Target, coreReq.TargetApp) {
			continue
		}
		if coreReq.Status != "" && run.Status != coreReq.Status {
			continue
		}
		copied := *run
		pbRun, err := workflowwire.RunToProto(&copied)
		if err != nil {
			return nil, err
		}
		out = append(out, pbRun)
	}
	return &proto.ListWorkflowProviderRunsResponse{Runs: out}, nil
}

func (p *testWorkflowProvider) SignalRun(_ context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	copiedRun := *run
	signal := workflowwire.SignalFromProto(req.GetSignal())
	if strings.TrimSpace(signal.ID) == "" {
		signal.ID = "signal-1"
	}
	return workflowwire.SignalRunResponseToProto(&coreworkflow.SignalRunResponse{
		Run:         &copiedRun,
		Signal:      signal,
		WorkflowKey: copiedRun.WorkflowKey,
	})
}

func (p *testWorkflowProvider) UpsertSchedule(_ context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	target := workflowwire.TargetFromProto(req.GetTarget())
	p.upsertedSchedules = append(p.upsertedSchedules, gproto.Clone(req).(*proto.UpsertWorkflowProviderScheduleRequest))
	schedule := &coreworkflow.Schedule{
		ID:                 strings.TrimSpace(req.GetScheduleId()),
		Cron:               strings.TrimSpace(req.GetCron()),
		Timezone:           strings.TrimSpace(req.GetTimezone()),
		Target:             target,
		DefinitionID:       strings.TrimSpace(req.GetDefinitionId()),
		Paused:             req.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(req.GetRequestedBySubjectId()),
	}
	p.schedules[schedule.ID] = schedule
	copied := *schedule
	return workflowwire.ScheduleToProto(&copied)
}

func (p *testWorkflowProvider) GetSchedule(_ context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule := p.schedules[strings.TrimSpace(req.GetScheduleId())]
	if schedule == nil {
		return nil, core.ErrNotFound
	}
	copied := *schedule
	return workflowwire.ScheduleToProto(&copied)
}

func (p *testWorkflowProvider) UpsertEventTrigger(_ context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	target := workflowwire.TargetFromProto(req.GetTarget())
	match := workflowwire.EventMatchFromProto(req.GetMatch())
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, gproto.Clone(req).(*proto.UpsertWorkflowProviderEventTriggerRequest))
	trigger := &coreworkflow.EventTrigger{
		ID:                 strings.TrimSpace(req.GetTriggerId()),
		Match:              match,
		Target:             target,
		DefinitionID:       strings.TrimSpace(req.GetDefinitionId()),
		Paused:             req.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(req.GetRequestedBySubjectId()),
	}
	p.eventTriggers[trigger.ID] = trigger
	copied := *trigger
	return workflowwire.EventTriggerToProto(&copied)
}

func (p *testWorkflowProvider) GetEventTrigger(_ context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger := p.eventTriggers[strings.TrimSpace(req.GetTriggerId())]
	if trigger == nil {
		return nil, core.ErrNotFound
	}
	copied := *trigger
	return workflowwire.EventTriggerToProto(&copied)
}

func (p *testWorkflowProvider) PublishEvent(_ context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	p.publishedEvents = append(p.publishedEvents, gproto.Clone(req).(*proto.PublishWorkflowProviderEventRequest))
	return gproto.Clone(req.GetEvent()).(*proto.WorkflowEvent), nil
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
