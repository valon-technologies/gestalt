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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
			Input:     map[string]any{"mode": "full"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	if definition == nil || definition.Definition == nil || !strings.HasPrefix(definition.Definition.ID, workflowDefinitionExecutionRefBasePrefix) {
		t.Fatalf("definition = %#v, want workflow definition execution ref", definition)
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
	if run == nil || run.Run == nil || run.Run.Target.App == nil {
		t.Fatalf("run = %#v, want app target", run)
	}
	if got := run.Run.Target.App.Operation; got != "issues.triage" {
		t.Fatalf("run target operation = %q, want issues.triage", got)
	}
	if got := run.Run.Target.App.Input["mode"]; got != "full" {
		t.Fatalf("run target input mode = %v, want full", got)
	}
	if run.ExecutionRef == nil || !strings.HasPrefix(run.ExecutionRef.ID, workflowRunExecutionRefBasePrefix) {
		t.Fatalf("run execution ref = %#v, want run snapshot ref", run.ExecutionRef)
	}
	if run.ExecutionRef.ID == definition.Definition.ID {
		t.Fatalf("run execution ref reused definition id %q, want snapshot ref", run.ExecutionRef.ID)
	}
	if run.ExecutionRef.SourceDefinitionID != definition.Definition.ID {
		t.Fatalf("run source definition id = %q, want %q", run.ExecutionRef.SourceDefinitionID, definition.Definition.ID)
	}
}

func TestStartRunRejectsProviderReplayWithDifferentExecutionRef(t *testing.T) {
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
	req := RunStart{
		ProviderName:   "local",
		CallerAppName:  "github",
		IdempotencyKey: "same-idempotency-key",
		WorkflowKey:    "github:issues:triage:first",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
		}},
	}

	first, err := manager.StartRun(context.Background(), caller, req)
	if err != nil {
		t.Fatalf("StartRun(first): %v", err)
	}
	if first == nil || first.Run == nil || first.ExecutionRef == nil {
		t.Fatalf("first run = %#v, want run and execution ref", first)
	}
	firstRefID := first.ExecutionRef.ID
	provider.startRunHook = func(req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
		copied := *first.Run
		return &copied, nil
	}
	req.WorkflowKey = "github:issues:triage:second"

	_, err = manager.StartRun(context.Background(), caller, req)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("StartRun(second) error = %v, want not found from mismatched execution ref", err)
	}
	secondRefID := newRunExecutionRefID(workflowCreateIdempotencyScope(caller, req.CallerAppName, req.IdempotencyKey), req.WorkflowKey)
	if secondRefID == firstRefID {
		t.Fatalf("second execution ref ID = first ref ID %q, want workflow-key scoped ref", firstRefID)
	}
	if provider.refs[firstRefID] == nil || provider.refs[firstRefID].RevokedAt != nil {
		t.Fatalf("first execution ref = %#v, want active", provider.refs[firstRefID])
	}
	if provider.refs[secondRefID] == nil || provider.refs[secondRefID].RevokedAt == nil {
		t.Fatalf("second execution ref = %#v, want revoked after mismatch", provider.refs[secondRefID])
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
		Event:        coreworkflow.Event{Type: "issue.created"},
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
		if req.AppName != "github" {
			t.Fatalf("publishedEvents[%d].AppName = %q, want github", i, req.AppName)
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
			Input:     map[string]any{"mode": "full"},
		}},
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
	if schedule == nil || schedule.Schedule == nil || schedule.Schedule.Target.App == nil {
		t.Fatalf("schedule = %#v, want app target", schedule)
	}
	if got := schedule.Schedule.Target.App.Operation; got != "issues.triage" {
		t.Fatalf("schedule target operation = %q, want issues.triage", got)
	}
	if schedule.ExecutionRef == nil || !strings.HasPrefix(schedule.ExecutionRef.ID, workflowScheduleExecutionRefBasePrefix) {
		t.Fatalf("schedule execution ref = %#v, want schedule snapshot ref", schedule.ExecutionRef)
	}
	if schedule.ExecutionRef.ID == definition.Definition.ID {
		t.Fatalf("schedule execution ref reused definition id %q, want snapshot ref", schedule.ExecutionRef.ID)
	}
	if schedule.ExecutionRef.SourceDefinitionID != definition.Definition.ID {
		t.Fatalf("schedule source definition id = %q, want %q", schedule.ExecutionRef.SourceDefinitionID, definition.Definition.ID)
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
	if trigger == nil || trigger.Trigger == nil || trigger.Trigger.Target.App == nil {
		t.Fatalf("trigger = %#v, want app target", trigger)
	}
	if got := trigger.Trigger.Target.App.Operation; got != "issues.triage" {
		t.Fatalf("trigger target operation = %q, want issues.triage", got)
	}
	if trigger.ExecutionRef == nil || !strings.HasPrefix(trigger.ExecutionRef.ID, workflowEventTriggerExecutionRefBasePrefix) {
		t.Fatalf("trigger execution ref = %#v, want trigger snapshot ref", trigger.ExecutionRef)
	}
	if trigger.ExecutionRef.ID == definition.Definition.ID {
		t.Fatalf("trigger execution ref reused definition id %q, want snapshot ref", trigger.ExecutionRef.ID)
	}
	if trigger.ExecutionRef.SourceDefinitionID != definition.Definition.ID {
		t.Fatalf("trigger source definition id = %q, want %q", trigger.ExecutionRef.SourceDefinitionID, definition.Definition.ID)
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
			Input:     map[string]any{"mode": "initial"},
		}},
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.updated",
			Input:     map[string]any{"mode": "updated"},
		}},
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
	if got := replayedSchedule.Schedule.Target.App.Operation; got != "issues.triage" {
		t.Fatalf("replayed schedule target operation = %q, want original snapshot", got)
	}
	if len(provider.upsertedSchedules) != 1 {
		t.Fatalf("provider upserted schedules = %d, want 1", len(provider.upsertedSchedules))
	}

	otherDefinition, err := manager.CreateDefinition(context.Background(), caller, DefinitionUpsert{
		ProviderName:  "local",
		CallerAppName: "github",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.updated",
			Input:     map[string]any{"mode": "other"},
		}},
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
	if got := replayedTrigger.Trigger.Target.App.Operation; got != "issues.updated" {
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
		}},
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
	target := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "github",
		Operation: "issues.triage",
	}}
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
	target := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "github",
		Operation: "issues.triage",
	}}
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
	target := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "github",
		Operation: "issues.triage",
	}}
	addRun := func(provider *testWorkflowProvider, providerName, runID string, createdAt time.Time) {
		refID := "ref-" + runID
		provider.refs[refID] = &coreworkflow.ExecutionReference{
			ID:           refID,
			ProviderName: providerName,
			Target:       target,
			SubjectID:    principal.UserSubjectID("ada"),
			SubjectKind:  string(principal.KindUser),
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

func TestListRunsFiltersTargetPluginInManager(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	githubTarget := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "github",
		Operation: "issues.triage",
	}}
	slackTarget := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "slack",
		Operation: "chat.postMessage",
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
	provider.listRunsHook = func(req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
		if req.TargetApp != "slack" {
			t.Fatalf("provider TargetApp = %q, want slack", req.TargetApp)
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
	if got := workflowManagerRunIDs(resp.Runs); !reflect.DeepEqual(got, []string{"run-slack"}) {
		t.Fatalf("ListRuns ids = %v, want slack run only", got)
	}
}

func TestListRunsKeepsUnorderedTokenlessProviderRunsReachable(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	target := coreworkflow.Target{App: &coreworkflow.AppTarget{
		AppName:   "github",
		Operation: "issues.triage",
	}}
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
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
		}},
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	var hookErr error
	remoteProvider.putExecutionReferenceHook = func(ref *coreworkflow.ExecutionReference) {
		if strings.TrimSpace(ref.ID) != definition.Definition.ID || ref.RevokedAt != nil {
			return
		}
		managed, err := manager.GetDefinition(context.Background(), caller, definition.Definition.ID)
		if err != nil {
			hookErr = err
			return
		}
		if managed.ProviderName != "remote" {
			hookErr = fmt.Errorf("definition provider during update = %q, want remote", managed.ProviderName)
		}
	}

	updated, err := manager.UpdateDefinition(context.Background(), caller, definition.Definition.ID, DefinitionUpsert{
		ProviderName:  "remote",
		CallerAppName: "github",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "issues.triage",
		}},
	})
	if err != nil {
		t.Fatalf("UpdateDefinition: %v", err)
	}
	if hookErr != nil {
		t.Fatalf("GetDefinition during provider change: %v", hookErr)
	}
	if updated.ProviderName != "remote" {
		t.Fatalf("updated provider = %q, want remote", updated.ProviderName)
	}
	localRef := localProvider.refs[definition.Definition.ID]
	if localRef == nil || localRef.RevokedAt == nil {
		t.Fatalf("local definition ref = %#v, want revoked", localRef)
	}
	remoteRef := remoteProvider.refs[definition.Definition.ID]
	if remoteRef == nil || remoteRef.RevokedAt != nil {
		t.Fatalf("remote definition ref = %#v, want active", remoteRef)
	}
}

