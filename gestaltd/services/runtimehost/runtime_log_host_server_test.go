package runtimehost

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	err = client.AppendLogs(context.Background(), "session-1", []gestalt.RuntimeLogEntry{
		{
			Stream:     gestalt.RuntimeLogStreamRuntime,
			Message:    "runtime boot",
			ObservedAt: observedAt,
			SourceSeq:  7,
		},
		{
			Stream:     gestalt.RuntimeLogStreamStderr,
			Message:    "stderr line\n",
			ObservedAt: observedAt.Add(time.Second),
			SourceSeq:  8,
		},
	})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
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

func TestRuntimeLogHostServerAppendsLogsAfterSessionStoppedOverSDKTransport(t *testing.T) {
	ctx := context.Background()
	store := runtimelogs.NewMemoryStore()
	if err := store.RegisterSession(ctx, runtimelogs.SessionRegistration{
		RuntimeProviderName: "modal",
		SessionID:           "session-1",
	}); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if _, err := store.AppendSessionLogs(ctx, "modal", "session-1", []runtimelogs.AppendEntry{{
		Stream:  runtimelogs.StreamRuntime,
		Message: "before stop",
	}}); err != nil {
		t.Fatalf("AppendSessionLogs(before stop): %v", err)
	}
	if err := store.MarkSessionStopped(ctx, "modal", "session-1", time.Now().UTC()); err != nil {
		t.Fatalf("MarkSessionStopped: %v", err)
	}

	hostServices, err := StartHostServices([]HostService{{
		Name:   "runtime_logs",
		EnvVar: gestalt.EnvRuntimeLogHostSocket,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer("modal", store.AppendSessionLogs))
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

	err = client.AppendLogs(ctx, "session-1", []gestalt.RuntimeLogEntry{{
		Stream:    gestalt.RuntimeLogStreamStderr,
		Message:   "after stop\n",
		SourceSeq: 2,
	}})
	if err != nil {
		t.Fatalf("AppendLogs(after stop): %v", err)
	}

	logs, err := store.ListSessionLogs(ctx, "modal", "session-1", 0, 10)
	if err != nil {
		t.Fatalf("ListSessionLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("ListSessionLogs len = %d, want 2", len(logs))
	}
	if logs[0].Message != "before stop" || logs[1].Message != "after stop\n" {
		t.Fatalf("ListSessionLogs messages = [%q, %q]", logs[0].Message, logs[1].Message)
	}

	if err := store.RegisterSession(ctx, runtimelogs.SessionRegistration{
		RuntimeProviderName: "modal",
		SessionID:           "session-1",
	}); err != nil {
		t.Fatalf("RegisterSession(second): %v", err)
	}
	err = client.AppendLogs(ctx, "session-1", []gestalt.RuntimeLogEntry{{
		Stream:  gestalt.RuntimeLogStreamRuntime,
		Message: "fresh session",
	}})
	if err != nil {
		t.Fatalf("AppendLogs(fresh session): %v", err)
	}
	logs, err = store.ListSessionLogs(ctx, "modal", "session-1", 0, 10)
	if err != nil {
		t.Fatalf("ListSessionLogs(fresh session): %v", err)
	}
	if len(logs) != 1 || logs[0].Seq != 1 || logs[0].Message != "fresh session" {
		t.Fatalf("fresh session logs = %#v, want one fresh log with seq 1", logs)
	}
}

func TestRuntimeLogHostServerMapsUnknownSessionToNotFound(t *testing.T) {
	ctx := context.Background()
	store := runtimelogs.NewMemoryStore()
	if err := store.MarkSessionStopped(ctx, "modal", "never-registered", time.Now().UTC()); err != nil {
		t.Fatalf("MarkSessionStopped(unknown): %v", err)
	}

	hostServices, err := StartHostServices([]HostService{{
		Name:   "runtime_logs",
		EnvVar: gestalt.EnvRuntimeLogHostSocket,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer("modal", store.AppendSessionLogs))
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
	err = client.AppendLogs(ctx, "never-registered", []gestalt.RuntimeLogEntry{{
		Stream:  gestalt.RuntimeLogStreamRuntime,
		Message: "should fail",
	}})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("AppendLogs(unknown) code = %v, want %v: %v", status.Code(err), codes.NotFound, err)
	}
}

func TestRuntimeLogHostServerKeepsStoppedSessionThroughEvictionPressure(t *testing.T) {
	ctx := context.Background()
	store := runtimelogs.NewMemoryStore()
	if err := store.RegisterSession(ctx, runtimelogs.SessionRegistration{
		RuntimeProviderName: "modal",
		SessionID:           "stopping-session",
	}); err != nil {
		t.Fatalf("RegisterSession(stopping): %v", err)
	}
	if err := store.RegisterSession(ctx, runtimelogs.SessionRegistration{
		RuntimeProviderName: "modal",
		SessionID:           "quiet-live-session",
	}); err != nil {
		t.Fatalf("RegisterSession(quiet live): %v", err)
	}
	if err := store.MarkSessionStopped(ctx, "modal", "stopping-session", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("MarkSessionStopped: %v", err)
	}
	for i := range 300 {
		sessionID := "filler-" + strconv.Itoa(i)
		if err := store.RegisterSession(ctx, runtimelogs.SessionRegistration{
			RuntimeProviderName: "modal",
			SessionID:           sessionID,
		}); err != nil {
			t.Fatalf("RegisterSession(%s): %v", sessionID, err)
		}
	}

	hostServices, err := StartHostServices([]HostService{{
		Name:   "runtime_logs",
		EnvVar: gestalt.EnvRuntimeLogHostSocket,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer("modal", store.AppendSessionLogs))
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
	err = client.AppendLogs(ctx, "stopping-session", []gestalt.RuntimeLogEntry{{
		Stream:  gestalt.RuntimeLogStreamRuntime,
		Message: "late shutdown log",
	}})
	if err != nil {
		t.Fatalf("AppendLogs(stopping session): %v", err)
	}
	err = client.AppendLogs(ctx, "quiet-live-session", []gestalt.RuntimeLogEntry{{
		Stream:  gestalt.RuntimeLogStreamRuntime,
		Message: "quiet live should have been evicted first",
	}})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("AppendLogs(quiet live) code = %v, want %v: %v", status.Code(err), codes.NotFound, err)
	}
}
