package sandbox

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/valon-technologies/gestalt/plugins/agentic/sandbox/pb"
)

const (
	healthPollInterval = 200 * time.Millisecond
	healthTimeout      = 30 * time.Second
	shutdownGrace      = 5 * time.Second
)

type Process struct {
	id        string
	cmd       *exec.Cmd
	client    pb.SandboxServiceClient
	conn      *grpc.ClientConn
	sockPath  string
	busy      atomic.Bool
	startedAt time.Time
}

func SpawnProcess(id, pythonCmd, script, toolServiceAddr string) (*Process, error) {
	sockPath := fmt.Sprintf("/tmp/gestalt-sandbox-%s.sock", id)
	_ = os.Remove(sockPath)

	listenAddr := fmt.Sprintf("unix://%s", sockPath)
	cmd := exec.Command(pythonCmd, script, "--listen-addr", listenAddr, "--tool-service-addr", toolServiceAddr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting sandbox process: %w", err)
	}

	p := &Process{
		id:        id,
		cmd:       cmd,
		sockPath:  sockPath,
		startedAt: time.Now(),
	}

	conn, err := grpc.NewClient("unix://"+sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("dialing sandbox: %w", err)
	}
	p.conn = conn
	p.client = pb.NewSandboxServiceClient(conn)

	if err := p.waitForHealth(); err != nil {
		p.cleanup()
		return nil, err
	}

	return p, nil
}

func (p *Process) waitForHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()

	for {
		_, err := p.client.Health(ctx, &pb.HealthRequest{})
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox %s health check timed out", p.id)
		case <-time.After(healthPollInterval):
		}
	}
}

func (p *Process) ID() string   { return p.id }
func (p *Process) IsBusy() bool { return p.busy.Load() }
func (p *Process) SetBusy()     { p.busy.Store(true) }
func (p *Process) SetIdle()     { p.busy.Store(false) }

type ChatEvent struct {
	TextDelta    string
	ToolCallID   string
	ToolName     string
	ToolInput    string
	ToolResult   string
	IsToolError  bool
	FullText     string
	InputTokens  int64
	OutputTokens int64
	Error        string
	Done         bool
}

func (p *Process) Converse(ctx context.Context, req *pb.ConversationRequest) (<-chan ChatEvent, error) {
	stream, err := p.client.Converse(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("calling Converse: %w", err)
	}

	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				ch <- ChatEvent{Error: err.Error(), Done: true}
				return
			}

			switch e := ev.Event.(type) {
			case *pb.ConversationEvent_TextDelta:
				ch <- ChatEvent{TextDelta: e.TextDelta.GetContent()}
			case *pb.ConversationEvent_ToolUse:
				ch <- ChatEvent{
					ToolCallID: e.ToolUse.GetToolCallId(),
					ToolName:   e.ToolUse.GetToolName(),
					ToolInput:  e.ToolUse.GetInputJson(),
				}
			case *pb.ConversationEvent_ToolResult:
				ch <- ChatEvent{
					ToolCallID:  e.ToolResult.GetToolCallId(),
					ToolResult:  e.ToolResult.GetContentJson(),
					IsToolError: e.ToolResult.GetIsError(),
				}
			case *pb.ConversationEvent_TurnComplete:
				ch <- ChatEvent{
					Done:         true,
					FullText:     e.TurnComplete.GetFullText(),
					InputTokens:  e.TurnComplete.GetInputTokens(),
					OutputTokens: e.TurnComplete.GetOutputTokens(),
				}
				return
			case *pb.ConversationEvent_Error:
				ch <- ChatEvent{Error: e.Error.GetMessage(), Done: true}
				return
			}
		}
	}()

	return ch, nil
}

func (p *Process) Shutdown(ctx context.Context) {
	if p.client != nil {
		shutCtx, cancel := context.WithTimeout(ctx, shutdownGrace)
		defer cancel()
		_, _ = p.client.Shutdown(shutCtx, &pb.ShutdownRequest{})
	}
	p.cleanup()
}

func (p *Process) cleanup() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(shutdownGrace):
			log.Printf("sandbox %s did not exit in time, killing", p.id)
			_ = p.cmd.Process.Kill()
			<-done
		}
	}
	_ = os.Remove(p.sockPath)
}
