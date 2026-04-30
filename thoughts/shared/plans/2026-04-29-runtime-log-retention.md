# Runtime Log Retention Simplification Plan

## Problem Statement

Hosted runtime logs are currently fragile around runtime shutdown and replacement. When a hosted runtime session is stopped, `gestaltd` immediately deletes the in-memory runtime log session. The Modal runtime provider can still have asynchronous stdout, stderr, and process-watch goroutines attempting to append final logs for that same session. Those late appends then fail with:

```text
append runtime session logs: runtime session not found
```

This makes failures harder to debug at exactly the moment logs are most valuable. It also adds an edge case to the mental model: a session can be known to the runtime provider, fail, get closed, and then lose the final logs because log retention is coupled to runtime lifecycle cleanup.

The fix should reduce the architecture's complexity. Runtime log storage should be an appendable diagnostic trail for a bounded time, not a strict mirror of whether a runtime session is currently alive.

## Requirements

- Preserve runtime logs after `StopSession` so startup, shutdown, and failure diagnostics remain queryable.
- Accept late log appends for a previously registered session after it has been marked stopped.
- Keep the public API shape unchanged unless a small internal semantic clarification is enough.
- Reduce branching and edge cases: "registered log session" should be the only condition needed to append logs, regardless of whether the runtime session is active or stopped.
- Keep retention bounded so an in-memory implementation cannot grow without limit.
- Preserve existing per-session record caps.
- Map unknown runtime log sessions to gRPC `NotFound`, not `Internal`.
- Verify through lifecycle/transport contract tests, not narrow unit-only tests.

## Constraints

- `RuntimeSessionLogs` is currently a process-local in-memory store initialized in `gestaltd/internal/coredata/services.go`.
- Existing admin runtime log APIs already read from `RuntimeSessionLogs`; they should become more useful after this change without a public route change.
- The in-memory store is intentionally bounded:
  - max sessions: `memoryStoreMaxSessions = 256`
  - max records per session: `memoryStoreMaxRecordsPerSession = 4096`
- Runtime providers can append logs asynchronously after a process has begun shutting down:
  - Modal log sink uses async stream goroutines in `/Users/hugh/src/gestalt-providers/runtime/modal/runtime_logs.go`.
- Hosted agent pool replacement can close a backend immediately after provider call or health-check failures:
  - `gestaltd/internal/bootstrap/hosted_agent_pool.go:1319`
  - `gestaltd/internal/bootstrap/hosted_agent_pool.go:1510`
- We should not introduce a durable log database in this iteration. That would add a new operational dependency and more architecture, while the immediate correctness bug is lifecycle semantics inside the existing store.

## Current State Analysis

### Registration

`executableProvider.StartSession` registers the runtime log session after the runtime provider returns a session:

```go
p.sessionLogs.RegisterSession(ctx, runtimelogs.SessionRegistration{
    RuntimeProviderName: p.name,
    SessionID:           session.ID,
    Metadata:            metadata,
})
```

Reference: `gestaltd/internal/pluginruntime/executable.go:113`

### Stop Deletes The Diagnostic Trail

`executableProvider.StopSession` calls `MarkSessionStopped` after the runtime provider stops:

Reference: `gestaltd/internal/pluginruntime/executable.go:213`

Today `MemoryStore.MarkSessionStopped` deletes the session and all logs:

```go
delete(s.sessions, key)
delete(s.logs, key)
```

Reference: `gestaltd/internal/runtimelogs/memory.go:186`

### Late Appends Fail

`MemoryStore.AppendSessionLogs` requires the session to still be registered:

```go
if _, ok := s.sessions[key]; !ok {
    return 0, fmt.Errorf("append runtime session logs: %w", ErrSessionNotFound)
}
```

Reference: `gestaltd/internal/runtimelogs/memory.go:79`

The Modal runtime provider reports those append failures to stderr:

```go
fmt.Fprintf(os.Stderr, "modal runtime: append session log for %q: %v\n", sessionID, err)
```

Reference: `/Users/hugh/src/gestalt-providers/runtime/modal/runtime_logs.go:111`

### Error Mapping Is Too Broad

