package providerhost

import (
	"context"
	"errors"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeLogAppendFunc func(ctx context.Context, runtimeProviderName, sessionID string, entries []runtimelogs.AppendEntry) (int64, error)

type RuntimeLogHostServer struct {
	proto.UnimplementedPluginRuntimeLogHostServer
	runtimeProviderName string
	appendLogs          runtimeLogAppendFunc
}

func NewRuntimeLogHostServer(runtimeProviderName string, appendLogs runtimeLogAppendFunc) *RuntimeLogHostServer {
	return &RuntimeLogHostServer{
		runtimeProviderName: strings.TrimSpace(runtimeProviderName),
		appendLogs:          appendLogs,
	}
}

func (s *RuntimeLogHostServer) AppendLogs(ctx context.Context, req *proto.AppendPluginRuntimeLogsRequest) (*proto.AppendPluginRuntimeLogsResponse, error) {
	if s == nil || s.appendLogs == nil {
		return nil, status.Error(codes.FailedPrecondition, "runtime log host is not available")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	entries := make([]runtimelogs.AppendEntry, 0, len(req.GetLogs()))
	for _, entry := range req.GetLogs() {
		if entry == nil || entry.GetMessage() == "" {
			continue
		}
		observedAt := time.Time{}
		if ts := entry.GetObservedAt(); ts != nil {
			observedAt = ts.AsTime()
		}
		entries = append(entries, runtimelogs.AppendEntry{
			SourceSeq:  entry.GetSourceSeq(),
			Stream:     logStreamFromProto(entry.GetStream()),
			Message:    entry.GetMessage(),
			ObservedAt: observedAt,
		})
	}
	lastSeq, err := s.appendLogs(ctx, s.runtimeProviderName, sessionID, entries)
	if err != nil {
		switch {
		case errors.Is(err, indexeddb.ErrNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "append runtime session logs: %v", err)
		}
	}
	return &proto.AppendPluginRuntimeLogsResponse{LastSeq: lastSeq}, nil
}

func logStreamFromProto(stream proto.PluginRuntimeLogStream) runtimelogs.Stream {
	switch stream {
	case proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDOUT:
		return runtimelogs.StreamStdout
	case proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDERR:
		return runtimelogs.StreamStderr
	case proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_RUNTIME:
		return runtimelogs.StreamRuntime
	default:
		return runtimelogs.StreamRuntime
	}
}

var _ proto.PluginRuntimeLogHostServer = (*RuntimeLogHostServer)(nil)
