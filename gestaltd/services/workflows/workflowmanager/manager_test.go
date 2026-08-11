package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

func testWorkflowAppStepTarget(appName, operation string, input map[string]any) coreworkflow.Target {
	call := &coreworkflow.AppCall{Name: appName, Operation: operation}
	if input != nil {
		call.Input = coreworkflow.Value{Object: map[string]coreworkflow.Value{}}
		for key, value := range input {
			call.Input.Object[key] = coreworkflow.Value{Literal: value, LiteralSet: true}
		}
	}
	return coreworkflow.Target{Steps: []coreworkflow.Step{{ID: "run", App: call}}}
}

func requireWorkflowAppStep(t *testing.T, target coreworkflow.Target, stepIndex int) *coreworkflow.AppCall {
	t.Helper()
	if len(target.Steps) <= stepIndex || target.Steps[stepIndex].App == nil {
		t.Fatalf("target steps = %#v, want app step at index %d", target.Steps, stepIndex)
	}
	return target.Steps[stepIndex].App
}

func testWorkflowManagerPrincipal() *principal.Principal {
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        "github",
		Operations: []string{"issues.triage"},
	}})
	return principal.Canonicalize(&principal.Principal{
		SubjectID: principal.UserSubjectID("ada"),
		UserID:    "ada",
		Kind:      principal.KindUser,
		Scopes:    principal.ScopeStringsFromPermissionSet(permissions),
	})
}

func testWorkflowManagerCaller() invocation.CallerProvider {
	return invocation.CallerProvider{Kind: invocation.ProviderKindApp, Name: "github"}
}

func requireWorkflowManagerRequestContext(t *testing.T, reqCtx *proto.RequestContext, kind invocation.ProviderKind, name string) {
	t.Helper()
	if got := reqCtx.GetSubject().GetId(); got != principal.UserSubjectID("ada") {
		t.Fatalf("request context subject = %q, want user:ada", got)
	}
	if got := reqCtx.GetCaller().GetKind(); got != string(kind) {
		t.Fatalf("request context caller kind = %q, want %q", got, kind)
	}
	if got := reqCtx.GetCaller().GetName(); got != name {
		t.Fatalf("request context caller name = %q, want %q", got, name)
	}
}

func testWorkflowManagerWithGithub(t *testing.T, provider *testWorkflowProvider) *Manager {
	return testWorkflowManagerWithGithubInvoker(t, provider, nil)
}

func testWorkflowRunAsSubject() string {
	return "service_account:workflow-runner"
}

func testWorkflowGithubProviders(t *testing.T) *registry.ProviderMap[core.Provider] {
	t.Helper()
	return testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "github",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "github",
			Operations: []catalog.CatalogOperation{
				{ID: "issues.triage", Method: "POST"},
			},
		},
	})
}

func testWorkflowManagerBroker(t *testing.T, providers *registry.ProviderMap[core.Provider], authz core.AuthorizationProvider) invocation.Invoker {
	t.Helper()
	return invocation.NewBroker(providers, nil, nil,
		invocation.WithAuthorizationProvider(authz),
		invocation.WithProviderKinds(map[string]invocation.ProviderKind{"github": invocation.ProviderKindApp}),
	)
}

func testWorkflowManagerWithGithubInvoker(t *testing.T, provider *testWorkflowProvider, invoker invocation.Invoker) *Manager {
	t.Helper()
	providers := testWorkflowGithubProviders(t)
	if invoker == nil {
		invoker = testWorkflowManagerBroker(t, providers, allowAllAuthz{})
	}
	cfg := Config{Providers: providers, Workflow: testWorkflowControl{provider: provider}, Invoker: invoker}
	return New(cfg)
}

func testWorkflowManagerPrincipalWithoutGithub() *principal.Principal {
	return principal.Canonicalize(&principal.Principal{
		SubjectID: principal.UserSubjectID("bob"),
		UserID:    "bob",
		Kind:      principal.KindUser,
	})
}

type allowAllAuthz struct {
	core.AuthorizationProvider
}

func (allowAllAuthz) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

type runAsGrantAuthz struct {
	core.AuthorizationProvider
	grants map[string]struct{}
}

