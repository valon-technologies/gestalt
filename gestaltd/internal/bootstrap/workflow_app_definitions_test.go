package bootstrap

import (
	"context"
	"strings"
	"testing"

	gproto "google.golang.org/protobuf/proto"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type fakeWorkflowProvider struct {
	definitions        map[string]*coreworkflow.Definition
	appliedDefinitions []*proto.ApplyWorkflowProviderDefinitionRequest
	deletedDefinitions []*proto.DeleteWorkflowProviderDefinitionRequest
}

func (p *fakeWorkflowProvider) ApplyDefinition(_ context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	p.appliedDefinitions = append(p.appliedDefinitions, gproto.Clone(req).(*proto.ApplyWorkflowProviderDefinitionRequest))
	spec, err := workflowwire.DefinitionSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, err
	}
	if p.definitions == nil {
		p.definitions = map[string]*coreworkflow.Definition{}
	}
	id := strings.TrimSpace(spec.ID)
	definition := &coreworkflow.Definition{
		ID:           id,
		Generation:   1,
		Target:       spec.Target,
		Activations:  append([]coreworkflow.Activation(nil), spec.Activations...),
		Paused:       spec.Paused,
		CreatedBy:    workflowConfigOwnerSubjectID(),
		ProviderName: req.GetProvider(),
		RunAs:        spec.RunAs,
	}
	if existing := p.definitions[id]; existing != nil {
		definition.Generation = existing.Generation + 1
	}
	p.definitions[id] = definition
	return workflowwire.DefinitionToProto(definition)
}

func (p *fakeWorkflowProvider) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	if definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]; definition != nil {
		return workflowwire.DefinitionToProto(definition)
	}
	return nil, core.ErrNotFound
}

func (p *fakeWorkflowProvider) ListDefinitions(_ context.Context, _ *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	resp := &proto.ListWorkflowProviderDefinitionsResponse{}
	for _, definition := range p.definitions {
		protoDef, err := workflowwire.DefinitionToProto(definition)
		if err != nil {
			return nil, err
		}
		resp.Definitions = append(resp.Definitions, protoDef)
	}
	return resp, nil
}

func (p *fakeWorkflowProvider) DeleteDefinition(_ context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	p.deletedDefinitions = append(p.deletedDefinitions, gproto.Clone(req).(*proto.DeleteWorkflowProviderDefinitionRequest))
	delete(p.definitions, strings.TrimSpace(req.GetDefinitionId()))
	return nil
}

func (p *fakeWorkflowProvider) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) DeliverEvent(context.Context, *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return nil, nil
}
func (p *fakeWorkflowProvider) Ping(context.Context) error { return nil }
func (p *fakeWorkflowProvider) Close() error               { return nil }

func testAppWorkflowSpecProto(localID, runAs, cron string) *proto.WorkflowDefinitionSpec {
	return &proto.WorkflowDefinitionSpec{
		Id:    localID,
		RunAs: runAs,
		Target: &proto.BoundWorkflowTarget{
			Steps: []*proto.WorkflowStep{{
				Id: "sync",
				Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
					Name:      "notes",
					Operation: "sync",
				}},
			}},
		},
		Activations: []*proto.WorkflowActivation{{
			Id: "daily",
			Trigger: &proto.WorkflowActivation_Schedule{Schedule: &proto.WorkflowScheduleActivation{
				Cron:     cron,
				Timezone: "UTC",
			}},
		}},
	}
}

func testWorkflowReconcileEnv(t *testing.T) (*config.Config, *workflowRuntime, *fakeWorkflowProvider, *appWorkflowDeclarations) {
	t.Helper()
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"notes": {ConnectionMode: "none"},
		},
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
		Workflows: config.WorkflowsConfig{
			Definitions: map[string]config.WorkflowDefinitionConfig{
				"backup": {
					Provider: "temporal",
					RunAs:    "service_account:cfg-sa",
					Steps: []config.WorkflowStepConfig{{
						App: &config.WorkflowStepAppCallConfig{Name: "notes", Operation: "sync"},
					}},
				},
			},
		},
	}
	runtime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	runtime.InitProviderPlaceholders(cfg.Providers.Workflow)
	provider := &fakeWorkflowProvider{}
	runtime.PublishProvider("temporal", provider)
	return cfg, runtime, provider, newAppWorkflowDeclarations()
}

