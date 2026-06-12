package client_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// recordingWorkflowProvider implements a handful of handler methods and
// inherits Unimplemented defaults for the rest.
type recordingWorkflowProvider struct {
	client.UnimplementedWorkflowProvider

	applied *client.ApplyWorkflowProviderDefinitionRequest
	deleted *client.DeleteWorkflowProviderDefinitionRequest
}

func (p *recordingWorkflowProvider) ApplyDefinition(_ context.Context, request *client.ApplyWorkflowProviderDefinitionRequest) (*client.WorkflowDefinition, error) {
	p.applied = request
	return &client.WorkflowDefinition{
		Id:                 "def-1",
		Generation:         3,
		Paused:             true,
		CreatedBySubjectId: request.RequestedBySubjectId,
	}, nil
}

func (p *recordingWorkflowProvider) DeleteDefinition(_ context.Context, request *client.DeleteWorkflowProviderDefinitionRequest) error {
	p.deleted = request
	return nil
}

func (p *recordingWorkflowProvider) GetRun(_ context.Context, request *client.GetWorkflowProviderRunRequest) (*client.WorkflowRun, error) {
	return nil, &client.GestaltError{Code: client.GestaltErrorCodeNotFound, Message: "no run " + request.RunId}
}

func (p *recordingWorkflowProvider) GetRunEvents(_ context.Context, request *client.GetWorkflowProviderRunEventsRequest) (*client.GetWorkflowProviderRunEventsResponse, error) {
	return &client.GetWorkflowProviderRunEventsResponse{
		Events: []*client.WorkflowRunEvent{
			{Id: "ev-1", RunId: request.RunId, Type: "step_started"},
			{Id: "ev-2", RunId: request.RunId, Type: "step_completed"},
		},
	}, nil
}

// TestWorkflowProviderServerTransport round-trips the generated loop: the
// generated Workflow client calls the generated provider adapter serving a
// native handler, covering request and response conversion, the
// empty-response path, the unwrap collapse, error mapping, and the
// Unimplemented defaults.
func TestWorkflowProviderServerTransport(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	proto.RegisterWorkflowServer(srv, client.NewWorkflowProviderServer(provider))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	workflow := client.NewWorkflow(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	definition, err := workflow.ApplyDefinitionRaw(ctx, &client.ApplyWorkflowProviderDefinitionRequest{
		ProviderName:         "temporal",
		IdempotencyKey:       "idem-1",
		RequestedBySubjectId: "sub-1",
		Context:              &client.RequestContext{Subject: &client.SubjectContext{Id: "sub-1", Email: "a@b.c"}},
	})
	if err != nil {
		t.Fatalf("ApplyDefinitionRaw: %v", err)
	}
	if definition.Id != "def-1" || definition.Generation != 3 || !definition.Paused || definition.CreatedBySubjectId != "sub-1" {
		t.Fatalf("ApplyDefinitionRaw response = %+v", definition)
	}
	if provider.applied.ProviderName != "temporal" || provider.applied.IdempotencyKey != "idem-1" {
		t.Fatalf("handler request = %+v", provider.applied)
	}
	if provider.applied.Context == nil || provider.applied.Context.Subject == nil || provider.applied.Context.Subject.Email != "a@b.c" {
		t.Fatalf("handler request context = %+v", provider.applied.Context)
	}

	if err := workflow.DeleteDefinition(ctx, "def-9"); err != nil {
		t.Fatalf("DeleteDefinition: %v", err)
	}
	if provider.deleted == nil || provider.deleted.DefinitionId != "def-9" {
		t.Fatalf("deleted = %+v", provider.deleted)
	}

	events, err := workflow.GetRunEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRunEvents: %v", err)
	}
	if len(events) != 2 || events[0].Id != "ev-1" || events[1].RunId != "run-1" {
		t.Fatalf("GetRunEvents = %+v", events)
	}

	_, err = workflow.GetRun(ctx, "run-404")
	var gerr *client.GestaltError
	if !errors.As(err, &gerr) || gerr.Code != client.GestaltErrorCodeNotFound || gerr.Message != "no run run-404" {
		t.Fatalf("GetRun error = %v", err)
	}

	_, err = workflow.CancelRun(ctx, "run-1", "because")
	if !errors.As(err, &gerr) || gerr.Code != client.GestaltErrorCodeUnimplemented || gerr.Message != "workflow cancel run is not implemented" {
		t.Fatalf("CancelRun error = %v", err)
	}
}