type remoteDelegatedWorkflowApp struct {
	*coretesting.StubIntegration
}

func (remoteDelegatedWorkflowApp) RemoteCredentialDelegated() bool { return true }

type denyingAgentWorkflowAuthorizer struct {
	agentmanager.Service
	requests []invocation.AgentWorkflowAuthorizationRequest
}

func (a *denyingAgentWorkflowAuthorizer) AuthorizeWorkflowInvocation(_ context.Context, req invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error) {
	a.requests = append(a.requests, req)
	return invocation.AgentWorkflowAuthorization{}, status.Error(codes.PermissionDenied, "agent workflow target is not allowed")
}

type tokenCountingWorkflowBroker struct {
	*invocation.Broker
	tokenResolutions int
}

func (b *tokenCountingWorkflowBroker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	b.tokenResolutions++
	return b.Broker.ResolveToken(ctx, p, providerName, connection, instance)
}

func (a *runAsGrantAuthz) grantKey(subjectID, providerName, operation string) string {
	return strings.TrimSpace(subjectID) + "|" + strings.TrimSpace(providerName) + "|" + strings.TrimSpace(operation)
}

func (a *runAsGrantAuthz) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	_, ok := a.grants[a.grantKey(req.GetSubject().GetId(), req.GetResource().GetId(), req.GetAction().GetName())]
	return &proto.CheckAccessResponse{Allowed: ok}, nil
}

