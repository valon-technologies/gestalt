package pluginruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
)

func TestLocalProviderCapturesRuntimeSessionLogsOnPluginStartupFailure(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	runtime := NewLocalProvider(WithLocalRuntimeSessionLogs("local", services.RuntimeSessionLogs))
	t.Cleanup(func() {
		_ = runtime.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := runtime.StartSession(ctx, StartSessionRequest{
		PluginName: "log-test",
		Metadata: map[string]string{
			"provider_name": "log-test",
			"provider_kind": "plugin",
			"owner_kind":    "test",
			"owner_id":      "runtime-log-test",
		},
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = runtime.StartPlugin(ctx, StartPluginRequest{
		SessionID:  session.ID,
		PluginName: "log-test",
		Command:    "/bin/sh",
		Args: []string{
			"-c",
			"printf 'hello stdout\\n'; printf 'hello stderr\\n' >&2; exit 17",
		},
	})
	if err == nil {
		t.Fatal("StartPlugin succeeded, want startup failure")
	}
	if !strings.Contains(err.Error(), "plugin process exited before serving gRPC") {
		t.Fatalf("StartPlugin error = %q, want startup failure", err)
	}

	gotSession, err := runtime.GetSession(ctx, GetSessionRequest{SessionID: session.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSession.State != SessionStateFailed {
		t.Fatalf("session state = %q, want %q", gotSession.State, SessionStateFailed)
	}

	logs, err := services.RuntimeSessionLogs.ListSessionLogs(ctx, "local", session.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListSessionLogs: %v", err)
	}
	if len(logs) < 4 {
		t.Fatalf("runtime session logs len = %d, want at least 4", len(logs))
	}

	byStream := map[runtimelogs.Stream]string{}
	for _, entry := range logs {
		byStream[entry.Stream] += entry.Message
	}

	if got := byStream[runtimelogs.StreamRuntime]; !strings.Contains(got, `starting plugin "log-test"`) {
		t.Fatalf("runtime logs = %q, want startup entry", got)
	}
	if got := byStream[runtimelogs.StreamRuntime]; !strings.Contains(got, "plugin process exited before serving gRPC") {
		t.Fatalf("runtime logs = %q, want startup failure entry", got)
	}
	if got := byStream[runtimelogs.StreamStdout]; !strings.Contains(got, "hello stdout") {
		t.Fatalf("stdout logs = %q, want stdout payload", got)
	}
	if got := byStream[runtimelogs.StreamStderr]; !strings.Contains(got, "hello stderr") {
		t.Fatalf("stderr logs = %q, want stderr payload", got)
	}

	tail, err := services.RuntimeSessionLogs.TailSessionLogs(ctx, "local", session.ID, 2)
	if err != nil {
		t.Fatalf("TailSessionLogs: %v", err)
	}
	if len(tail) != 2 {
		t.Fatalf("TailSessionLogs len = %d, want 2", len(tail))
	}
	if tail[0].Seq >= tail[1].Seq {
		t.Fatalf("TailSessionLogs seqs = [%d %d], want ascending order", tail[0].Seq, tail[1].Seq)
	}

	if err := runtime.StopSession(ctx, StopSessionRequest{SessionID: session.ID}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	retained, err := services.RuntimeSessionLogs.ListSessionLogs(ctx, "local", session.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListSessionLogs after stop: %v", err)
	}
	if len(retained) != len(logs) {
		t.Fatalf("ListSessionLogs after stop len = %d, want %d", len(retained), len(logs))
	}
}
