package workflowmanager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestSignalOrStartRunExecutionRefInheritsDeclaredAgentToolInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "bot.commitFiles"},
				{Plugin: "github", Operation: "bot.commentFinal", CredentialMode: providermanifestv1.ConnectionModeNone},
				{Plugin: "github", Operation: "bot.openPullRequest"},
			},
		},
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

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{Plugin: "github", Operation: "bot.commitFiles"},
				{Plugin: "github", Operation: "bot.openPullRequest"},
			},
			OutputDelivery: &coreworkflow.OutputDelivery{
				Target: coreworkflow.PluginTarget{
					PluginName: "github",
					Operation:  "bot.commentFinal",
				},
				InputBindings: []coreworkflow.OutputBinding{
					{InputField: "body", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
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
		Plugin: "github",
		Operations: []string{
			"bot.commentFinal",
			"bot.commitFiles",
			"bot.openPullRequest",
			"events.handle",
		},
	}}
	if !reflect.DeepEqual(managed.ExecutionRef.Permissions, wantPermissions) {
		t.Fatalf("execution ref permissions = %#v, want %#v", managed.ExecutionRef.Permissions, wantPermissions)
	}
	if managed.ExecutionRef.CallerPluginName != "github" {
		t.Fatalf("caller plugin = %q, want github", managed.ExecutionRef.CallerPluginName)
	}
	if got := managed.ExecutionRef.Target.Agent.OutputDelivery.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("output delivery credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
}

func TestSignalOrStartRunExecutionRefDoesNotInheritSurfaceInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Surface: "graphql"},
			},
		},
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

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{Plugin: "github", Operation: "bot.createPullRequest"},
			},
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("SignalOrStartRun error = %v, want not found from unauthorized target", err)
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
			{Plugin: "github", Operation: "bot.openPullRequest"},
		},
	}}
	ref := &coreworkflow.ExecutionReference{
		ID:           "workflow_run:stale-permissions",
		ProviderName: "local",
		Target:       target,
		SubjectID:    "system:http_binding:github:event",
		Permissions: []core.AccessPermission{{
			Plugin:     "github",
			Operations: []string{"events.handle"},
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
		Target:         coreworkflow.Target{Agent: &coreworkflow.AgentTarget{ProviderName: "simple", Prompt: "Sync roadmap."}},
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
	refs              map[string]*coreworkflow.ExecutionReference
	runs              map[string]*coreworkflow.Run
	schedules         map[string]*coreworkflow.Schedule
	upsertedSchedules []coreworkflow.UpsertScheduleRequest
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		refs:      map[string]*coreworkflow.ExecutionReference{},
		runs:      map[string]*coreworkflow.Run{},
		schedules: map[string]*coreworkflow.Schedule{},
	}
}

func (p *testWorkflowProvider) SignalOrStartRun(_ context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
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

func (p *testWorkflowProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	copied := *ref
	p.refs[copied.ID] = &copied
	return &copied, nil
}

func (p *testWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	ref := p.refs[strings.TrimSpace(id)]
	if ref == nil {
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