func TestApplyDefinitionAndStartRunUseDefinitionGenerationAndInput(t *testing.T) {
	t.Parallel()

	t.Run("generation and input", func(t *testing.T) {
		t.Parallel()

		provider := newTestWorkflowProvider()
		manager := testWorkflowManagerWithGithub(t, provider)
		caller := testWorkflowManagerPrincipal()

		definition, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
			ProviderName:   "local",
			Caller:         testWorkflowManagerCaller(),
			IdempotencyKey: "definition-apply-1",
			Spec: coreworkflow.DefinitionSpec{
				ID:     "definition-1",
				RunAs:  testWorkflowRunAsSubject(),
				Target: testWorkflowAppStepTarget("github", "issues.triage", map[string]any{"mode": "full"}),
				Activations: []coreworkflow.Activation{{
					ID: "github_issue",
					Event: &coreworkflow.EventActivation{Match: coreworkflow.EventMatch{
						Type:   "github.issue",
						Source: "github",
					}},
					Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"issue": {Signal: "data.issue"},
					}},
				}},
			},
		})
		if err != nil {
			t.Fatalf("ApplyDefinition: %v", err)
		}
		if definition == nil || definition.Definition == nil || definition.Definition.ID != "definition-1" || definition.Definition.Generation != 1 {
			t.Fatalf("definition = %#v", definition)
		}
		if len(provider.applyRequests) != 1 {
			t.Fatalf("apply requests = %#v", provider.applyRequests)
		}
		if got := provider.applyRequests[0].GetProvider(); got != "local" {
			t.Fatalf("apply provider name = %q, want local", got)
		}
		requireWorkflowManagerRequestContext(t, provider.applyRequests[0].GetContext(), invocation.ProviderKindApp, "github")

		run, err := manager.StartRun(context.Background(), caller, RunStart{
			ProviderName:   "local",
			Caller:         testWorkflowManagerCaller(),
			DefinitionID:   "definition-1",
			WorkflowKey:    "github:issues:triage",
			Input:          map[string]any{"issue": map[string]any{"number": 42}},
			IdempotencyKey: "run-1",
		})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if run == nil || run.Run == nil {
			t.Fatalf("run = %#v", run)
		}
		if run.Run.DefinitionGeneration != 1 {
			t.Fatalf("run generation = %d, want 1", run.Run.DefinitionGeneration)
		}
		if got := run.Run.Input["issue"].(map[string]any)["number"]; got != float64(42) {
			t.Fatalf("run input issue.number = %#v, want 42", got)
		}
		runApp := requireWorkflowAppStep(t, run.Run.Target, 0)
		if got := runApp.Operation; got != "issues.triage" {
			t.Fatalf("run target operation = %q, want issues.triage", got)
		}
		if len(provider.startRunRequests) != 1 || provider.startRunRequests[0].GetExpectedDefinitionGeneration() != 1 {
			t.Fatalf("start requests = %#v", provider.startRunRequests)
		}
		if got := provider.startRunRequests[0].GetProvider(); got != "local" {
			t.Fatalf("start provider name = %q, want local", got)
		}
		requireWorkflowManagerRequestContext(t, provider.startRunRequests[0].GetContext(), invocation.ProviderKindApp, "github")
	})

	t.Run("run_as target grants", func(t *testing.T) {
		t.Parallel()

		grant := map[string]struct{}{
			"service_account:workflow-runner|github|issues.triage": {},
		}
		spec := coreworkflow.DefinitionSpec{
			ID:     "definition-run-as",
			RunAs:  testWorkflowRunAsSubject(),
			Target: testWorkflowAppStepTarget("github", "issues.triage", nil),
		}

		t.Run("author without step grants", func(t *testing.T) {
			t.Parallel()
			providers := testWorkflowGithubProviders(t)
			manager := New(Config{
				Providers: providers,
				Workflow:  testWorkflowControl{provider: newTestWorkflowProvider()},
				Invoker:   testWorkflowManagerBroker(t, providers, &runAsGrantAuthz{grants: grant}),
			})
			if _, err := manager.ApplyDefinition(context.Background(), testWorkflowManagerPrincipalWithoutGithub(), DefinitionApply{
				ProviderName: "local",
				Spec:         spec,
			}); err != nil {
				t.Fatalf("ApplyDefinition: %v", err)
			}
		})

		t.Run("run_as without grants", func(t *testing.T) {
			t.Parallel()
			providers := testWorkflowGithubProviders(t)
			broker := &tokenCountingWorkflowBroker{Broker: testWorkflowManagerBroker(t, providers, &runAsGrantAuthz{}).(*invocation.Broker)}
			manager := New(Config{
				Providers: providers,
				Workflow:  testWorkflowControl{provider: newTestWorkflowProvider()},
				Invoker:   broker,
			})
			_, err := manager.ApplyDefinition(context.Background(), testWorkflowManagerPrincipal(), DefinitionApply{
				ProviderName: "local",
				Spec:         spec,
			})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("ApplyDefinition error = %v, want authorization denied", err)
			}
			if broker.tokenResolutions != 0 {
				t.Fatalf("token resolutions = %d, want 0", broker.tokenResolutions)
			}
		})

		t.Run("starter without step grants", func(t *testing.T) {
			t.Parallel()
			provider := newTestWorkflowProvider()
			providers := testWorkflowGithubProviders(t)
			manager := New(Config{
				Providers: providers,
				Workflow:  testWorkflowControl{provider: provider},
				Invoker:   testWorkflowManagerBroker(t, providers, &runAsGrantAuthz{grants: grant}),
			})
			if _, err := manager.ApplyDefinition(context.Background(), testWorkflowManagerPrincipal(), DefinitionApply{
				ProviderName: "local",
				Spec:         spec,
			}); err != nil {
				t.Fatalf("ApplyDefinition: %v", err)
			}
			if _, err := manager.StartRun(context.Background(), testWorkflowManagerPrincipalWithoutGithub(), RunStart{
				ProviderName: "local",
				DefinitionID: "definition-run-as",
				WorkflowKey:  "github:issues:triage",
			}); err != nil {
				t.Fatalf("StartRun: %v", err)
			}
		})

		t.Run("remote delegated target skips local grants", func(t *testing.T) {
			t.Parallel()
			provider := newTestWorkflowProvider()
			providers := testutil.NewProviderRegistry(t, remoteDelegatedWorkflowApp{StubIntegration: &coretesting.StubIntegration{
				N:          "linear",
				ConnMode:   core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.triage", Method: "POST"}}},
			}})
			manager := New(Config{
				Providers: providers,
				Workflow:  testWorkflowControl{provider: provider},
				Invoker:   testWorkflowManagerBroker(t, providers, &runAsGrantAuthz{}),
			})
			spec := coreworkflow.DefinitionSpec{
				ID:     "definition-remote-delegated",
				RunAs:  testWorkflowRunAsSubject(),
				Target: testWorkflowAppStepTarget("linear", "issues.triage", nil),
			}
			if _, err := manager.ApplyDefinition(context.Background(), testWorkflowManagerPrincipalWithoutGithub(), DefinitionApply{
				ProviderName: "local",
				Spec:         spec,
			}); err != nil {
				t.Fatalf("ApplyDefinition: %v", err)
			}
			if _, err := manager.StartRun(context.Background(), testWorkflowManagerPrincipalWithoutGithub(), RunStart{
				ProviderName: "local",
				DefinitionID: spec.ID,
				WorkflowKey:  "linear:issues:triage",
			}); err != nil {
				t.Fatalf("StartRun: %v", err)
			}
		})

		t.Run("agent starts authorize the stored target", func(t *testing.T) {
			t.Parallel()
			provider := newTestWorkflowProvider()
			provider.definitions["definition-agent"] = &coreworkflow.Definition{
				ID:         "definition-agent",
				Generation: 1,
				RunAs:      testWorkflowRunAsSubject(),
				Target:     testWorkflowAppStepTarget("github", "issues.triage", nil),
			}
			providers := testWorkflowGithubProviders(t)
			agentAuth := &denyingAgentWorkflowAuthorizer{}
			manager := New(Config{
				Providers:    providers,
				Workflow:     testWorkflowControl{provider: provider},
				AgentManager: agentAuth,
				Invoker:      testWorkflowManagerBroker(t, providers, allowAllAuthz{}),
			})
			ctx := invocation.WithAgentInvocationContext(context.Background(), invocation.AgentInvocationContext{
				ProviderName: "agent",
				SessionID:    "session-1",
				TurnID:       "turn-1",
			})
			operations := []string{workflowManagerOperationRunsStart, workflowManagerOperationRunsSignalOrStart}
			for _, operation := range operations {
				_, _, _, _, err := manager.resolveRequestProviderTarget(ctx, testWorkflowManagerPrincipal(), "local", "definition-agent", testWorkflowManagerCaller(), operation)
				if status.Code(err) != codes.PermissionDenied {
					t.Fatalf("resolveRequestProviderTarget(%q) error = %v, want permission denied", operation, err)
				}
			}
			if len(agentAuth.requests) != 2 {
				t.Fatalf("agent authorization requests = %d, want 2", len(agentAuth.requests))
			}
			for i, req := range agentAuth.requests {
				if req.Operation != operations[i] {
					t.Fatalf("agent authorization operation = %q, want %q", req.Operation, operations[i])
				}
				if req.Target == nil || requireWorkflowAppStep(t, *req.Target, 0).Name != "github" || requireWorkflowAppStep(t, *req.Target, 0).Operation != "issues.triage" {
					t.Fatalf("agent authorization target = %#v, want github app step", req.Target)
				}
			}
		})
	})
}