`RuntimeLogHostServer.AppendLogs` maps `indexeddb.ErrNotFound` to gRPC `NotFound`, but not `runtimelogs.ErrSessionNotFound`, so local runtime-log misses show as `Internal`:

Reference: `gestaltd/internal/providerhost/runtime_log_host_server.go:57`

## Desired End State

Runtime log sessions have a simpler lifecycle:

```text
RegisterSession
  -> appends accepted
MarkSessionStopped
  -> session is marked stopped, logs retained
  -> late appends still accepted
capacity eviction
  -> oldest retained sessions/logs removed
```

The runtime session lifecycle and runtime log retention lifecycle are no longer the same thing.

### Example Behavior Before

```go
session, _ := runtime.StartSession(ctx, req)
_ = runtime.StopSession(ctx, pluginruntime.StopSessionRequest{SessionID: session.ID})

_, err := logs.AppendSessionLogs(ctx, "modal", session.ID, entries)
// err: append runtime session logs: runtime session not found

_, err = logs.ListSessionLogs(ctx, "modal", session.ID, 0, 100)
// err: list runtime session logs: runtime session not found
```

### Example Behavior After

```go
session, _ := runtime.StartSession(ctx, req)
_ = runtime.StopSession(ctx, pluginruntime.StopSessionRequest{SessionID: session.ID})

_, err := logs.AppendSessionLogs(ctx, "modal", session.ID, entries)
// err == nil

records, err := logs.ListSessionLogs(ctx, "modal", session.ID, 0, 100)
// err == nil, records include pre-stop and late shutdown logs
```

Unknown session IDs should still fail:

```go
_, err := logs.AppendSessionLogs(ctx, "modal", "never-registered", entries)
// errors.Is(err, runtimelogs.ErrSessionNotFound) == true
```

Over gRPC transport, truly unknown sessions should surface as `codes.NotFound`, not `codes.Internal`.

## Proposed Interface Changes

### Public API

No public HTTP, SDK, YAML, or proto shape change.

### Internal Runtime Log Store Semantics

Clarify the existing `runtimelogs.Store` contract:

```go
type Store interface {
    RegisterSession(ctx context.Context, registration SessionRegistration) error
    AppendSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, entries []AppendEntry) (int64, error)
    ListSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, afterSeq int64, limit int) ([]Record, error)
    TailSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, limit int) ([]Record, error)
    MarkSessionStopped(ctx context.Context, runtimeProviderName, sessionID string, stoppedAt time.Time) error
}
```

`RegisterSession` changes or clarifies duplicate semantics:

- Registering an already-known `runtimeProviderName + sessionID` starts a fresh diagnostic trail for that key.
- Existing logs for that key are cleared on re-registration so a reused session ID cannot inherit old records or continue old sequence numbers.

`MarkSessionStopped` changes semantics:

- Before: delete the session and logs immediately.
- After: touch the registered session's retention timestamp and keep logs. Unknown sessions remain a no-op and must not become appendable.

`AppendSessionLogs` remains simple:

- append if the log session is registered, active or stopped.
- return `ErrSessionNotFound` only if the log session was never registered or has already been evicted.

## Implementation Approach

Keep this as a single `gestaltd` PR. The fix belongs in core runtime-log lifecycle semantics, not in the Modal runtime provider or hosted-agent pool. That makes every runtime provider simpler: log append behavior no longer depends on racing backend shutdown.

### Phase 1: Retain Stopped Runtime Logs

#### Files

- `gestaltd/internal/runtimelogs/memory.go`
- `gestaltd/internal/runtimelogs/types.go` if comments should clarify the contract

#### Changes

- Change `RegisterSession` to initialize a fresh empty log slice for the session key.
- Change `MarkSessionStopped` to refresh a registered session's retention timestamp instead of deleting sessions/logs.
- Treat `MarkSessionStopped` for unknown sessions as a no-op. It must not create an appendable session.
- Keep logs in `s.logs[key]`.
- Continue using `evictOldSessionsLocked` as the bounded retention mechanism.
- Keep the max sessions and max records constants unchanged.
- Update comments to state that `MarkSessionStopped` does not remove retained logs.

