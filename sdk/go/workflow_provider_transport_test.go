package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fullWorkflowProvider struct {
	gestalt.UnimplementedWorkflowProvider
	closeTracker
	configuredName  string
	publishedEvents []string
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

func (p *fullWorkflowProvider) StartRun(_ context.Context, req *gestalt.StartWorkflowProviderRunRequest) (*gestalt.BoundWorkflowRun, error) {
	return &gestalt.BoundWorkflowRun{
		ID:     req.IdempotencyKey,
		Status: gestalt.WorkflowRunStatusValuePending,
		Target: req.Target,
	}, nil
}

func (p *fullWorkflowProvider) PublishEvent(_ context.Context, req *gestalt.PublishWorkflowProviderEventRequest) error {
	if req.Event != nil {
		p.publishedEvents = append(p.publishedEvents, req.Event.Type)
	}
	return nil
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

	run, err := workflowClient.StartRun(rpcCtx, &proto.StartWorkflowProviderRunRequest{
		IdempotencyKey: "run-1",
		Target: &proto.BoundWorkflowTarget{
			Kind: &proto.BoundWorkflowTarget_Plugin{
				Plugin: &proto.BoundWorkflowPluginTarget{
					PluginName: "github",
					Operation:  "issues.create",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.GetId() != "run-1" || run.GetStatus() != proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING {
		t.Fatalf("StartRun = %+v, want pending run", run)
	}

	if _, err := workflowClient.PublishEvent(rpcCtx, &proto.PublishWorkflowProviderEventRequest{
		Event: &proto.WorkflowEvent{Type: "issue.created"},
	}); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(provider.publishedEvents) != 1 || provider.publishedEvents[0] != "issue.created" {
		t.Fatalf("published events = %v, want [issue.created]", provider.publishedEvents)
	}
}