func TestSignalOrStartRunRequiresDefinitionAndCarriesInput(t *testing.T) {
	t.Parallel()

	t.Run("carries input", func(t *testing.T) {
		t.Parallel()

		provider := newTestWorkflowProvider()
		manager := testWorkflowManagerWithGithub(t, provider)
		caller := testWorkflowManagerPrincipal()
		if _, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
			ProviderName: "local",
			WorkflowKey:  "github:issue:42",
			Signal:       coreworkflow.Signal{Name: "github.issue"},
		}); !errors.Is(err, invocation.ErrInvalidInvocation) {
			t.Fatalf("SignalOrStartRun without definition error = %v, want invalid invocation", err)
		}

		if _, err := manager.ApplyDefinition(context.Background(), caller, DefinitionApply{
			ProviderName: "local",
			Caller:       testWorkflowManagerCaller(),
			Spec: coreworkflow.DefinitionSpec{
				ID:     "definition-1",
				RunAs:  testWorkflowRunAsSubject(),
				Target: testWorkflowAppStepTarget("github", "issues.triage", nil),
			},
		}); err != nil {
			t.Fatalf("ApplyDefinition: %v", err)
		}
		signaled, err := manager.SignalOrStartRun(context.Background(), caller, RunSignalOrStart{
			ProviderName:   "local",
			Caller:         testWorkflowManagerCaller(),
			WorkflowKey:    "github:issue:42",
			DefinitionID:   "definition-1",
			Input:          map[string]any{"issue_number": 42},
			IdempotencyKey: "signal-1",
			Signal:         coreworkflow.Signal{Name: "github.issue", Payload: map[string]any{"ok": true}},
		})
		if err != nil {
			t.Fatalf("SignalOrStartRun: %v", err)
		}
		if signaled == nil || signaled.Run == nil || !signaled.StartedRun {
			t.Fatalf("signaled = %#v", signaled)
		}
		if got := signaled.Run.Input["issue_number"]; got != float64(42) {
			t.Fatalf("run input issue_number = %#v, want 42", got)
		}
		if len(provider.signalOrStartRequests) != 1 || provider.signalOrStartRequests[0].GetDefinitionId() != "definition-1" {
			t.Fatalf("signal requests = %#v", provider.signalOrStartRequests)
		}
		if got := provider.signalOrStartRequests[0].GetProvider(); got != "local" {
			t.Fatalf("signal provider name = %q, want local", got)
		}
		requireWorkflowManagerRequestContext(t, provider.signalOrStartRequests[0].GetContext(), invocation.ProviderKindApp, "github")
	})
}

