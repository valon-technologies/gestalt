package workflows

import (
	"context"
	"net"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

type recordingWorkflowProviderServer struct {
	proto.UnimplementedWorkflowProviderServer
	proto.UnimplementedProviderLifecycleServer

	tokens map[string]string
}

func (s *recordingWorkflowProviderServer) record(name, token string) {
	if s.tokens == nil {
		s.tokens = map[string]string{}
	}
	s.tokens[name] = token
}

func (s *recordingWorkflowProviderServer) GetProviderIdentity(context.Context, *emptypb.Empty) (*proto.ProviderIdentity, error) {
	return &proto.ProviderIdentity{
		Kind:               proto.ProviderKind_PROVIDER_KIND_WORKFLOW,
		Name:               "recording",
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *recordingWorkflowProviderServer) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *recordingWorkflowProviderServer) ApplyDefinition(_ context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	s.record("ApplyDefinition", req.GetInvocationToken())
	return &proto.WorkflowDefinition{Id: req.GetSpec().GetId(), Target: req.GetSpec().GetTarget()}, nil
}

func (s *recordingWorkflowProviderServer) StartRun(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	s.record("StartRun", req.GetInvocationToken())
	return &proto.WorkflowRun{Id: "run-1", Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING}, nil
}

func (s *recordingWorkflowProviderServer) DeliverEvent(_ context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	s.record("DeliverEvent", req.GetInvocationToken())
	return &proto.WorkflowEvent{Id: "event-1", Type: req.GetEvent().GetType()}, nil
}

func TestRemoteWorkflowForwardsRestoredInvocationToken(t *testing.T) {
	t.Parallel()

	manager, err := appaccessservice.NewInvocationTokenManager([]byte("workflow-provider-token-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token, err := manager.MintRootToken(context.Background(), "caller", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	tokenCtx, err := manager.ResolveToken(token, "caller")
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	ctx := appaccessservice.RestoreTokenContext(context.Background(), tokenCtx, "")

	workflow, server := newRecordingRemoteWorkflow(t)
	target := &proto.BoundWorkflowTarget{Steps: []*proto.WorkflowStep{{
		Id:     "call_app",
		Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{Name: "slack", Operation: "chat.postMessage"}},
	}}}

	calls := map[string]func() error{
		"ApplyDefinition": func() error {
			_, err := workflow.ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{
				Spec: &proto.WorkflowDefinitionSpec{Id: "definition-1", Target: target},
			})
			return err
		},
		"StartRun": func() error {
			_, err := workflow.StartRun(ctx, &proto.StartWorkflowProviderRunRequest{DefinitionId: "definition-1"})
			return err
		},
		"DeliverEvent": func() error {
			_, err := workflow.DeliverEvent(ctx, &proto.DeliverWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{Type: "issue.created"}})
			return err
		},
	}

	for name, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name := range calls {
		if got := server.tokens[name]; got != token {
			t.Fatalf("%s invocation token = %q, want restored token", name, got)
		}
	}
}

func newRecordingRemoteWorkflow(t *testing.T) (coreworkflow.Provider, *recordingWorkflowProviderServer) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	provider := &recordingWorkflowProviderServer{}
	proto.RegisterWorkflowProviderServer(srv, provider)
	proto.RegisterProviderLifecycleServer(srv, provider)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///workflow-provider",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	workflow, err := NewRemote(context.Background(), RemoteConfig{
		Client:  proto.NewWorkflowProviderClient(conn),
		Runtime: proto.NewProviderLifecycleClient(conn),
		Closer:  noopCloser{},
		Name:    "recording",
	})
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	return workflow, provider
}
