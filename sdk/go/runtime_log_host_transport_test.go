package gestalt_test

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type runtimeLogHostTestServer struct {
	proto.UnimplementedRuntimeLogHostServer

	mu       sync.Mutex
	requests []*proto.AppendRuntimeLogsRequest
}

func (s *runtimeLogHostTestServer) AppendLogs(_ context.Context, req *proto.AppendRuntimeLogsRequest) (*proto.AppendRuntimeLogsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	var lastSeq int64
	if logs := req.GetLogs(); len(logs) > 0 {
		lastSeq = logs[len(logs)-1].GetSourceSeq()
	}
	return &proto.AppendRuntimeLogsResponse{LastSeq: lastSeq}, nil
}

func (s *runtimeLogHostTestServer) requestsCopy() []*proto.AppendRuntimeLogsRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*proto.AppendRuntimeLogsRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func TestRuntimeLogHostAppendUsesRuntimeSessionEnv(t *testing.T) {
	tmp, err := os.CreateTemp("", "runtime-log-*.sock")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	socket := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(socket)
	t.Cleanup(func() { _ = os.Remove(socket) })
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	logs := &runtimeLogHostTestServer{}
	proto.RegisterRuntimeLogHostServer(srv, logs)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, socket)
	t.Setenv(gestalt.EnvRuntimeSessionID, "runtime-session-1")

	client, err := gestalt.RuntimeLogHost()
	if err != nil {
		t.Fatalf("RuntimeLogHost: %v", err)
	}
	observedAt := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	err = client.Append(
		context.Background(),
		"runtime boot\n",
		gestalt.WithRuntimeLogStream(gestalt.RuntimeLogStreamStderr),
		gestalt.WithRuntimeLogObservedAt(observedAt),
		gestalt.WithRuntimeLogSourceSeq(7),
	)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	requests := logs.requestsCopy()
	if len(requests) != 1 {
		t.Fatalf("AppendLogs requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.GetSessionId() != "runtime-session-1" {
		t.Fatalf("AppendLogs session_id = %q, want runtime-session-1", req.GetSessionId())
	}
	entry := req.GetLogs()[0]
	if entry.GetMessage() != "runtime boot\n" {
		t.Fatalf("AppendLogs message = %q, want runtime boot", entry.GetMessage())
	}
	if entry.GetStream() != proto.RuntimeLogStream_RUNTIME_LOG_STREAM_STDERR {
		t.Fatalf("AppendLogs stream = %v, want stderr", entry.GetStream())
	}
	if !entry.GetObservedAt().AsTime().Equal(observedAt) {
		t.Fatalf("AppendLogs observed_at = %s, want %s", entry.GetObservedAt().AsTime(), observedAt)
	}
	if entry.GetSourceSeq() != 7 {
		t.Fatalf("AppendLogs source_seq = %d, want 7", entry.GetSourceSeq())
	}
}
