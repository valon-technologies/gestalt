package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

type workflowHostTransportHarness struct {
	proto.UnimplementedWorkflowHostServer

	mu       sync.Mutex
	requests []*proto.InvokeWorkflowOperationRequest
	tokens   []string
}

func (h *workflowHostTransportHarness) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.InvokeWorkflowOperationRequest))
	h.mu.Unlock()

	return &proto.InvokeWorkflowOperationResponse{
		Status: 202,
		Body:   req.GetRunId() + ":" + req.GetTarget().GetPlugin().GetOperation(),
	}, nil
}

func TestTransport_WorkflowHostTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowHostTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowHostSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowHostSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowHost()
	if err != nil {
		t.Fatalf("WorkflowHost: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		RunId: "run-1",
		Target: &proto.BoundWorkflowTarget{
			Kind: &proto.BoundWorkflowTarget_Plugin{
				Plugin: &proto.BoundWorkflowPluginTarget{
					PluginName: "roadmap",
					Operation:  "sync_items",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("InvokeOperation: %v", err)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != "run-1:sync_items" {
		t.Fatalf("response = %#v", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("invoke requests len = %d, want 1", len(harness.requests))
	}
	got := harness.requests[0]
	if got.GetRunId() != "run-1" || got.GetTarget().GetPlugin().GetPluginName() != "roadmap" || got.GetTarget().GetPlugin().GetOperation() != "sync_items" {
		t.Fatalf("invoke request = %#v", got)
	}
}