func TestDeliverEventPreservesCallerApp(t *testing.T) {
	t.Parallel()

	provider := newTestWorkflowProvider()
	manager := New(Config{Workflow: testWorkflowControl{provider: provider}})
	caller := principal.Canonicalize(&principal.Principal{
		SubjectID: principal.UserSubjectID("ada"),
		UserID:    "ada",
		Kind:      principal.KindUser,
	})

	if _, err := manager.DeliverEvent(context.Background(), caller, EventDeliver{
		ProviderName: "local",
		AppName:      " github ",
		Event:        coreworkflow.Event{Type: "issue.created", Source: "slack"},
	}); err != nil {
		t.Fatalf("DeliverEvent selected provider: %v", err)
	}
	if _, err := manager.DeliverEvent(context.Background(), caller, EventDeliver{
		ProviderName: "local",
		AppName:      " github ",
		Event:        coreworkflow.Event{Type: "issue.updated"},
	}); err != nil {
		t.Fatalf("DeliverEvent fan-out: %v", err)
	}
	if len(provider.deliveredEvents) != 2 {
		t.Fatalf("delivered events = %d, want 2", len(provider.deliveredEvents))
	}
	for i, req := range provider.deliveredEvents {
		if got := req.GetProvider(); got != "local" {
			t.Fatalf("deliveredEvents[%d].ProviderName = %q, want local", i, got)
		}
		if req.GetEvent().GetSource() != "github" {
			t.Fatalf("deliveredEvents[%d].Event.Source = %q, want github", i, req.GetEvent().GetSource())
		}
		requireWorkflowManagerRequestContext(t, req.GetContext(), invocation.ProviderKindApp, "github")
	}

	if _, err := manager.DeliverEvent(context.Background(), caller, EventDeliver{
		Event: coreworkflow.Event{Type: "issue.deleted"},
	}); !errors.Is(err, ErrWorkflowEventSourceRequired) {
		t.Fatalf("DeliverEvent without source app error = %v, want ErrWorkflowEventSourceRequired", err)
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

type testWorkflowProvider struct {
	definitions           map[string]*coreworkflow.Definition
	runs                  map[string]*coreworkflow.Run
	applyRequests         []*proto.ApplyWorkflowProviderDefinitionRequest
	startRunRequests      []*proto.StartWorkflowProviderRunRequest
	signalOrStartRequests []*proto.SignalOrStartWorkflowProviderRunRequest
	deliveredEvents       []*proto.DeliverWorkflowProviderEventRequest
	listRunsRequests      []*proto.ListWorkflowProviderRunsRequest
	listRunsHandler       func(*proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error)
}

func newTestWorkflowProvider() *testWorkflowProvider {
	return &testWorkflowProvider{
		definitions: map[string]*coreworkflow.Definition{},
		runs:        map[string]*coreworkflow.Run{},
	}
}

func (p *testWorkflowProvider) ApplyDefinition(_ context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	p.applyRequests = append(p.applyRequests, gproto.Clone(req).(*proto.ApplyWorkflowProviderDefinitionRequest))
	spec, err := workflowwire.DefinitionSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, err
	}
	if spec == nil {
		spec = &coreworkflow.DefinitionSpec{}
	}
	id := strings.TrimSpace(spec.ID)
	if id == "" {
		id = fmt.Sprintf("definition-%d", len(p.definitions)+1)
	}
	nextGeneration := int64(1)
	if existing := p.definitions[id]; existing != nil {
		nextGeneration = existing.Generation + 1
	}
	definition := &coreworkflow.Definition{
		ID:          id,
		Generation:  nextGeneration,
		Target:      spec.Target,
		Activations: spec.Activations,
		Paused:      spec.Paused,
		CreatedBy:   appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
		RunAs:       spec.RunAs,
	}
	p.definitions[id] = definition
	return workflowwire.DefinitionToProto(definition)
}

func (p *testWorkflowProvider) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	copied := *definition
	return workflowwire.DefinitionToProto(&copied)
}

