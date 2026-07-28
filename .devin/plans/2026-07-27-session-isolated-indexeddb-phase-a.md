# Session-Isolated IndexedDB for Reverse-Injected Local Apps — Phase A Plan

## Overview

Implement the additive, fail-closed infrastructure for session-isolated IndexedDB used by locally-developed apps that are reverse-published into a production Gestalt deployment. Phase A focuses on the production-side building blocks: protocol advertisement, namespace persistence, physical name mapping, host-service relay token claims, and request context propagation. Existing production behavior must remain unchanged unless a verified namespace capability is present.

## Current State Analysis

- `gestaltd/services/indexeddb/server.go` passes logical object-store names through unchanged (`storeName` is the identity function).
- `EffectiveAppIndexedDB` in `gestaltd/internal/config/provider_selection.go` computes `ProviderName`, `DB`, and `ObjectStores` but this data is never passed to the IndexedDB server.
- Cache and S3 are scoped per app in `gestaltd/internal/bootstrap/host_service_bootstrap.go`; IndexedDB is not.
- Remote IndexedDB currently flows over the public gRPC bearer route, not the host-service relay capability.
- `HostServiceRelayTokenManager` in `gestaltd/services/runtimehost/host_service_relay_token.go` already mints app/session/service/method-scoped tokens but has no namespace claims.
- `RemoteRegistrationService` in `gestaltd/internal/coredata/remote_registrations.go` has generation and lease lifecycle but no session state or namespace records.
- `remote.proto` only exposes `CreateRemote`, `ListRemotes`, and `DeleteRemote`; there is no `PrepareRemoteSession`/`ActivateRemoteSession`/`HeartbeatRemoteSession` or capability advertisement.
- The reverse tunnel exists for registration validation but not as an app invocation data plane.

## Desired End State (Phase A)

After Phase A:

1. `remote.proto` contains new additive messages and RPCs for feature advertisement, prepare, activate, heartbeat, and IndexedDB capabilities.
2. Generated Go server proto bindings include the new types.
3. `coredata` has `RemoteIndexedDBNamespace` and `RemoteIndexedDBNamespaceStore` persistence with schema bootstrap.
4. `gestaltd/services/indexeddb` has namespace-aware store-name resolver interfaces and a deterministic physical-name hashing function.
5. `HostServiceRelayTokenManager` supports an `IndexedDBNamespace` private claim with strict validation.
6. `hostserviceingress/context.go` restores verified namespace claims into request context.
7. `ListRemotesResponse` advertises `RemoteServerCapabilities`.
8. `RemoteManagement` service implements the new RPCs with appropriate auth checks (may be partial/stub for methods that need the invocation data plane).
9. A failing end-to-end test demonstrates that an unscoped local app currently touches production `tasks` and, after completion, that a scoped session does not.
10. If no verified namespace claim is present, all existing paths behave exactly as before.

## What We're NOT Doing (Phase A)

- Full `PrepareRemoteSession` / `ActivateRemoteSession` / `HeartbeatRemoteSession` state machine wired to local bootstrap.
- Local scoped IndexedDB client construction and token refresh.
- Publisher activation, heartbeat, and shutdown integration.
- Lease reaper and asynchronous namespace cleanup janitor.
- Broad dev proxy token (`GESTALT_DEV_API_PROXY_TOKEN`) hardening.
- TypeScript, Python, Rust, and browser SDK regeneration.
- Production deployment or enabling the local fail-closed check by default.

## Implementation Approach

- Additive only: every change is a no-op unless a verified `IndexedDBNamespace` claim is in context.
- Centralize mapping: all store-name resolution goes through one context-aware resolver.
- Fail closed: a missing, malformed, or unverifiable namespace claim causes `PermissionDenied` or `Unauthenticated`, not a fallback to raw storage.
- Keep logical names for metrics and app-visible responses; physical names are internal.
- Reuse existing host-service relay machinery rather than inventing a new transport.

## Phase A.1: Protocol and Generated Code

### Changes

**File:** `sdk/proto/v1/remote.proto`

- Add `RemoteServerCapabilities` message with `prepared_remote_sessions`, `session_scoped_indexeddb`, `remote_session_heartbeat`.
- Add `PrepareRemoteSessionRequest`, `PreparedRemoteSession`, `RemoteProviderCapability`, `RemoteIndexedDBCapability`.
- Add `ActivateRemoteSessionRequest` and `HeartbeatRemoteSessionRequest` / `HeartbeatRemoteSessionResponse`.
- Add `PrepareRemoteSession`, `ActivateRemoteSession`, `HeartbeatRemoteSession` RPCs to `RemoteManagement`.
- Add `capabilities` field to `ListRemotesResponse`.

