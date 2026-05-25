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

func (s *recordingWorkflowProviderServer) StartRun(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	s.record("StartRun", req.GetInvocationToken())
	return &proto.BoundWorkflowRun{Id: "run-1", Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING}, nil
}

func (s *recordingWorkflowProviderServer) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	s.record("GetRun", req.GetInvocationToken())
	return &proto.BoundWorkflowRun{Id: req.GetRunId(), Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED}, nil
}

func (s *recordingWorkflowProviderServer) ListRuns(_ context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	s.record("ListRuns", req.GetInvocationToken())
	return &proto.ListWorkflowProviderRunsResponse{}, nil
}

func (s *recordingWorkflowProviderServer) CancelRun(_ context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	s.record("CancelRun", req.GetInvocationToken())
	return &proto.BoundWorkflowRun{Id: req.GetRunId(), Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED}, nil
}

func (s *recordingWorkflowProviderServer) SignalRun(_ context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	s.record("SignalRun", req.GetInvocationToken())
	return &proto.SignalWorkflowRunResponse{Run: &proto.BoundWorkflowRun{Id: req.GetRunId()}}, nil
}

func (s *recordingWorkflowProviderServer) SignalOrStartRun(_ context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	s.record("SignalOrStartRun", req.GetInvocationToken())
	return &proto.SignalWorkflowRunResponse{Run: &proto.BoundWorkflowRun{Id: "run-1"}}, nil
}

func (s *recordingWorkflowProviderServer) UpsertSchedule(_ context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	s.record("UpsertSchedule", req.GetInvocationToken())
	return &proto.BoundWorkflowSchedule{Id: "schedule-1"}, nil
}

func (s *recordingWorkflowProviderServer) GetSchedule(_ context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	s.record("GetSchedule", req.GetInvocationToken())
	return &proto.BoundWorkflowSchedule{Id: req.GetScheduleId()}, nil
}

func (s *recordingWorkflowProviderServer) ListSchedules(_ context.Context, req *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	s.record("ListSchedules", req.GetInvocationToken())
	return &proto.ListWorkflowProviderSchedulesResponse{}, nil
}

func (s *recordingWorkflowProviderServer) DeleteSchedule(_ context.Context, req *proto.DeleteWorkflowProviderScheduleRequest) (*emptypb.Empty, error) {
	s.record("DeleteSchedule", req.GetInvocationToken())
	return &emptypb.Empty{}, nil
}

func (s *recordingWorkflowProviderServer) PauseSchedule(_ context.Context, req *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	s.record("PauseSchedule", req.GetInvocationToken())
	return &proto.BoundWorkflowSchedule{Id: req.GetScheduleId()}, nil
}

func (s *recordingWorkflowProviderServer) ResumeSchedule(_ context.Context, req *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	s.record("ResumeSchedule", req.GetInvocationToken())
	return &proto.BoundWorkflowSchedule{Id: req.GetScheduleId()}, nil
}

func (s *recordingWorkflowProviderServer) UpsertEventTrigger(_ context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	s.record("UpsertEventTrigger", req.GetInvocationToken())
	return &proto.BoundWorkflowEventTrigger{Id: "trigger-1"}, nil
}

func (s *recordingWorkflowProviderServer) GetEventTrigger(_ context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	s.record("GetEventTrigger", req.GetInvocationToken())
	return &proto.BoundWorkflowEventTrigger{Id: req.GetTriggerId()}, nil
}

func (s *recordingWorkflowProviderServer) ListEventTriggers(_ context.Context, req *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	s.record("ListEventTriggers", req.GetInvocationToken())
	return &proto.ListWorkflowProviderEventTriggersResponse{}, nil
}

func (s *recordingWorkflowProviderServer) DeleteEventTrigger(_ context.Context, req *proto.DeleteWorkflowProviderEventTriggerRequest) (*emptypb.Empty, error) {
	s.record("DeleteEventTrigger", req.GetInvocationToken())
	return &emptypb.Empty{}, nil
}