Capacity tradeoff: retained stopped sessions still count toward `memoryStoreMaxSessions`. That is acceptable for this fix because runtime logs are bounded diagnostics, and removing stopped sessions first would add lifecycle branching. If a later incident shows active sessions are being evicted too aggressively, add TTL or stopped-session-priority eviction as a separate change with metrics.

### Phase 2: Correct Runtime Log Host Error Mapping

#### Files

- `gestaltd/internal/providerhost/runtime_log_host_server.go`

#### Changes

- Map `runtimelogs.ErrSessionNotFound` to gRPC `NotFound`.
- Keep the existing `indexeddb.ErrNotFound` mapping.

Example:

```go
case errors.Is(err, indexeddb.ErrNotFound), errors.Is(err, runtimelogs.ErrSessionNotFound):
    return nil, status.Error(codes.NotFound, err.Error())
```

### Phase 3: Contract Tests

#### Files

- `gestaltd/internal/pluginruntime/local_test.go`
- `gestaltd/internal/providerhost/runtime_log_host_server_test.go`

#### Test Updates

- Update the existing local runtime lifecycle test so `ListSessionLogs` after `StopSession` succeeds and returns the retained logs.
- Add or extend a runtime-log-host transport contract test using a real `runtimelogs.MemoryStore`:
  - register a runtime log session,
  - mark it stopped,
  - append logs through `gestalt.RuntimeLogHost()` over the host-service transport,
  - assert the late appended logs are retained.
- Add or extend transport error coverage so `ErrSessionNotFound` maps to gRPC `NotFound`.
- Add lifecycle coverage that re-registering the same session key starts a fresh log stream and does not mix old records.

These tests exercise lifecycle and transport contracts. Do not add isolated tests that only assert private fields.

## What We're Not Doing

- Not adding a durable runtime-log store in this iteration.
- Not changing Modal runtime logging behavior.
- Not adding Cloud Run affinity or public relay routing changes.
- Not changing hosted-agent replacement policy.
- Not increasing provider RPC deadlines as part of this fix.
- Not exposing stopped-state metadata through public APIs yet.

## Success Criteria

### Automated Verification

- `go test ./gestaltd/internal/runtimelogs ./gestaltd/internal/providerhost ./gestaltd/internal/pluginruntime`
- Existing runtime-log startup failure test still captures recent logs.
- A stopped runtime log session can be queried after `StopSession`.
- A late log append after `MarkSessionStopped` succeeds over the runtime-log host transport.
- Re-registering the same runtime log key starts a fresh diagnostic trail.
- `MarkSessionStopped` for an unknown session does not make that session appendable.
- Unknown runtime log sessions return gRPC `NotFound`.

### Manual Verification

- Reproduce a hosted runtime failure or shutdown in a dev/test deployment.
- Confirm `modal runtime: append session log ... runtime session not found` no longer appears for ordinary stopped-session late appends.
- Confirm the admin runtime-log endpoint can still read logs after the runtime session has stopped, until bounded in-memory eviction.

## Risks And Mitigations

- Risk: retaining logs after stop increases memory lifetime.
  - Mitigation: the store is already bounded by session count and record count. This plan does not remove those caps.
- Risk: accepting late appends after stop could hide a bad runtime lifecycle.
  - Mitigation: unknown sessions still return `ErrSessionNotFound`; only previously registered sessions get retained.
- Risk: tests could become too implementation-specific.
  - Mitigation: test observable behavior through provider lifecycle and runtime-log host transport.

## Code References

- Runtime log session registration: `gestaltd/internal/pluginruntime/executable.go:113`
- Runtime log session stop path: `gestaltd/internal/pluginruntime/executable.go:213`
- Immediate log deletion today: `gestaltd/internal/runtimelogs/memory.go:186`
- Append requires registered session today: `gestaltd/internal/runtimelogs/memory.go:79`
- Runtime log host error mapping: `gestaltd/internal/providerhost/runtime_log_host_server.go:57`
- Hosted runtime replacement after provider call errors: `gestaltd/internal/bootstrap/hosted_agent_pool.go:1319`
- Hosted runtime close path: `gestaltd/internal/bootstrap/hosted_agent_pool.go:1510`
- Modal async log append path: `/Users/hugh/src/gestalt-providers/runtime/modal/runtime_logs.go:54`