func TestSignalOrStartRunExecutionRefInheritsDeclaredAgentToolInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"github": {
				{App: "github", Operation: "bot.commitFiles"},
				{App: "github", Operation: "bot.commentFinal", CredentialMode: core.ConnectionModeNone},
				{App: "github", Operation: "bot.commentStarted", CredentialMode: core.ConnectionModeNone},
				{App: "github", Operation: "bot.openPullRequest"},
			},
		},
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
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{App: "github", Operation: "bot.commitFiles"},
				{App: "github", Operation: "bot.openPullRequest"},
			},
			OutputDelivery: &coreworkflow.OutputDelivery{
				Target: coreworkflow.AppTarget{
					AppName:   "github",
					Operation: "bot.commentFinal",
				},
				InputBindings: []coreworkflow.OutputBinding{
					{InputField: "body", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
				},
			},
			SessionReadyDelivery: &coreworkflow.OutputDelivery{
				Target: coreworkflow.AppTarget{
					AppName:   "github",
					Operation: "bot.commentStarted",
				},
				InputBindings: []coreworkflow.OutputBinding{
					{InputField: "session_id", Value: coreworkflow.OutputValueSource{AgentSession: "id"}},
				},
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil {
		t.Fatalf("managed signal = %#v, want execution ref", managed)
	}

	wantPermissions := []core.AccessPermission{{
		App: "github",
		Operations: []string{
			"bot.commentFinal",
			"bot.commentStarted",
			"bot.commitFiles",
			"bot.openPullRequest",
			"events.handle",
		},
	}, {
		App: "simple",
	}}
	if !reflect.DeepEqual(managed.ExecutionRef.Permissions, wantPermissions) {
		t.Fatalf("execution ref permissions = %#v, want %#v", managed.ExecutionRef.Permissions, wantPermissions)
	}
	if managed.ExecutionRef.CallerAppName != "github" {
		t.Fatalf("caller app = %q, want github", managed.ExecutionRef.CallerAppName)
	}
	if got := managed.ExecutionRef.Target.Agent.OutputDelivery.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("output delivery credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := managed.ExecutionRef.Target.Agent.SessionReadyDelivery.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("session ready delivery credential mode = %q, want %q", got, core.ConnectionModeNone)
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
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Steps: []coreworkflow.AgentStep{
				{ID: "diagnosis", Prompt: "Diagnose the alert."},
				{
					ID:     "pr_fix",
					Prompt: "Open a PR.",
					When: &coreworkflow.AgentStepWhen{
						StepID:     "diagnosis",
						OutputPath: "structured_output.actionable_for_pr",
					},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow agent steps[1].when.equals is required") {
		t.Fatalf("SignalOrStartRun error = %v, want missing when.equals validation", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
	}
}

func TestSignalOrStartRunRejectsOutputDeliveryTargetCredentialMode(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"github": {
				{App: "github", Operation: "bot.commentFinal", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	callerPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle", "bot.commentFinal"},
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

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			OutputDelivery: &coreworkflow.OutputDelivery{
				Target: coreworkflow.AppTarget{
					AppName:        "github",
					Operation:      "bot.commentFinal",
					CredentialMode: core.ConnectionModeNone,
				},
				InputBindings: []coreworkflow.OutputBinding{
					{InputField: "body", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
				},
				CredentialMode: core.ConnectionModeNone,
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, invocation.ErrInvalidInvocation) {
		t.Fatalf("SignalOrStartRun error = %v, want invalid invocation", err)
	}
}

func TestSignalOrStartRunAppTargetCredentialModeUsesDeclaredInvoke(t *testing.T) {
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"github": {
				{App: "github", Operation: "reviewPullRequest", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle", "reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:github_app_installation:99:repo:acme/widgets",
		Kind:             principal.Kind("service_account"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7:policy:pr-review",
		CallerAppName: "github",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:        "github",
			Operation:      "reviewPullRequest",
			CredentialMode: core.ConnectionModeNone,
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil || managed.ExecutionRef.Target.App == nil {
		t.Fatalf("managed signal = %#v, want app execution ref", managed)
	}
	if got := managed.ExecutionRef.Target.App.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("stored credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != core.ConnectionModeNone {
		t.Fatalf("resolver credential modes = %#v, want final %q", invoker.modes, core.ConnectionModeNone)
	}
}

func TestSignalOrStartRunAppTargetCredentialModeKeepsBlankModeBlank(t *testing.T) {
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"github": {
				{App: "github", Operation: "reviewPullRequest", CredentialMode: core.ConnectionModeNone},
			},
		},
	})
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle", "reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "service_account:github_app_installation:99:repo:acme/widgets",
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7:policy:pr-review",
		CallerAppName: "github",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:   "github",
			Operation: "reviewPullRequest",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if got := managed.ExecutionRef.Target.App.CredentialMode; got != "" {
		t.Fatalf("stored credential mode = %q, want empty", got)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != "" {
		t.Fatalf("resolver credential modes = %#v, want final empty", invoker.modes)
	}
	blankRefID := managed.ExecutionRef.ID

	explicit, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7:policy:pr-review",
		CallerAppName: "github",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:        "github",
			Operation:      "reviewPullRequest",
			CredentialMode: core.ConnectionModeNone,
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun explicit mode: %v", err)
	}
	if explicit.ExecutionRef.ID == blankRefID {
		t.Fatalf("explicit credential mode reused blank execution ref id %q", blankRefID)
	}
}

func TestCreateScheduleRejectsAppTargetCredentialModeWithoutCaller(t *testing.T) {
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
		App:        "github",
		Operations: []string{"reviewPullRequest"},
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        principal.UserSubjectID("ada"),
		TokenPermissions: permissions,
		Scopes:           principal.PermissionApps(permissions),
	})

	_, err := manager.CreateSchedule(context.Background(), caller, ScheduleUpsert{
		ProviderName: "local",
		Cron:         "*/5 * * * *",
		Target: coreworkflow.Target{App: &coreworkflow.AppTarget{
			AppName:        "github",
			Operation:      "reviewPullRequest",
			CredentialMode: core.ConnectionModeNone,
		}},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("CreateSchedule error = %v, want authorization denied", err)
	}
}

func TestSignalOrStartRunReusesExecutionRefForSameWorkflowKeyAndTarget(t *testing.T) {
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
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	}

	first, err := manager.SignalOrStartRun(context.Background(), caller, req)
	if err != nil {
		t.Fatalf("SignalOrStartRun(first): %v", err)
	}
	second, err := manager.SignalOrStartRun(context.Background(), caller, req)
	if err != nil {
		t.Fatalf("SignalOrStartRun(second): %v", err)
	}
	if first.ExecutionRef.ID == "" {
		t.Fatal("first execution ref ID is empty")
	}
	if second.ExecutionRef.ID != first.ExecutionRef.ID {
		t.Fatalf("execution ref ID changed from %q to %q", first.ExecutionRef.ID, second.ExecutionRef.ID)
	}
	if len(provider.refs) != 1 {
		t.Fatalf("execution refs stored = %d, want 1", len(provider.refs))
	}
}

func TestSignalOrStartRunRejectsDeniedExecutionRefPermissionsBeforeEnqueue(t *testing.T) {
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
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{App: "github", Operation: "bot.admin"},
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
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

func TestSignalOrStartRunFailureDoesNotRevokeStableExecutionRef(t *testing.T) {
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
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	}

	first, err := manager.SignalOrStartRun(context.Background(), caller, req)
	if err != nil {
		t.Fatalf("SignalOrStartRun(first): %v", err)
	}
	if first.ExecutionRef == nil || first.ExecutionRef.ID == "" {
		t.Fatalf("first execution ref = %#v, want stable ref", first.ExecutionRef)
	}

	provider.signalOrStartErr = errors.New("transient provider failure")
	_, err = manager.SignalOrStartRun(context.Background(), caller, req)
	if !errors.Is(err, provider.signalOrStartErr) {
		t.Fatalf("SignalOrStartRun(second) error = %v, want %v", err, provider.signalOrStartErr)
	}

	stored := provider.refs[first.ExecutionRef.ID]
	if stored == nil {
		t.Fatalf("execution ref %q was removed", first.ExecutionRef.ID)
	}
	if stored.RevokedAt != nil {
		t.Fatalf("execution ref RevokedAt = %v, want nil", stored.RevokedAt)
	}
}

func TestSignalOrStartRunFirstFailureKeepsStableExecutionRef(t *testing.T) {
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
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	}

	provider.signalOrStartErr = errors.New("transient provider failure")
	_, err := manager.SignalOrStartRun(context.Background(), caller, req)
	if !errors.Is(err, provider.signalOrStartErr) {
		t.Fatalf("SignalOrStartRun error = %v, want %v", err, provider.signalOrStartErr)
	}
	if len(provider.refs) != 1 {
		t.Fatalf("execution refs stored = %d, want 1", len(provider.refs))
	}
	for id, ref := range provider.refs {
		if ref.RevokedAt != nil {
			t.Fatalf("execution ref %q RevokedAt = %v, want nil", id, ref.RevokedAt)
		}
	}
}

func TestSignalOrStartRunCreatesExecutionRefAfterGRPCNotFound(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	provider.getMissingExecutionReferenceErr = status.Error(codes.NotFound, "missing")
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: "system:http_binding:github:event",
	})

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil {
		t.Fatalf("managed signal = %#v, want execution ref", managed)
	}
	if provider.putExecutionReferenceCalls != 1 {
		t.Fatalf("PutExecutionReference calls = %d, want 1", provider.putExecutionReferenceCalls)
	}
}

func TestSignalOrStartRunDoesNotRewriteExecutionRefForDifferentPermissions(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
	})
	initialPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle"},
	}, {
		App: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: initialPermissions,
		Scopes:           principal.PermissionApps(initialPermissions),
	})
	req := RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	}

	first, err := manager.SignalOrStartRun(context.Background(), caller, req)
	if err != nil {
		t.Fatalf("SignalOrStartRun(first): %v", err)
	}
	stored := provider.refs[first.ExecutionRef.ID]
	if stored == nil {
		t.Fatalf("execution ref %q was not stored", first.ExecutionRef.ID)
	}
	initialRefPermissions := stored.Permissions
	putCalls := provider.putExecutionReferenceCalls

	broaderPermissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"events.handle", "bot.admin"},
	}, {
		App: "simple",
	}})
	broaderCaller := principal.Canonicalize(&principal.Principal{
		SubjectID:        caller.SubjectID,
		TokenPermissions: broaderPermissions,
		Scopes:           principal.PermissionApps(broaderPermissions),
	})
	provider.signalOrStartErr = errors.New("transient provider failure")
	_, err = manager.SignalOrStartRun(context.Background(), broaderCaller, req)
	if !errors.Is(err, provider.signalOrStartErr) {
		t.Fatalf("SignalOrStartRun(second) error = %v, want %v", err, provider.signalOrStartErr)
	}
	if provider.putExecutionReferenceCalls != putCalls+1 {
		t.Fatalf("PutExecutionReference calls = %d, want %d", provider.putExecutionReferenceCalls, putCalls+1)
	}
	stored = provider.refs[first.ExecutionRef.ID]
	if !reflect.DeepEqual(stored.Permissions, initialRefPermissions) {
		t.Fatalf("execution ref permissions = %#v, want original %#v", stored.Permissions, initialRefPermissions)
	}
	if stored.RevokedAt != nil {
		t.Fatalf("execution ref RevokedAt = %v, want nil", stored.RevokedAt)
	}
	if len(provider.refs) != 2 {
		t.Fatalf("execution refs stored = %d, want 2 permission-scoped refs", len(provider.refs))
	}
	for id, ref := range provider.refs {
		if ref.RevokedAt != nil {
			t.Fatalf("execution ref %q RevokedAt = %v, want nil", id, ref.RevokedAt)
		}
	}
}