func (s *recordingWorkflowProviderServer) PauseEventTrigger(_ context.Context, req *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	s.record("PauseEventTrigger", req.GetInvocationToken())
	return &proto.BoundWorkflowEventTrigger{Id: req.GetTriggerId()}, nil
}

func (s *recordingWorkflowProviderServer) ResumeEventTrigger(_ context.Context, req *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	s.record("ResumeEventTrigger", req.GetInvocationToken())
	return &proto.BoundWorkflowEventTrigger{Id: req.GetTriggerId()}, nil
}

func (s *recordingWorkflowProviderServer) PublishEvent(_ context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	s.record("PublishEvent", req.GetInvocationToken())
	return &proto.WorkflowEvent{Id: "event-1", Type: req.GetEvent().GetType()}, nil
}

func TestRemoteWorkflowForwardsInvocationToken(t *testing.T) {
	t.Parallel()

	workflow, server := newRecordingRemoteWorkflow(t)
	ctx := appaccessservice.WithInvocationToken(context.Background(), "workflow-provider-token")
	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID:  "call_app",
		App: &coreworkflow.AppCall{Name: "slack", Operation: "chat.postMessage"},
	}}}

	calls := map[string]func() error{
		"StartRun": func() error {
			_, err := workflow.StartRun(ctx, coreworkflow.StartRunRequest{Target: target})
			return err
		},
		"GetRun": func() error {
			_, err := workflow.GetRun(ctx, coreworkflow.GetRunRequest{RunID: "run-1"})
			return err
		},
		"ListRuns": func() error {
			_, err := workflow.ListRuns(ctx, coreworkflow.ListRunsRequest{})
			return err
		},
		"CancelRun": func() error {
			_, err := workflow.CancelRun(ctx, coreworkflow.CancelRunRequest{RunID: "run-1"})
			return err
		},
		"SignalRun": func() error {
			_, err := workflow.SignalRun(ctx, coreworkflow.SignalRunRequest{RunID: "run-1", Signal: coreworkflow.Signal{Name: "wake"}})
			return err
		},
		"SignalOrStartRun": func() error {
			_, err := workflow.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{Target: target, Signal: coreworkflow.Signal{Name: "wake"}})
			return err
		},
		"UpsertSchedule": func() error {
			_, err := workflow.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{Target: target})
			return err
		},
		"GetSchedule": func() error {
			_, err := workflow.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: "schedule-1"})
			return err
		},
		"ListSchedules": func() error {
			_, err := workflow.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{})
			return err
		},
		"DeleteSchedule": func() error {
			return workflow.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{ScheduleID: "schedule-1"})
		},
		"PauseSchedule": func() error {
			_, err := workflow.PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{ScheduleID: "schedule-1"})
			return err
		},
		"ResumeSchedule": func() error {
			_, err := workflow.ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{ScheduleID: "schedule-1"})
			return err
		},
		"UpsertEventTrigger": func() error {
			_, err := workflow.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{Target: target})
			return err
		},
		"GetEventTrigger": func() error {
			_, err := workflow.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		},
		"ListEventTriggers": func() error {
			_, err := workflow.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{})
			return err
		},
		"DeleteEventTrigger": func() error {
			return workflow.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{TriggerID: "trigger-1"})
		},
		"PauseEventTrigger": func() error {
			_, err := workflow.PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		},
		"ResumeEventTrigger": func() error {
			_, err := workflow.ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		},
		"PublishEvent": func() error {
			_, err := workflow.PublishEvent(ctx, coreworkflow.PublishEventRequest{Event: coreworkflow.Event{Type: "issue.created"}})
			return err
		},
	}

	for name, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name := range calls {
		if got := server.tokens[name]; got != "workflow-provider-token" {
			t.Fatalf("%s invocation token = %q, want workflow-provider-token", name, got)
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