func (p *testWorkflowProvider) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	out := &proto.ListWorkflowProviderDefinitionsResponse{}
	for _, definition := range p.definitions {
		pb, err := workflowwire.DefinitionToProto(definition)
		if err != nil {
			return nil, err
		}
		out.Definitions = append(out.Definitions, pb)
	}
	return out, nil
}

func (p *testWorkflowProvider) SetDefinitionPaused(_ context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	definition.Paused = req.GetPaused()
	return workflowwire.DefinitionToProto(definition)
}

func (p *testWorkflowProvider) SetActivationPaused(_ context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	for i := range definition.Activations {
		if definition.Activations[i].ID == strings.TrimSpace(req.GetActivationId()) {
			definition.Activations[i].Paused = req.GetPaused()
		}
	}
	return workflowwire.DefinitionToProto(definition)
}

func (p *testWorkflowProvider) DeleteDefinition(_ context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	id := strings.TrimSpace(req.GetDefinitionId())
	if p.definitions[id] == nil {
		return core.ErrNotFound
	}
	delete(p.definitions, id)
	return nil
}

func (p *testWorkflowProvider) StartRun(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	p.startRunRequests = append(p.startRunRequests, gproto.Clone(req).(*proto.StartWorkflowProviderRunRequest))
	return p.startDefinitionRun(req.GetDefinitionId(), req.GetExpectedDefinitionGeneration(), req.GetWorkflowKey(), protoutil.MapFromStruct(req.GetInput()), appaccessservice.SubjectIDFromRequestContext(req.GetContext()))
}

func (p *testWorkflowProvider) SignalOrStartRun(_ context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	p.signalOrStartRequests = append(p.signalOrStartRequests, gproto.Clone(req).(*proto.SignalOrStartWorkflowProviderRunRequest))
	runProto, err := p.startDefinitionRun(req.GetDefinitionId(), req.GetExpectedDefinitionGeneration(), req.GetWorkflowKey(), protoutil.MapFromStruct(req.GetInput()), appaccessservice.SubjectIDFromRequestContext(req.GetContext()))
	if err != nil {
		return nil, err
	}
	run, err := workflowwire.RunFromProto(runProto)
	if err != nil {
		return nil, err
	}
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

func (p *testWorkflowProvider) startDefinitionRun(definitionID string, generation int64, workflowKey string, input map[string]any, createdBySubjectID string) (*proto.WorkflowRun, error) {
	definition := p.definitions[strings.TrimSpace(definitionID)]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	if generation == 0 {
		generation = definition.Generation
	}
	run := &coreworkflow.Run{
		ID:                   fmt.Sprintf("run-%d", len(p.runs)+1),
		Status:               coreworkflow.RunStatusRunning,
		WorkflowKey:          strings.TrimSpace(workflowKey),
		Target:               definition.Target,
		DefinitionID:         definition.ID,
		DefinitionGeneration: generation,
		Input:                input,
		CreatedBy:            createdBySubjectID,
	}
	p.runs[run.ID] = run
	return workflowwire.RunToProto(run)
}

func (p *testWorkflowProvider) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	copied := *run
	return workflowwire.RunToProto(&copied)
}