func TestReconcileAppWorkflowDefinitions(t *testing.T) {
	t.Parallel()

	cfg, runtime, provider, decls := testWorkflowReconcileEnv(t)
	decls.Set("notes", []*proto.WorkflowDefinitionSpec{
		testAppWorkflowSpecProto("daily-summary", "service_account:sa1", "0 2 * * *"),
	})
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err != nil {
		t.Fatalf("reconcile apply: %v", err)
	}
	if len(provider.appliedDefinitions) != 2 {
		t.Fatalf("applied definitions = %d, want 2 (app + cfg)", len(provider.appliedDefinitions))
	}
	var appApply *proto.ApplyWorkflowProviderDefinitionRequest
	for _, req := range provider.appliedDefinitions {
		if req.GetSpec().GetId() == "app_notes_daily-summary" {
			appApply = req
			break
		}
	}
	if appApply == nil {
		t.Fatal("missing app workflow apply")
	}
	if got := appApply.GetContext().GetSubject().GetId(); got != workflowConfigOwnerSubjectID() {
		t.Fatalf("apply subject = %q, want %q", got, workflowConfigOwnerSubjectID())
	}

	decls.Set("notes", []*proto.WorkflowDefinitionSpec{
		testAppWorkflowSpecProto("daily-summary", "service_account:sa1", "0 3 * * *"),
	})
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err != nil {
		t.Fatalf("reconcile edit: %v", err)
	}
	if len(provider.appliedDefinitions) < 3 {
		t.Fatalf("expected re-apply after edit, got %d applies", len(provider.appliedDefinitions))
	}

	decls.Set("notes", nil)
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err != nil {
		t.Fatalf("reconcile remove: %v", err)
	}
	if len(provider.deletedDefinitions) != 1 || provider.deletedDefinitions[0].GetDefinitionId() != "app_notes_daily-summary" {
		t.Fatalf("deleted after removal = %#v", provider.deletedDefinitions)
	}
	if provider.definitions["cfg_backup"] == nil {
		t.Fatal("cfg_* definition was deleted")
	}

	cfg.Workflows.Definitions = nil
	provider.definitions = map[string]*coreworkflow.Definition{
		"app_notes_old": {ID: "app_notes_old"},
	}
	decls = newAppWorkflowDeclarations()
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err != nil {
		t.Fatalf("reconcile unreported app: %v", err)
	}
	if len(provider.deletedDefinitions) != 1 {
		t.Fatalf("deleted while app unreported = %d, want 1", len(provider.deletedDefinitions))
	}

	delete(cfg.Apps, "notes")
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err != nil {
		t.Fatalf("reconcile removed app: %v", err)
	}
	if len(provider.deletedDefinitions) != 2 || provider.deletedDefinitions[1].GetDefinitionId() != "app_notes_old" {
		t.Fatalf("deleted after app removal = %#v", provider.deletedDefinitions)
	}

	decls.Set("notes", []*proto.WorkflowDefinitionSpec{
		testAppWorkflowSpecProto("daily", "", "0 2 * * *"),
	})
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err == nil {
		t.Fatal("expected validation error for missing run_as")
	}

	for _, tc := range []struct {
		localID string
		want    string
	}{
		{"cfg_backup", "reserved"},
		{"app_foo", "reserved"},
		{"Bad-ID", "must match"},
		{"has space", "must match"},
	} {
		tc := tc
		t.Run("invalid local id "+tc.localID, func(t *testing.T) {
			t.Parallel()
			invalidDecls := newAppWorkflowDeclarations()
			invalidDecls.Set("notes", []*proto.WorkflowDefinitionSpec{
				testAppWorkflowSpecProto(tc.localID, "service_account:sa1", "0 2 * * *"),
			})
			err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, invalidDecls, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("localID=%q err = %v, want substring %q", tc.localID, err, tc.want)
			}
		})
	}

	decls = newAppWorkflowDeclarations()
	decls.Set("a", []*proto.WorkflowDefinitionSpec{
		testAppWorkflowSpecProto("b_c", "service_account:sa1", "0 2 * * *"),
	})
	decls.Set("a_b", []*proto.WorkflowDefinitionSpec{
		testAppWorkflowSpecProto("c", "service_account:sa1", "0 2 * * *"),
	})
	if err := reconcileWorkflowConfigDefinitions(context.Background(), cfg, runtime, decls, nil); err == nil || !strings.Contains(err.Error(), "app_a_b_c") {
		t.Fatalf("duplicate id error = %v", err)
	}
}
