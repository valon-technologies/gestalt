package pluginruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestLocalProviderPreparesGitWorkspaceAndCleansUpWithSession(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createWorkspaceGitRepo(t)

	runtime := NewLocalProvider()
	t.Cleanup(func() {
		_ = runtime.Close()
	})
	session, err := runtime.StartSession(ctx, StartSessionRequest{PluginName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	req := PrepareWorkspaceRequest{
		SessionID:      session.ID,
		AgentSessionID: "agent-session-1",
		Workspace: &Workspace{
			CWD: "app",
			Checkouts: []WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	}
	prepared, err := runtime.PrepareWorkspace(ctx, req)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if prepared.Root == "" || prepared.CWD == "" {
		t.Fatalf("prepared workspace = %#v", prepared)
	}
	if !filepath.IsAbs(prepared.Root) || !filepath.IsAbs(prepared.CWD) {
		t.Fatalf("prepared workspace paths must be absolute: %#v", prepared)
	}
	data, err := os.ReadFile(filepath.Join(prepared.CWD, "README.md"))
	if err != nil {
		t.Fatalf("read checkout README: %v", err)
	}
	if strings.TrimSpace(string(data)) != "workspace fixture" {
		t.Fatalf("README = %q", data)
	}

	again, err := runtime.PrepareWorkspace(ctx, req)
	if err != nil {
		t.Fatalf("PrepareWorkspace retry: %v", err)
	}
	if again.CWD != prepared.CWD || again.Root != prepared.Root {
		t.Fatalf("prepared retry = %#v, want %#v", again, prepared)
	}

	different := req
	different.Workspace = &Workspace{
		CWD: "other",
		Checkouts: []WorkspaceGitCheckout{{
			URL:  "file://" + filepath.ToSlash(repo),
			Path: "other",
		}},
	}
	if _, err := runtime.PrepareWorkspace(ctx, different); err == nil || !strings.Contains(err.Error(), "different spec") {
		t.Fatalf("PrepareWorkspace with different spec error = %v, want different spec", err)
	}

	if err := runtime.StopSession(ctx, StopSessionRequest{SessionID: session.ID}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if _, err := os.Stat(prepared.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace root stat error = %v, want not exist", err)
	}
}

func TestLocalProviderRejectsSchemeLessGitWorkspaceURL(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime := NewLocalProvider()
	t.Cleanup(func() {
		_ = runtime.Close()
	})
	session, err := runtime.StartSession(ctx, StartSessionRequest{PluginName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = runtime.PrepareWorkspace(ctx, PrepareWorkspaceRequest{
		SessionID:      session.ID,
		AgentSessionID: "agent-session-1",
		Workspace: &Workspace{
			CWD: "app",
			Checkouts: []WorkspaceGitCheckout{{
				URL:  "github.com/valon-technologies/app",
				Path: "app",
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "scheme is required") {
		t.Fatalf("PrepareWorkspace error = %v, want scheme required", err)
	}
}

func TestLocalProviderRejectsTraversalWorkspaceID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime := NewLocalProvider()
	t.Cleanup(func() {
		_ = runtime.Close()
	})
	session, err := runtime.StartSession(ctx, StartSessionRequest{PluginName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, id := range []string{".", "..", "nested/path"} {
		if err := runtime.RemoveWorkspace(ctx, RemoveWorkspaceRequest{SessionID: session.ID, AgentSessionID: id}); err == nil {
			t.Fatalf("RemoveWorkspace(%q) succeeded, want invalid id", id)
		}
	}
}

func createWorkspaceGitRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo): %v", err)
	}
	runWorkspaceGit(t, repo, "init")
	runWorkspaceGit(t, repo, "config", "user.email", "workspace@example.invalid")
	runWorkspaceGit(t, repo, "config", "user.name", "Workspace Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("workspace fixture\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README): %v", err)
	}
	runWorkspaceGit(t, repo, "add", "README.md")
	runWorkspaceGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runWorkspaceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}