func (p *testWorkflowProvider) ListRuns(_ context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	p.listRunsRequests = append(p.listRunsRequests, gproto.Clone(req).(*proto.ListWorkflowProviderRunsRequest))
	if p.listRunsHandler != nil {
		return p.listRunsHandler(req)
	}
	out := &proto.ListWorkflowProviderRunsResponse{}
	for _, run := range p.runs {
		pb, err := workflowwire.RunToProto(run)
		if err != nil {
			return nil, err
		}
		out.Runs = append(out.Runs, pb)
	}
	return out, nil
}

func (p *testWorkflowProvider) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return &proto.GetWorkflowProviderRunEventsResponse{}, nil
}

func (p *testWorkflowProvider) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return &proto.GetWorkflowProviderRunOutputResponse{}, nil
}

func (p *testWorkflowProvider) CancelRun(_ context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	run.Status = coreworkflow.RunStatusCanceled
	return workflowwire.RunToProto(run)
}

func (p *testWorkflowProvider) SignalRun(_ context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	run := p.runs[strings.TrimSpace(req.GetRunId())]
	if run == nil {
		return nil, core.ErrNotFound
	}
	signal := workflowwire.SignalFromProto(req.GetSignal())
	return workflowwire.SignalRunResponseToProto(&coreworkflow.SignalRunResponse{
		Run:         run,
		Signal:      signal,
		WorkflowKey: run.WorkflowKey,
	})
}

func (p *testWorkflowProvider) DeliverEvent(_ context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	p.deliveredEvents = append(p.deliveredEvents, gproto.Clone(req).(*proto.DeliverWorkflowProviderEventRequest))
	return gproto.Clone(req.GetEvent()).(*proto.WorkflowEvent), nil
}

func (p *testWorkflowProvider) Ping(context.Context) error { return nil }

func (p *testWorkflowProvider) Close() error { return nil }

func TestRunMatchesListFiltersEmptyTargetUsesDefinitionOwnership(t *testing.T) {
	t.Parallel()
	run := &coreworkflow.Run{
		ID:           "list-summary",
		DefinitionID: "app_ai-spend-tracker_ai_spend_tracker_sync_every_four_hours",
		Target:       coreworkflow.Target{},
		Status:       coreworkflow.RunStatusSucceeded,
	}
	if !runMatchesListFilters(run, coreworkflow.ListRunsRequest{TargetApp: "ai-spend-tracker"}) {
		t.Fatal("empty-target list summary should match via app-owned definition id")
	}
	if runMatchesListFilters(run, coreworkflow.ListRunsRequest{TargetApp: "ci-cd"}) {
		t.Fatal("empty-target list summary should not match a different app")
	}
}

func TestRunMatchesListFiltersKnownAppsDisambiguatesPrefixCollision(t *testing.T) {
	t.Parallel()
	run := &coreworkflow.Run{
		ID:           "list-summary",
		DefinitionID: "app_foo_bar_daily_sync",
		Target:       coreworkflow.Target{},
		Status:       coreworkflow.RunStatusSucceeded,
	}
	known := []string{"foo", "foo_bar"}
	if runMatchesListFilters(run, coreworkflow.ListRunsRequest{TargetApp: "foo", KnownApps: known}) {
		t.Fatal("prefix collision should not match shorter app when longer owner is known")
	}
	if !runMatchesListFilters(run, coreworkflow.ListRunsRequest{TargetApp: "foo_bar", KnownApps: known}) {
		t.Fatal("prefix collision should match longest known app owner")
	}
}

