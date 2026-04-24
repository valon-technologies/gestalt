package pluginruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestLocalProviderGetSessionDiagnosticsWrapsUnavailable(t *testing.T) {
	t.Parallel()

	provider := NewLocalProvider()
	_, err := provider.GetSessionDiagnostics(context.Background(), GetSessionDiagnosticsRequest{SessionID: "missing"})
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("GetSessionDiagnostics error = %v, want ErrSessionUnavailable", err)
	}
}

func TestSessionLogBufferChunksLargePendingFragments(t *testing.T) {
	t.Parallel()

	buffer := newSessionLogBuffer(8)
	buffer.capture(LogStreamStderr, strings.Repeat("x", maxSessionLogPendingBytes+17), time.Now().UTC())

	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if got := len(buffer.entries); got != 1 {
		t.Fatalf("entries len = %d, want 1", got)
	}
	if got := len(buffer.entries[0].Message); got != maxSessionLogPendingBytes {
		t.Fatalf("entries[0] len = %d, want %d", got, maxSessionLogPendingBytes)
	}
	if got := len(buffer.pending[LogStreamStderr]); got != 17 {
		t.Fatalf("pending len = %d, want 17", got)
	}
}

func TestEnrichErrorWithDiagnosticsPreservesWrappedStatus(t *testing.T) {
	t.Parallel()

	base := status.Error(codes.DeadlineExceeded, "timed out")
	got := enrichErrorWithDiagnostics(base, &SessionDiagnostics{
		Logs: []LogEntry{{
			Stream:  LogStreamStderr,
			Message: "Traceback: boom",
		}},
		Truncated: true,
	})
	if status.Code(got) != codes.DeadlineExceeded {
		t.Fatalf("status.Code(error) = %v, want DeadlineExceeded", status.Code(got))
	}
	if !strings.Contains(got.Error(), "recent runtime logs:") {
		t.Fatalf("error = %q, want runtime log context", got.Error())
	}
	if !strings.Contains(got.Error(), "[truncated]") {
		t.Fatalf("error = %q, want truncation marker", got.Error())
	}
}

func TestExecutableProviderGetSessionDiagnosticsUsesFallbackSessionError(t *testing.T) {
	t.Parallel()

	provider := &executableProvider{
		runtime: stubRuntimeClient{
			getSessionDiagnostics: func(context.Context, *proto.GetPluginRuntimeSessionDiagnosticsRequest, ...grpc.CallOption) (*proto.PluginRuntimeSessionDiagnostics, error) {
				return nil, status.Error(codes.Unimplemented, "diagnostics unavailable")
			},
			getSession: func(context.Context, *proto.GetPluginRuntimeSessionRequest, ...grpc.CallOption) (*proto.PluginRuntimeSession, error) {
				return nil, status.Error(codes.Internal, "session lookup failed")
			},
		},
	}

	_, err := provider.GetSessionDiagnostics(context.Background(), GetSessionDiagnosticsRequest{SessionID: "session-1"})
	if !errors.Is(err, ErrDiagnosticsUnavailable) {
		t.Fatalf("GetSessionDiagnostics error = %v, want ErrDiagnosticsUnavailable", err)
	}
	if !strings.Contains(err.Error(), "session lookup failed") {
		t.Fatalf("GetSessionDiagnostics error = %q, want fallback session failure", err)
	}
	if strings.Contains(err.Error(), "code = Unimplemented") {
		t.Fatalf("GetSessionDiagnostics error = %q, want original unimplemented rpc detail omitted", err)
	}
}

type stubRuntimeClient struct {
	getSession            func(context.Context, *proto.GetPluginRuntimeSessionRequest, ...grpc.CallOption) (*proto.PluginRuntimeSession, error)
	getSessionDiagnostics func(context.Context, *proto.GetPluginRuntimeSessionDiagnosticsRequest, ...grpc.CallOption) (*proto.PluginRuntimeSessionDiagnostics, error)
}

func (s stubRuntimeClient) GetSupport(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.PluginRuntimeSupport, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) StartSession(context.Context, *proto.StartPluginRuntimeSessionRequest, ...grpc.CallOption) (*proto.PluginRuntimeSession, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) GetSession(ctx context.Context, req *proto.GetPluginRuntimeSessionRequest, opts ...grpc.CallOption) (*proto.PluginRuntimeSession, error) {
	if s.getSession != nil {
		return s.getSession(ctx, req, opts...)
	}
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) GetSessionDiagnostics(ctx context.Context, req *proto.GetPluginRuntimeSessionDiagnosticsRequest, opts ...grpc.CallOption) (*proto.PluginRuntimeSessionDiagnostics, error) {
	if s.getSessionDiagnostics != nil {
		return s.getSessionDiagnostics(ctx, req, opts...)
	}
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) StopSession(context.Context, *proto.StopPluginRuntimeSessionRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) BindHostService(context.Context, *proto.BindPluginRuntimeHostServiceRequest, ...grpc.CallOption) (*proto.PluginRuntimeHostServiceBinding, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}

func (s stubRuntimeClient) StartPlugin(context.Context, *proto.StartHostedPluginRequest, ...grpc.CallOption) (*proto.HostedPlugin, error) {
	return nil, status.Error(codes.Unimplemented, "unused")
}
