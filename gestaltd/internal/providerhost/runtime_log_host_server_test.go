package providerhost

import (
	"context"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRuntimeLogHostServerAppendsLogsOverSDKTransport(t *testing.T) {
	type appendCall struct {
		runtimeProviderName string
		sessionID           string
		entries             []runtimelogs.AppendEntry
	}

	var (
		mu    sync.Mutex
		calls []appendCall
	)

	hostServices, err := StartHostServices([]HostService{{
		Name:   "runtime_logs",
		EnvVar: gestalt.EnvRuntimeLogHostSocket,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer("modal", func(_ context.Context, runtimeProviderName, sessionID string, entries []runtimelogs.AppendEntry) (int64, error) {
				mu.Lock()
				defer mu.Unlock()
				copied := make([]runtimelogs.AppendEntry, len(entries))
				copy(copied, entries)
				calls = append(calls, appendCall{
					runtimeProviderName: runtimeProviderName,
					sessionID:           sessionID,
					entries:             copied,
				})
				return int64(len(copied)), nil
			}))
		},
	}})
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() { _ = hostServices.Close() })

	bindings := hostServices.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("host service bindings len = %d, want 1", len(bindings))
	}
	t.Setenv(gestalt.EnvRuntimeLogHostSocket, bindings[0].SocketPath)

	client, err := gestalt.RuntimeLogHost()
	if err != nil {
		t.Fatalf("RuntimeLogHost: %v", err)
	}

	observedAt := time.Date(2026, time.April, 23, 12, 34, 56, 0, time.UTC)
	resp, err := client.AppendLogs(context.Background(), &proto.AppendPluginRuntimeLogsRequest{
		SessionId: "session-1",
		Logs: []*proto.PluginRuntimeLogEntry{
			{
				Stream:     proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
				Message:    "runtime boot",
				ObservedAt: timestamppb.New(observedAt),
				SourceSeq:  7,
			},
			{
				Stream:     proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDERR,
				Message:    "stderr line\n",
				ObservedAt: timestamppb.New(observedAt.Add(time.Second)),
				SourceSeq:  8,
			},
		},
	})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}
	if resp.GetLastSeq() != 2 {
		t.Fatalf("AppendLogs last_seq = %d, want 2", resp.GetLastSeq())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("append calls len = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.runtimeProviderName != "modal" {
		t.Fatalf("append call runtimeProviderName = %q, want modal", call.runtimeProviderName)
	}
	if call.sessionID != "session-1" {
		t.Fatalf("append call sessionID = %q, want session-1", call.sessionID)
	}
	if len(call.entries) != 2 {
		t.Fatalf("append call entries len = %d, want 2", len(call.entries))
	}
	if call.entries[0].Stream != runtimelogs.StreamRuntime || call.entries[0].Message != "runtime boot" || !call.entries[0].ObservedAt.Equal(observedAt) || call.entries[0].SourceSeq != 7 {
		t.Fatalf("append call entries[0] = %#v", call.entries[0])
	}
	if call.entries[1].Stream != runtimelogs.StreamStderr || call.entries[1].Message != "stderr line\n" || !call.entries[1].ObservedAt.Equal(observedAt.Add(time.Second)) || call.entries[1].SourceSeq != 8 {
		t.Fatalf("append call entries[1] = %#v", call.entries[1])
	}
}