**Regeneration**

```bash
cd sdk/proto
buf generate --template buf.go.server.gen.yaml
cd ../..
```

Verify with:

```bash
../.github/scripts/check-go-proto-split.sh .
```

Only Go server bindings are regenerated in Phase A; other language SDKs are deferred.

### Success Criteria

- [ ] `gestaltd/rpc/protov1/v1/remote*.go` compiles and contains new types.
- [ ] Existing `CreateRemote` / `ListRemotes` / `DeleteRemote` behavior unchanged.

## Phase A.2: Coredata Namespace Persistence

### Changes

**File:** `gestaltd/internal/coredata/schemas.go`

- Add constants `StoreRemoteIndexedDBNamespaces` and `StoreRemoteIndexedDBNamespaceStores`.
- Add `RemoteIndexedDBNamespacesSchema` and `RemoteIndexedDBNamespaceStoresSchema` with indexes described in the specification.

**New file:** `gestaltd/internal/coredata/remote_indexeddb_namespaces.go`

- Types `RemoteIndexedDBNamespace`, `RemoteIndexedDBNamespaceState`, `RemoteIndexedDBNamespaceStore`.
- `RemoteIndexedDBNamespaceService` with methods:
  - `Prepare`
  - `ResolveActive`
  - `TrackStore`
  - `MarkStoreDeleted`
  - `ActivateRegistration`
  - `RenewRegistration`
  - `MarkRegistrationCleanupPending`
  - `ClaimCleanup`
  - `CompleteCleanup`
  - `RecordCleanupFailure`
- Deterministic physical store name helper using truncated base32 SHA-256 (44-character prefix `gd_`).

**File:** `gestaltd/internal/coredata/services.go`

- Bootstrap the two new object stores in `NewWithOptions`.
- Add `RemoteIndexedDBNamespaces` to `Services`.

**File:** `gestaltd/internal/coredata/remote_registrations.go`

- Extend `RemoteRegistration` with `SessionID` and `State` (`preparing` / `active` / `deleting`).
- Update `Replace` / `RenewLease` / `Delete` / `Expire` / `ResolveProvider` to respect state and session where required.

### Success Criteria

- [ ] `go test ./internal/coredata/... -run 'Remote|Namespace' -count=1` passes.
- [ ] Namespace records round-trip through the service.
- [ ] Physical name hashing is stable and bounded.
- [ ] Generation and lease checks reject stale sessions.

## Phase A.3: IndexedDB Namespace Resolver

### Changes

**File:** `gestaltd/services/indexeddb/server.go`

- Extend `ServerOptions` with `StoreNames StoreNameResolver` and `StoreTracker NamespaceStoreTracker`.
- Define `StoreNameResolver`, `ResolvedStoreScope`, and `NamespaceStoreTracker` interfaces.
- Add `resolveStoreName(ctx, logical)` helper that:
  - Validates `AllowedStores` against the logical name first.
  - Falls back to identity mapping when no resolver is configured.
  - Returns physical name + scope when a resolver is configured.
- Replace `s.storeName(name)` with `s.resolveStoreName(ctx, name)` in:
  - `CreateObjectStore`, `DeleteObjectStore`
  - `CreateIndex`, `DeleteIndex`
  - `Get`, `GetKey`, `Add`, `Put`, `Delete`, `Clear`
  - `GetAll`, `GetAllKeys`, `Count`, `DeleteRange`
  - `IndexGet`, `IndexGetKey`, `IndexGetAll`, `IndexGetAllKeys`, `IndexCount`, `IndexDelete`
  - `OpenCursor`
  - `Transaction` begin store list and per-operation store access
- Track store creation/deletion through `StoreTracker`.
- Keep metric `ObjectStore` label as the logical name.

**New file:** `gestaltd/services/indexeddb/namespace.go`

- `PhysicalDevelopmentStoreName(namespaceID, logicalName)` helper.
- `RemoteDevelopmentStoreNameResolver` implementing `StoreNameResolver`.

### Success Criteria

- [ ] `go test ./services/indexeddb/... -count=1` passes.
- [ ] New namespace unit tests verify:
  - Same namespace/logical maps identically.
  - Different session/app/logical maps differently.
  - Bounded, opaque physical names.
  - Collision detection fails closed.
  - Identity behavior when no namespace context is present.

## Phase A.4: Relay Token Claims and Context

