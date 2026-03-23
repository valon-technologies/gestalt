package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pb "github.com/valon-technologies/gestalt/internal/sandbox/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	healthPollInterval = 100 * time.Millisecond
	healthPollTimeout  = 30 * time.Second
	shutdownTimeout    = 10 * time.Second
)

type sandboxProcess struct {
	id         string
	cmd        *exec.Cmd
	listenAddr string
	client     pb.SandboxServiceClient
	conn       *grpc.ClientConn
	busy       atomic.Bool
}

func spawnProcess(ctx context.Context, pythonCmd, script, toolServiceAddr string) (*sandboxProcess, error) {
	id := uuid.New().String()
	listenAddr := fmt.Sprintf("unix:///tmp/gestalt-sandbox-%s.sock", id)

	cmd := exec.CommandContext(ctx, pythonCmd, script,
		"--listen-addr", listenAddr,
		"--tool-service-addr", toolServiceAddr,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting sandbox process: %w", err)
	}

	conn, err := waitForHealth(ctx, listenAddr)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("sandbox %s health check failed: %w", id, err)
	}

	return &sandboxProcess{
		id:         id,
		cmd:        cmd,
		listenAddr: listenAddr,
		client:     pb.NewSandboxServiceClient(conn),
		conn:       conn,
	}, nil
}

func waitForHealth(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, healthPollTimeout)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dialing sandbox: %w", err)
	}

	client := pb.NewSandboxServiceClient(conn)
	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Close()
			return nil, fmt.Errorf("timed out waiting for sandbox to become healthy")
		case <-ticker.C:
			resp, err := client.Health(ctx, &pb.HealthRequest{})
			if err == nil && resp.GetReady() {
				return conn, nil
			}
		}
	}
}

func Converse(p *sandboxProcess, ctx context.Context, req *pb.ConversationRequest) (<-chan *pb.ConversationEvent, error) {
	return p.converse(ctx, req)
}

func (p *sandboxProcess) converse(ctx context.Context, req *pb.ConversationRequest) (<-chan *pb.ConversationEvent, error) {
	stream, err := p.client.Converse(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("starting converse stream: %w", err)
	}

	ch := make(chan *pb.ConversationEvent, 32)
	go func() {
		defer close(ch)
		for {
			event, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (p *sandboxProcess) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	_, _ = p.client.Shutdown(ctx, &pb.ShutdownRequest{TimeoutSeconds: int32(shutdownTimeout.Seconds())})
	_ = p.conn.Close()

	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		return fmt.Errorf("sandbox %s did not exit gracefully, killed", p.id)
	}
}
