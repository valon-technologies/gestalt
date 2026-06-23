package runtimeprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
)

func TestPaginateSortedSessionIDsContinuesWithTokenPageSizeWhenPageSizeOmitted(t *testing.T) {
	t.Parallel()

	first, token, err := paginateSortedSessionIDs([]string{"a", "b", "c", "d", "e"}, &proto.ListRuntimeSessionsRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("paginateSortedSessionIDs(first): %v", err)
	}
	if !reflect.DeepEqual(first, []string{"a", "b"}) || token == "" {
		t.Fatalf("first page = %v token=%q, want first two ids and token", first, token)
	}

	second, nextToken, err := paginateSortedSessionIDs([]string{"a", "b", "c", "d", "e"}, &proto.ListRuntimeSessionsRequest{PageToken: token})
	if err != nil {
		t.Fatalf("paginateSortedSessionIDs(second): %v", err)
	}
	if !reflect.DeepEqual(second, []string{"c", "d"}) || nextToken == "" {
		t.Fatalf("second page = %v token=%q, want next two ids and token", second, nextToken)
	}
}

func TestLocalProviderCapturesRuntimeSessionLogsOnPluginStartupFailure(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	runtime := NewLocalProvider(WithLocalRuntimeSessionLogs("local", services.RuntimeSessionLogs))
	t.Cleanup(func() {
		_ = runtime.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{
		AppName: "log-test",
		Metadata: map[string]string{
			"provider_name": "log-test",
			"provider_kind": "app",
			"owner_kind":    "test",
			"owner_id":      "runtime-log-test",
		},
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = runtime.StartApp(ctx, &proto.StartHostedAppRequest{
		SessionId: session.GetId(),
		AppName:   "log-test",
		Command:   "/bin/sh",
		Args: []string{
			"-c",
			"printf 'hello stdout\\n'; printf 'hello stderr\\n' >&2; exit 17",
		},
	})
	if err == nil {
		t.Fatal("StartApp succeeded, want startup failure")
	}
	if !strings.Contains(err.Error(), "plugin process exited before serving gRPC") {
		t.Fatalf("StartApp error = %q, want startup failure", err)
	}

	gotSession, err := runtime.GetSession(ctx, &proto.GetRuntimeSessionRequest{SessionId: session.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSession.GetState() != SessionStateFailed {
		t.Fatalf("session state = %q, want %q", gotSession.GetState(), SessionStateFailed)
	}

	logs, err := services.RuntimeSessionLogs.ListSessionLogs(ctx, "local", session.GetId(), 0, 100)
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

	if got := byStream[runtimelogs.StreamRuntime]; !strings.Contains(got, `starting app "log-test"`) {
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

	tail, err := services.RuntimeSessionLogs.TailSessionLogs(ctx, "local", session.GetId(), 2)
	if err != nil {
		t.Fatalf("TailSessionLogs: %v", err)
	}
	if len(tail) != 2 {
		t.Fatalf("TailSessionLogs len = %d, want 2", len(tail))
	}
	if tail[0].Seq >= tail[1].Seq {
		t.Fatalf("TailSessionLogs seqs = [%d %d], want ascending order", tail[0].Seq, tail[1].Seq)
	}

	if err := runtime.StopSession(ctx, &proto.StopRuntimeSessionRequest{SessionId: session.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	retained, err := services.RuntimeSessionLogs.ListSessionLogs(ctx, "local", session.GetId(), 0, 100)
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
	session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	req := &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      session.GetId(),
		AgentSessionId: "agent-session-1",
		Workspace: &proto.AgentWorkspace{
			Cwd: "app",
			Checkouts: []*proto.AgentWorkspaceGitCheckout{{
				Url:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	}
	prepared, err := runtime.PrepareWorkspace(ctx, req)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if prepared.GetWorkspace().GetRoot() == "" || prepared.GetWorkspace().GetCwd() == "" {
		t.Fatalf("prepared workspace = %#v", prepared)
	}
	if !filepath.IsAbs(prepared.GetWorkspace().GetRoot()) || !filepath.IsAbs(prepared.GetWorkspace().GetCwd()) {
		t.Fatalf("prepared workspace paths must be absolute: %#v", prepared)
	}
	data, err := os.ReadFile(filepath.Join(prepared.GetWorkspace().GetCwd(), "README.md"))
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
	if again.GetWorkspace().GetCwd() != prepared.GetWorkspace().GetCwd() || again.GetWorkspace().GetRoot() != prepared.GetWorkspace().GetRoot() {
		t.Fatalf("prepared retry = %#v, want %#v", again, prepared)
	}

	different := &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      req.GetSessionId(),
		AgentSessionId: req.GetAgentSessionId(),
		Workspace: &proto.AgentWorkspace{
			Cwd: "other",
			Checkouts: []*proto.AgentWorkspaceGitCheckout{{
				Url:  "file://" + filepath.ToSlash(repo),
				Path: "other",
			}},
		},
	}
	if _, err := runtime.PrepareWorkspace(ctx, different); err == nil || !strings.Contains(err.Error(), "different spec") {
		t.Fatalf("PrepareWorkspace with different spec error = %v, want different spec", err)
	}

	if err := runtime.StopSession(ctx, &proto.StopRuntimeSessionRequest{SessionId: session.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if _, err := os.Stat(prepared.GetWorkspace().GetRoot()); !errors.Is(err, os.ErrNotExist) {
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
	session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = runtime.PrepareWorkspace(ctx, &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      session.GetId(),
		AgentSessionId: "agent-session-1",
		Workspace: &proto.AgentWorkspace{
			Cwd: "app",
			Checkouts: []*proto.AgentWorkspaceGitCheckout{{
				Url:  "github.com/valon-technologies/app",
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
	session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "workspace-test"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, id := range []string{".", "..", "nested/path"} {
		if err := runtime.RemoveWorkspace(ctx, &proto.RemoveRuntimeWorkspaceRequest{SessionId: session.GetId(), AgentSessionId: id}); err == nil {
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

func TestLocalProviderTeesSessionLogsToStderr(t *testing.T) {
	store := runtimelogs.NewMemoryStore()
	var sessionID string
	captured := captureStderr(t, func() {
		runtime := NewLocalProvider(WithLocalRuntimeSessionLogs("local", store))
		defer func() { _ = runtime.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "log-tee"})
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		sessionID = session.GetId()

		_, _ = runtime.StartApp(ctx, &proto.StartHostedAppRequest{
			SessionId: sessionID,
			AppName:   "log-tee",
			Command:   "/bin/sh",
			Args: []string{
				"-c",
				"printf 'hello stdout\n'; printf 'hello stderr\n' >&2; exit 17",
			},
		})
	})
	if sessionID == "" {
		t.Fatal("session ID was not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logs, err := store.ListSessionLogs(ctx, "local", sessionID, 0, 100)
	if err != nil {
		t.Fatalf("ListSessionLogs: %v", err)
	}
	byStream := map[runtimelogs.Stream]string{}
	for _, entry := range logs {
		byStream[entry.Stream] += entry.Message
	}
	if got := byStream[runtimelogs.StreamStdout]; !strings.Contains(got, "hello stdout") {
		t.Fatalf("stdout logs = %q, want hello stdout", got)
	}
	if got := byStream[runtimelogs.StreamStderr]; !strings.Contains(got, "hello stderr") {
		t.Fatalf("stderr logs = %q, want hello stderr", got)
	}

	if !strings.Contains(captured, "provider=log-tee hello stdout") {
		t.Fatalf("captured stderr = %q, want provider=log-tee hello stdout", captured)
	}
	if !strings.Contains(captured, "provider=log-tee hello stderr") {
		t.Fatalf("captured stderr = %q, want provider=log-tee hello stderr", captured)
	}
}

func TestLocalProviderNoSessionLogsDefaultsToStderr(t *testing.T) {
	var sessionID string
	captured := captureStderr(t, func() {
		runtime := NewLocalProvider()
		defer func() { _ = runtime.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "no-log-tee"})
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		sessionID = session.GetId()

		_, _ = runtime.StartApp(ctx, &proto.StartHostedAppRequest{
			SessionId: sessionID,
			AppName:   "no-log-tee",
			Command:   "/bin/sh",
			Args: []string{
				"-c",
				"printf 'hello stdout\n'; printf 'hello stderr\n' >&2; exit 17",
			},
		})
	})

	if !strings.Contains(captured, "hello stdout") {
		t.Fatalf("captured stderr = %q, want hello stdout", captured)
	}
	if !strings.Contains(captured, "hello stderr") {
		t.Fatalf("captured stderr = %q, want hello stderr", captured)
	}
	if strings.Contains(captured, "provider=") {
		t.Fatalf("captured stderr = %q, want no provider prefix", captured)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	_ = r.Close()
	return buf.String()
}

func TestProviderLogWriterPrefixesLines(t *testing.T) {
	var buf bytes.Buffer
	w := newProviderLogWriter(&buf, "codeReview")
	if _, err := w.Write([]byte("first line\nsecond line\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "provider=codeReview first line\nprovider=codeReview second line\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestProviderLogWriterBuffersPartialLines(t *testing.T) {
	var buf bytes.Buffer
	w := newProviderLogWriter(&buf, "codeReview")
	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte(" second\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := "provider=codeReview first second\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestProviderLogWriterFlushWithoutNewline(t *testing.T) {
	var buf bytes.Buffer
	w := newProviderLogWriter(&buf, "codeReview")
	if _, err := w.Write([]byte("partial")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	want := "provider=codeReview partial"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestProviderLogWriterEmptyNameFallsBack(t *testing.T) {
	var buf bytes.Buffer
	w := newProviderLogWriter(&buf, "")
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := buf.String(); got != "provider=unknown line\n" {
		t.Fatalf("output = %q", got)
	}
}
