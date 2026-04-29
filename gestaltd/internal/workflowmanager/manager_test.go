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
			ToolSource:   coreagent.ToolSourceModeNativeSearch,
			ToolRefs: []coreagent.ToolRef{
				{Plugin: "github", Operation: "bot.commitFiles"},
				{Plugin: "github", Operation: "bot.openPullRequest"},
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
			ToolSource:   coreagent.ToolSourceModeNativeSearch,
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
		ToolSource:   coreagent.ToolSourceModeNativeSearch,
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
	refs map[string]*coreworkflow.ExecutionReference
	runs map[string]*coreworkflow.Run
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		refs: map[string]*coreworkflow.ExecutionReference{},
		runs: map[string]*coreworkflow.Run{},
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