func TestExecutionRefPermissionsScopeDistinguishesNilAndEmpty(t *testing.T) {
	t.Parallel()

	if executionRefPermissionsScope(nil) == executionRefPermissionsScope([]core.AccessPermission{}) {
		t.Fatal("nil and empty execution ref permissions produced the same scope")
	}
}

func TestSignalOrStartRunExecutionRefDoesNotInheritSurfaceInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"github": {
				{App: "github", Surface: "graphql"},
			},
		},
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

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{App: "github", Operation: "bot.createPullRequest"},
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("SignalOrStartRun error = %v, want not found from unauthorized target", err)
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
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("SignalOrStartRun error = %v, want authorization denied from unauthorized agent provider", err)
	}
	if provider.signalOrStartCalls != 0 {
		t.Fatalf("SignalOrStartRun provider calls = %d, want 0", provider.signalOrStartCalls)
	}
}

func TestSignalOrStartRunRejectsRuntimeDeniedAgentProvider(t *testing.T) {
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

	_, err = manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:  "local",
		WorkflowKey:   "github:99:acme/widgets:7",
		CallerAppName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("SignalOrStartRun error = %v, want authorization denied from runtime-denied agent provider", err)
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
	target := coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName: "simple",
		Prompt:       "Handle the webhook.",
		ToolRefs: []coreagent.ToolRef{
			{App: "github", Operation: "bot.openPullRequest"},
		},
	}}
	ref := &coreworkflow.ExecutionReference{
		ID:           "workflow_run:stale-permissions",
		ProviderName: "local",
		Target:       target,
		SubjectID:    "system:http_binding:github:event",
		Permissions: []core.AccessPermission{{
			App:        "github",
			Operations: []string{"events.handle"},
		}, {
			App: "simple",
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
		Target:         coreworkflow.Target{Agent: &coreworkflow.AgentTarget{ProviderName: "simple", Prompt: "Sync roadmap."}},
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
	schedules                       map[string]*coreworkflow.Schedule
	eventTriggers                   map[string]*coreworkflow.EventTrigger
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
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		refs:          map[string]*coreworkflow.ExecutionReference{},
		runs:          map[string]*coreworkflow.Run{},
		schedules:     map[string]*coreworkflow.Schedule{},
		eventTriggers: map[string]*coreworkflow.EventTrigger{},
	}
}

func (p *testWorkflowProvider) StartRun(_ context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
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
		if req.TargetApp != "" && (run.Target.App == nil || strings.TrimSpace(run.Target.App.AppName) != strings.TrimSpace(req.TargetApp)) {
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

func (p *testWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	p.publishedEvents = append(p.publishedEvents, req)
	return &req.Event, nil
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
