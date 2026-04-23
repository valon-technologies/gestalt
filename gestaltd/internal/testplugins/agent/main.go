package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentProvider struct {
	proto.UnimplementedAgentProviderServer

	mu   sync.Mutex
	runs map[string]*proto.BoundAgentRun
}

func newAgentProvider() *agentProvider {
	return &agentProvider{runs: make(map[string]*proto.BoundAgentRun)}
}

func (p *agentProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *agentProvider) StartRun(ctx context.Context, req *proto.StartAgentProviderRunRequest) (*proto.BoundAgentRun, error) {
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		runID = "agent-run-1"
	}

	output := map[string]any{
		"provider_name": req.GetProviderName(),
	}

	if len(req.GetTools()) > 0 {
		host, err := gestalt.AgentHost()
		if err != nil {
			output["host_error"] = err.Error()
		} else {
			defer func() {
				if closeErr := host.Close(); closeErr != nil && output["host_error"] == nil {
					output["host_error"] = closeErr.Error()
				}
			}()

			arguments, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
			if err != nil {
				return nil, fmt.Errorf("build tool arguments: %w", err)
			}
			resp, err := host.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
				RunId:      runID,
				ToolCallId: "call-1",
				ToolId:     req.GetTools()[0].GetId(),
				Arguments:  arguments,
			})
			if err != nil {
				output["tool_error"] = err.Error()
			} else {
				output["tool_status"] = resp.GetStatus()
				output["tool_body"] = resp.GetBody()
			}

			data, err := structpb.NewStruct(map[string]any{"provider_name": req.GetProviderName()})
			if err != nil {
				return nil, fmt.Errorf("build event payload: %w", err)
			}
			if err := host.EmitEvent(ctx, &proto.EmitAgentEventRequest{
				RunId:      runID,
				Type:       "agent.test",
				Visibility: "private",
				Data:       data,
			}); err != nil {
				output["event_error"] = err.Error()
			} else {
				output["event_emitted"] = true
			}
		}
	}

	body, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	now := timestamppb.Now()
	run := &proto.BoundAgentRun{
		Id:           runID,
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
		Status:       proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		OutputText:   string(body),
		SessionRef:   req.GetSessionRef(),
		CreatedBy:    req.GetCreatedBy(),
		CreatedAt:    now,
		StartedAt:    now,
		CompletedAt:  now,
		ExecutionRef: req.GetExecutionRef(),
	}

	p.mu.Lock()
	p.runs[runID] = run
	p.mu.Unlock()
	return run, nil
}

func (p *agentProvider) GetRun(_ context.Context, req *proto.GetAgentProviderRunRequest) (*proto.BoundAgentRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	runID := strings.TrimSpace(req.GetRunId())
	run, ok := p.runs[runID]
	if !ok {
		return nil, status.Error(codes.NotFound, "run not found")
	}
	return run, nil
}

func (p *agentProvider) ListRuns(context.Context, *proto.ListAgentProviderRunsRequest) (*proto.ListAgentProviderRunsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	runs := make([]*proto.BoundAgentRun, 0, len(p.runs))
	for _, run := range p.runs {
		runs = append(runs, run)
	}
	return &proto.ListAgentProviderRunsResponse{Runs: runs}, nil
}

func (p *agentProvider) CancelRun(_ context.Context, req *proto.CancelAgentProviderRunRequest) (*proto.BoundAgentRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	runID := strings.TrimSpace(req.GetRunId())
	run, ok := p.runs[runID]
	if !ok {
		return nil, status.Error(codes.NotFound, "run not found")
	}
	run.Status = proto.AgentRunStatus_AGENT_RUN_STATUS_CANCELED
	return run, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeAgentProvider(ctx, newAgentProvider()); err != nil {
		panic(err)
	}
}