### Changes

**File:** `gestaltd/services/runtimehost/host_service_relay_token.go`

- Add `IndexedDBNamespaceClaims` type.
- Extend `HostServiceRelayTokenRequest` and `HostServiceRelayTarget` with `IndexedDBNamespace *IndexedDBNamespaceClaims`.
- Add validation at mint and resolve time:
  - Namespace claims only allowed when `Service == "indexeddb"` and `MethodPrefix` is the IndexedDB service prefix or narrower.
  - `AppName`, `SessionID`, `NamespaceID`, `RegistrationID`, and `Generation` must be non-empty/non-zero.
  - A namespace token may not use `MethodPrefix: "/"`.
- Defensively copy namespace claims.

**File:** `gestaltd/services/hostserviceingress/context.go`

- Add `indexedDBNamespaceContextKey{}` and `IndexedDBNamespaceFromContext`.
- In `ApplyCapability`, attach verified namespace claims to context.

**File:** `gestaltd/services/indexeddb/server.go`

- Read namespace claims from context in `RemoteDevelopmentStoreNameResolver`.

### Success Criteria

- [ ] `go test ./services/runtimehost/... -count=1` passes.
- [ ] `go test ./services/hostserviceingress/... -count=1` passes.
- [ ] Token tests cover namespace round-trip, tampering, wrong method/service, and missing fields.

## Phase A.5: RemoteManagement Capability Advertisement

### Changes

**File:** `gestaltd/services/remotemanagement/service.go`

- Implement `PrepareRemoteSession`, `ActivateRemoteSession`, and `HeartbeatRemoteSession` with:
  - Admin owner authorization.
  - Config-authoritative app/provider/database/allowlist resolution.
  - Rejection of unknown or non-eligible apps.
  - Generation and session validation.
  - Minting of host-service relay tokens with `IndexedDBNamespace` claims.
- Update `ListRemotes` to include `RemoteServerCapabilities` with all flags set to `false` in Phase A (or true only behind an opt-in config guard).
- Ensure capability tokens are never returned by `ListRemotes`.

**File:** `gestaltd/internal/server/reverse_remote.go` and `runtime.go`

- Wire the new `RemoteIndexedDBNamespaceService` and pass it to `remotemanagement.New`.

### Success Criteria

- [ ] `go test ./services/remotemanagement/... -count=1` passes.
- [ ] `ListRemotes` includes `RemoteServerCapabilities`.
- [ ] New RPCs are guarded by admin auth and require exact provider sets.

## Phase A.6: Failing End-to-End Test

### Changes

**New test file** under an appropriate e2e / integration package (e.g. `gestaltd/internal/bootstrap` or a dedicated `gestaltd/services/indexeddb` integration test).

The test should:

1. Seed production `tasks` with `task-prod`.
2. Simulate a reverse-injected local app using logical store `tasks`.
3. Before the fix path: assert local writes/clear affect production `tasks` (this demonstrates the current bug).
4. After scoped preparation is wired: assert local count starts at zero, local write is session-scoped, and `task-prod` remains untouched.

In Phase A the test should compile but fail until later phases complete, or be tagged/titled to reflect the expected behavior change.

### Success Criteria

- [ ] Test clearly documents the expected isolation behavior.
- [ ] Test fails before the scoped path is complete and passes after.

## Testing Strategy

- Unit tests for namespace hashing and mapping (`gestaltd/services/indexeddb`).
- Unit tests for token claim validation (`gestaltd/services/runtimehost`).
- Unit tests for context restoration anti-forgery (`gestaltd/services/hostserviceingress`).
- Unit tests for coredata namespace service (`gestaltd/internal/coredata`).
- Service tests for `RemoteManagement` new RPCs (`gestaltd/services/remotemanagement`).

## Migration Notes

- New object stores are created on `coredata.NewWithOptions` bootstrap.
- Existing remote registrations are unaffected; the new `SessionID` and `State` fields default to empty/zero, which should be treated as legacy registrations.
- No migration of existing IndexedDB data is required because physical names are only used when a verified namespace claim is present.

## References

- Original specification: user prompt (session-isolated IndexedDB for reverse-injected local apps).
- `sdk/proto/v1/remote.proto`
- `gestaltd/internal/coredata/remote_registrations.go`
- `gestaltd/services/indexeddb/server.go`
- `gestaltd/services/runtimehost/host_service_relay_token.go`
- `gestaltd/services/hostserviceingress/context.go`
- `gestaltd/internal/bootstrap/host_service_bootstrap.go`
