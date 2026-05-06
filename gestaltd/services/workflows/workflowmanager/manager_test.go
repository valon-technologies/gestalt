package workflowmanager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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

func TestSignalOrStartRunExecutionRefInheritsDeclaredAgentToolInvokes(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{
		Workflow:     testWorkflowControl{provider: provider},
		Agent:        testAgentControl{},
		AgentManager: testAgentManager{},
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"github": {
				{Plugin: "github", Operation: "bot.commitFiles"},
				{Plugin: "github", Operation: "bot.commentFinal", CredentialMode: core.ConnectionModeNone},
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
	}, {
		Plugin: "simple",
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

func TestSignalOrStartRunRejectsOutputDeliveryTargetCredentialMode(t *testing.T) {
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

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			OutputDelivery: &coreworkflow.OutputDelivery{
				Target: coreworkflow.PluginTarget{
					PluginName:     "github",
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

func TestSignalOrStartRunPluginTargetCredentialModeUsesDeclaredInvoke(t *testing.T) {
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

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7:policy:pr-review",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{
			PluginName:     "github",
			Operation:      "reviewPullRequest",
			CredentialMode: core.ConnectionModeNone,
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if managed == nil || managed.ExecutionRef == nil || managed.ExecutionRef.Target.Plugin == nil {
		t.Fatalf("managed signal = %#v, want plugin execution ref", managed)
	}
	if got := managed.ExecutionRef.Target.Plugin.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("stored credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != core.ConnectionModeNone {
		t.Fatalf("resolver credential modes = %#v, want final %q", invoker.modes, core.ConnectionModeNone)
	}
}

func TestSignalOrStartRunPluginTargetCredentialModeKeepsBlankModeBlank(t *testing.T) {
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

	managed, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7:policy:pr-review",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{
			PluginName: "github",
			Operation:  "reviewPullRequest",
		}},
		Signal: coreworkflow.Signal{Name: "github.app.webhook"},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if got := managed.ExecutionRef.Target.Plugin.CredentialMode; got != "" {
		t.Fatalf("stored credential mode = %q, want empty", got)
	}
	if len(invoker.modes) == 0 || invoker.modes[len(invoker.modes)-1] != "" {
		t.Fatalf("resolver credential modes = %#v, want final empty", invoker.modes)
	}
	blankRefID := managed.ExecutionRef.ID

	explicit, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7:policy:pr-review",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{
			PluginName:     "github",
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
		Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{
			PluginName:     "github",
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
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName: "simple",
			Prompt:       "Handle the webhook.",
			ToolRefs: []coreagent.ToolRef{
				{Plugin: "github", Operation: "bot.admin"},
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
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
		Plugin:     "github",
		Operations: []string{"events.handle"},
	}, {
		Plugin: "simple",
	}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:github:event",
		TokenPermissions: initialPermissions,
		Scopes:           principal.PermissionPlugins(initialPermissions),
	})
	req := RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
		Plugin:     "github",
		Operations: []string{"events.handle", "bot.admin"},
	}, {
		Plugin: "simple",
	}})
	broaderCaller := principal.Canonicalize(&principal.Principal{
		SubjectID:        caller.SubjectID,
		TokenPermissions: broaderPermissions,
		Scopes:           principal.PermissionPlugins(broaderPermissions),
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

func TestSignalOrStartRunRejectsUnauthorizedAgentProvider(t *testing.T) {
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

	_, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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

	_, err = manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
		ProviderName:     "local",
		WorkflowKey:      "github:99:acme/widgets:7",
		CallerPluginName: "github",
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
	refs                            map[string]*coreworkflow.ExecutionReference
	runs                            map[string]*coreworkflow.Run
	schedules                       map[string]*coreworkflow.Schedule
	upsertedSchedules               []coreworkflow.UpsertScheduleRequest
	signalOrStartErr                error
	signalOrStartCalls              int
	getMissingExecutionReferenceErr error
	putExecutionReferenceCalls      int
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		refs:      map[string]*coreworkflow.ExecutionReference{},
		runs:      map[string]*coreworkflow.Run{},
		schedules: map[string]*coreworkflow.Schedule{},
	}
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
	p.putExecutionReferenceCalls++
	copied := *ref
	p.refs[copied.ID] = &copied
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