func TestManagerListRunsForwardsAggregatesAndKnownApps(t *testing.T) {
	t.Parallel()
	provider := newTestWorkflowProvider()
	total := int64(42)
	provider.listRunsHandler = func(req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
		run := &coreworkflow.Run{
			ID:           "run-1",
			DefinitionID: "app_foo_daily",
			Status:       coreworkflow.RunStatusRunning,
		}
		pb, err := workflowwire.RunToProto(run)
		if err != nil {
			return nil, err
		}
		return &proto.ListWorkflowProviderRunsResponse{
			Runs:          []*proto.WorkflowRun{pb},
			NextPageToken: "provider-page-2",
			TotalCount:    &total,
			StatusCounts: &proto.WorkflowRunStatusCounts{
				Running:   2,
				Succeeded: 40,
			},
		}, nil
	}
	manager := New(Config{
		Providers: testWorkflowGithubProviders(t),
		Workflow:  testWorkflowControl{provider: provider},
		Invoker:   testWorkflowManagerBroker(t, testWorkflowGithubProviders(t), allowAllAuthz{}),
		AppNames:  []string{"foo", "foo_bar"},
	})
	resp, err := manager.ListRuns(context.Background(), testWorkflowManagerPrincipal(), "local", coreworkflow.ListRunsRequest{
		PageSize:  1,
		TargetApp: "foo",
	})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if resp.TotalCount == nil || *resp.TotalCount != 42 {
		t.Fatalf("TotalCount = %v, want 42", resp.TotalCount)
	}
	if resp.StatusCounts == nil || resp.StatusCounts.GetRunning() != 2 || resp.StatusCounts.GetSucceeded() != 40 {
		t.Fatalf("StatusCounts = %#v, want running=2 succeeded=40", resp.StatusCounts)
	}
	if resp.NextPageToken == "" {
		t.Fatal("expected manager next page token")
	}
	if len(provider.listRunsRequests) != 1 {
		t.Fatalf("provider ListRuns calls = %d, want 1", len(provider.listRunsRequests))
	}
	gotKnown := provider.listRunsRequests[0].GetKnownApps()
	if len(gotKnown) != 2 || gotKnown[0] != "foo" || gotKnown[1] != "foo_bar" {
		t.Fatalf("KnownApps = %#v, want [foo foo_bar]", gotKnown)
	}
	if got := provider.listRunsRequests[0].GetTargetApp(); got != "foo" {
		t.Fatalf("TargetApp = %q, want foo", got)
	}

	// Continuation page must echo aggregates from the manager page token without
	// requiring the provider to resend them on a non-empty provider token.
	provider.listRunsHandler = func(req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
		if strings.TrimSpace(req.GetPageToken()) == "" {
			t.Fatal("page 2 should resume with a non-empty provider page token")
		}
		run := &coreworkflow.Run{
			ID:           "run-2",
			DefinitionID: "app_foo_daily",
			Status:       coreworkflow.RunStatusSucceeded,
		}
		pb, err := workflowwire.RunToProto(run)
		if err != nil {
			return nil, err
		}
		return &proto.ListWorkflowProviderRunsResponse{Runs: []*proto.WorkflowRun{pb}}, nil
	}
	page2, err := manager.ListRuns(context.Background(), testWorkflowManagerPrincipal(), "local", coreworkflow.ListRunsRequest{
		PageSize:  1,
		PageToken: resp.NextPageToken,
		TargetApp: "foo",
	})
	if err != nil {
		t.Fatalf("ListRuns page 2: %v", err)
	}
	if page2.TotalCount == nil || *page2.TotalCount != 42 {
		t.Fatalf("page 2 TotalCount = %v, want 42", page2.TotalCount)
	}
	if page2.StatusCounts == nil || page2.StatusCounts.GetRunning() != 2 || page2.StatusCounts.GetSucceeded() != 40 {
		t.Fatalf("page 2 StatusCounts = %#v, want running=2 succeeded=40", page2.StatusCounts)
	}
}

func TestCloneWorkflowRunStatusCountsDoesNotCopyLocks(t *testing.T) {
	t.Parallel()
	src := &proto.WorkflowRunStatusCounts{Pending: 1, Failed: 3}
	got := cloneWorkflowRunStatusCounts(src)
	if got == nil || got.GetPending() != 1 || got.GetFailed() != 3 {
		t.Fatalf("clone = %#v", got)
	}
	got.Pending = 9
	if src.GetPending() != 1 {
		t.Fatal("clone must not alias source fields")
	}
	if cloneWorkflowRunStatusCounts(nil) != nil {
		t.Fatal("nil clone should stay nil")
	}
}
