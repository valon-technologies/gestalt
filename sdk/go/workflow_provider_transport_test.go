package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type fullWorkflowProvider struct {
	gestalt.UnimplementedWorkflowProvider
	closeTracker
	configuredName          string
	startRunInvocationToken string
	definitionToken         string
	deliverEventToken       string
	deliveredEvents         []string
}

func (p *fullWorkflowProvider) Configure(_ context.Context, name string, _ map[string]any) error {
	p.configuredName = name
	return nil
}

func (p *fullWorkflowProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindWorkflow,
		Name:        "stub-workflow",
		DisplayName: "Stub Workflow",
		Version:     "1.0",
	}
}

func (p *fullWorkflowProvider) StartRun(ctx context.Context, req *gestalt.StartWorkflowProviderRunRequest) (*gestalt.WorkflowRun, error) {
	p.startRunInvocationToken = gestalt.InvocationTokenFromContext(ctx)
	return &gestalt.WorkflowRun{
		ID:                   req.IdempotencyKey,
		Status:               gestalt.WorkflowRunStatusValuePending,
		DefinitionID:         req.DefinitionID,
		DefinitionGeneration: req.ExpectedDefinitionGeneration,
		Input:                req.Input,
		WorkflowKey:          req.WorkflowKey,
	}, nil
}

func (p *fullWorkflowProvider) ApplyDefinition(ctx context.Context, req *gestalt.ApplyWorkflowProviderDefinitionRequest) (*gestalt.WorkflowDefinition, error) {
	p.definitionToken = gestalt.InvocationTokenFromContext(ctx)
	spec := req.Spec
	if spec == nil {
		spec = &gestalt.WorkflowDefinitionSpec{}
	}
	return &gestalt.WorkflowDefinition{
		ID:          spec.ID,
		Generation:  2,
		Target:      spec.Target,
		Activations: spec.Activations,
		Paused:      spec.Paused,
	}, nil
}

func (p *fullWorkflowProvider) DeliverEvent(ctx context.Context, req *gestalt.DeliverWorkflowProviderEventRequest) (*gestalt.WorkflowEvent, error) {
	p.deliverEventToken = gestalt.InvocationTokenFromContext(ctx)
	if req.Event != nil {
		p.deliveredEvents = append(p.deliveredEvents, req.Event.Type)
		return &gestalt.WorkflowEvent{ID: "delivered-go", Type: req.Event.Type}, nil
	}
	return &gestalt.WorkflowEvent{ID: "delivered-go"}, nil
}

func TestWorkflowProviderTypedTransportRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "workflow.sock")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(proto.EnvProviderName, "workflow-test")

	ctx, cancel := context.WithCancel(context.Background())
	provider := &fullWorkflowProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeWorkflowProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("workflow provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewProviderLifecycleClient(conn)
	workflowClient := proto.NewWorkflowProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := runtimeClient.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_WORKFLOW {
		t.Fatalf("kind = %v, want WORKFLOW", meta.GetKind())
	}

	definition, err := workflowClient.ApplyDefinition(rpcCtx, &proto.ApplyWorkflowProviderDefinitionRequest{
		IdempotencyKey:  "definition-1",
		InvocationToken: "workflow-definition-token",
		Spec: &proto.WorkflowDefinitionSpec{
			Id: "definition-1",
			Target: &proto.BoundWorkflowTarget{
				Steps: []*proto.WorkflowStep{{
					Id: "search_issues",
					Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
						Name:      "github",
						Operation: "issues.search",
					}},
				}},
			},
			Activations: []*proto.WorkflowActivation{{
				Id: "github_issue",
				Trigger: &proto.WorkflowActivation_Event{Event: &proto.WorkflowEventActivation{
					Match: &proto.WorkflowEventMatch{Type: "github.issue", Source: "github"},
				}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if definition.GetId() != "definition-1" || definition.GetGeneration() != 2 {
		t.Fatalf("ApplyDefinition = %#v", definition)
	}
	if provider.definitionToken != "workflow-definition-token" {
		t.Fatalf("ApplyDefinition invocation token = %q, want workflow-definition-token", provider.definitionToken)
	}

	input, err := structpb.NewStruct(map[string]any{"issue": map[string]any{"number": 42}})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	run, err := workflowClient.StartRun(rpcCtx, &proto.StartWorkflowProviderRunRequest{
		IdempotencyKey:               "run-1",
		InvocationToken:              "workflow-run-token",
		DefinitionId:                 "definition-1",
		ExpectedDefinitionGeneration: 2,
		WorkflowKey:                  "github:issue:42",
		Input:                        input,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.GetId() != "run-1" || run.GetStatus() != proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING {
		t.Fatalf("StartRun = %+v, want pending run", run)
	}
	if run.GetDefinitionId() != "definition-1" || run.GetDefinitionGeneration() != 2 {
		t.Fatalf("run definition = %q/%d", run.GetDefinitionId(), run.GetDefinitionGeneration())
	}
	if provider.startRunInvocationToken != "workflow-run-token" {
		t.Fatalf("StartRun invocation token = %q, want workflow-run-token", provider.startRunInvocationToken)
	}

	delivered, err := workflowClient.DeliverEvent(rpcCtx, &proto.DeliverWorkflowProviderEventRequest{
		Event:           &proto.WorkflowEvent{Type: "issue.created"},
		InvocationToken: "workflow-event-token",
	})
	if err != nil {
		t.Fatalf("DeliverEvent: %v", err)
	}
	if delivered.GetId() != "delivered-go" {
		t.Fatalf("DeliverEvent id = %q, want delivered-go", delivered.GetId())
	}
	if len(provider.deliveredEvents) != 1 || provider.deliveredEvents[0] != "issue.created" {
		t.Fatalf("delivered events = %v, want [issue.created]", provider.deliveredEvents)
	}
	if provider.deliverEventToken != "workflow-event-token" {
		t.Fatalf("DeliverEvent invocation token = %q, want workflow-event-token", provider.deliverEventToken)
	}
}
