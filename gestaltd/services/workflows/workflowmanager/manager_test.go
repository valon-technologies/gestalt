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
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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
	return New(Config{
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
}

func TestApplyDefinitionAndStartRunUseDefinitionGenerationAndInput(t *testing.T) {
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
	requireWorkflowManagerRequestContext(t, provider.startRunRequests[0].GetContext(), invocation.ProviderKindApp, "github")
}

func TestSignalOrStartRunRequiresDefinitionAndCarriesInput(t *testing.T) {
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
	requireWorkflowManagerRequestContext(t, provider.signalOrStartRequests[0].GetContext(), invocation.ProviderKindApp, "github")
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
		AppName: " github ",
		Event:   coreworkflow.Event{Type: "issue.updated"},
	}); err != nil {
		t.Fatalf("DeliverEvent fan-out: %v", err)
	}
	if len(provider.deliveredEvents) != 2 {
		t.Fatalf("delivered events = %d, want 2", len(provider.deliveredEvents))
	}
	for i, req := range provider.deliveredEvents {
		if req.GetAppName() != "github" {
			t.Fatalf("deliveredEvents[%d].AppName = %q, want github", i, req.GetAppName())
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

func (c testWorkflowControl) ProviderNames() []string {
	return []string{"local"}
}

type testWorkflowProvider struct {
	definitions           map[string]*coreworkflow.Definition
	runs                  map[string]*coreworkflow.Run
	applyRequests         []*proto.ApplyWorkflowProviderDefinitionRequest
	startRunRequests      []*proto.StartWorkflowProviderRunRequest
	signalOrStartRequests []*proto.SignalOrStartWorkflowProviderRunRequest
	deliveredEvents       []*proto.DeliverWorkflowProviderEventRequest
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
		ID:                 id,
		Generation:         nextGeneration,
		Target:             spec.Target,
		Activations:        spec.Activations,
		Paused:             spec.Paused,
		CreatedBySubjectID: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
		RunAs:              spec.RunAs,
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
		CreatedBySubjectID:   createdBySubjectID,
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

func (p *testWorkflowProvider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
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
