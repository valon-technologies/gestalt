# Managed Agent Identities Implementation Plan

Status: draft implementation plan with PR1 implementation underway
Last updated: 2026-04-15
Depends on: `docs/internal/managed-agent-identities.md`

## Goal

Implement runtime-managed non-human identities across:

- `gestaltd` backend + HTTP API
- CLI
- default UI client in `~/src/gestalt-providers/ui/default`

The implementation should be additive and preserve existing behavior for:

- human users
- static YAML workloads under `authorization.workloads`
- the legacy deployment-wide shared `identity` credential owner

## Delivery Strategy

Use vertical, functionally testable PRs rather than one cross-repo mega-change.

Recommended rollout:

1. PR1: identity resources, memberships, grants, and initial CLI management
2. PR2: identity API tokens, managed-identity principals, and mode-`none` invocation
3. PR3: identity-owned upstream connections, OAuth/manual connect, and credentialed invocation
4. PR4: web UI management surface, plus any cleanup needed after the backend/CLI rollout

This keeps each PR integration-testable while still moving toward the full product.

## Core Design Decisions

### 1. Treat managed identities as a distinct principal kind

Do not overload them as:

- human users
- static workloads
- the fixed `identity:__identity__` owner

Add a new principal kind for managed identities and add helper methods for:

- human-only checks
- non-human checks
- identity checks

### 2. Generalize permissions separately from ownership

There are two different permission layers:

- identity grant permissions
- token permissions

They should use the same structured representation, and effective runtime access should be their intersection.

### 3. Minimize schema churn where possible

For V1, prefer additive schema changes rather than renaming legacy fields in-place unless the migration path is clearly safe.

Implication:

- `api_tokens` should be generalized in PR2
- `integration_tokens` can be generalized in PR3 when identity-owned credentials are added

## Proposed Data Model

### New store: `identities`

Purpose:

- identity resource metadata

Fields:

- `id` string PK
- `display_name` string
- `created_at` time
- `updated_at` time

Indexes:

- `by_display_name`

Notes:

- Keep V1 metadata minimal.
- No single owner field is required because the resource is workspace-owned.

### New store: `identity_memberships`

Purpose:

- human membership on an identity

Fields:

- `id` string PK
- `identity_id` string
- `user_id` string
- `email` string
- `role` string
- `created_at` time
- `updated_at` time

Indexes:

- `by_identity`
- `by_identity_user` unique on `(identity_id, user_id)`
- optional `by_user`

Notes:

- Store both `user_id` and normalized `email`, matching the style used in plugin authorizations.
- Use `FindOrCreateUser` on writes so user/email sharing stays user-backed.

### New store: `identity_grants`

Purpose:

- plugin and operation access for an identity

Fields:

- `id` string PK
- `identity_id` string
- `plugin` string
- `permissions_json` string
- `created_at` time
- `updated_at` time

Indexes:

- `by_identity`
- `by_identity_plugin` unique on `(identity_id, plugin)`

Notes:

- One row per identity/plugin is simplest.
- `permissions_json` stores either:
  - plugin-wide access
  - operation subset

### Existing store changes: `api_tokens`

Purpose:

- support both user-owned tokens and identity-owned tokens

Proposed additive fields:

- `owner_kind` string
- `owner_id` string
- `permissions_json` string

Keep existing fields temporarily for compatibility:

- `user_id`
- `scopes`

Compatibility interpretation:

- legacy user token:
  - `owner_kind = "user"` if present, else implied from `user_id`
  - `permissions_json` empty
  - `scopes` remains plugin-wide allowlist
- new identity token:
  - `owner_kind = "identity"`
  - `owner_id = <identity_id>`
  - `permissions_json` required

Indexes:

- existing hash index remains
- add `by_owner` on `(owner_kind, owner_id)`
- keep `by_user` and `by_user_id` until all old call sites are migrated

### Existing store changes: `integration_tokens` in PR3

Purpose:

- allow identity-owned upstream credentials

Recommended additive fields:

- `owner_kind` string
- `owner_id` string

Keep legacy `user_id` field during migration if needed.

Indexes:

- add `by_owner`
- add `by_owner_integration`
- add `by_owner_connection`
- add `by_owner_lookup` unique on `(owner_kind, owner_id, integration, connection, instance)`

PR3 can then switch runtime lookup over to owner-based access without breaking old records.

## Shared Permission Shape

Use a shared structured representation for:

- identity grants
- token permissions

Proposed Go shape:

```go
type AccessPermission struct {
    Plugin     string   `json:"plugin"`
    Operations []string `json:"operations,omitempty"`
}
```

Semantics:

- `Operations == nil` or empty: plugin-wide
- non-empty operations: explicit subset only

Recommended helper shape at runtime:

```go
type ResolvedPluginPermission struct {
    AllOperations bool
    Operations    map[string]struct{}
}
```

This avoids repeated JSON decoding and simplifies intersection logic.

## Principal Model Changes

### `principal.Kind`

Add:

```go
const (
    KindUser     Kind = "user"
    KindWorkload Kind = "workload"
    KindIdentity Kind = "identity"
)
```

Extend `principal.Principal` with:

- `IdentityID string`
- structured token permissions for API-token-backed principals

Recommended helper methods:

- `IsHuman()`
- `IsWorkload()`
- `IsManagedIdentity()`
- `IsNonHuman()`

### Principal resolution

Update `principal.Resolver` so `gst_api_...` tokens can resolve to:

- user principal
- managed identity principal

Resolution rules:

- owner kind `user` => existing behavior
- owner kind `identity` => `KindIdentity`, `IdentityID`, `SubjectID = "identity:<id>"`

## Authorization Model Changes

The authorization layer should handle three separate concerns:

1. human plugin authorization
2. static workload allowlists and bindings
3. managed identity grants

These should not be collapsed together.

### Human plugin authorization

Keep existing `authorization.policies` + dynamic plugin authorizations.

Add one new rule for identity management:

- when listing or mutating an identity's plugin grants / connections, only expose plugins the current human is authorized for
- when mutating an identity's operation-scoped grants, only accept operations the current human can currently invoke
- when mutating a plugin-wide grant, only accept it if the current human can invoke every visible operation on that plugin

### Static workloads

Keep existing workload semantics unchanged.

### Managed identities

Add authorizer support for:

- loading an identity's plugin grants
- checking whether a managed identity is allowed to access a plugin
- checking whether a managed identity is allowed to access an operation

Effective runtime rule:

```text
allowed(identity token, plugin/op) =
    identity_grant(identity, plugin/op)
    intersect
    token_permission(token, plugin/op)
```

## HTTP API Plan

### PR1: identity core

Add:

- `GET /api/v1/identities`
- `POST /api/v1/identities`
- `GET /api/v1/identities/{identityId}`
- `DELETE /api/v1/identities/{identityId}` maybe defer to PR2 if delete gating is easier after memberships exist
- `GET /api/v1/identities/{identityId}/grants`
- `PUT /api/v1/identities/{identityId}/grants/{plugin}`
- `DELETE /api/v1/identities/{identityId}/grants/{plugin}`
- `GET /api/v1/identities/{identityId}/tokens`
- `POST /api/v1/identities/{identityId}/tokens`
- `DELETE /api/v1/identities/{identityId}/tokens/{tokenId}`

Suggested request body for grant upsert:

```json
{
  "operations": ["get_forecast", "get_alerts"]
}
```

Plugin-wide grant example:

```json
{
  "operations": []
}
```

Suggested token create request:

```json
{
  "name": "automation",
  "permissions": [
    { "plugin": "weather", "operations": ["get_forecast"] },
    { "plugin": "news" }
  ]
}
```

Suggested token create response:

```json
{
  "id": "tok_123",
  "name": "automation",
  "token": "gst_api_..."
}
```

### PR2: memberships and role enforcement

Add:

- `PATCH /api/v1/identities/{identityId}` or `PUT` for metadata update
- `GET /api/v1/identities/{identityId}/members`
- `PUT /api/v1/identities/{identityId}/members`
- `DELETE /api/v1/identities/{identityId}/members/{userId}`

Suggested membership upsert body:

```json
{
  "email": "editor@example.com",
  "role": "editor"
}
```

Response bodies should include the caller's effective role for the identity to simplify CLI/UI logic.

### PR3: connections

Add:

- `GET /api/v1/identities/{identityId}/integrations`
- `DELETE /api/v1/identities/{identityId}/integrations/{name}`
- `POST /api/v1/identities/{identityId}/auth/start-oauth`
- `POST /api/v1/identities/{identityId}/auth/connect-manual`

These should mirror existing user-scoped integration routes, but act on identity-owned credentials instead of human-owned credentials.

## CLI Plan

### Command shape

Add a top-level `identities` command family.

Recommended structure:

```text
gestalt identities list
gestalt identities create --name <display-name>
gestalt identities get <identity-id>
gestalt identities update <identity-id> --name <display-name>
gestalt identities delete <identity-id>

gestalt identities grants list <identity-id>
gestalt identities grants put <identity-id> <plugin> [--operation op ...]
gestalt identities grants revoke <identity-id> <plugin>

gestalt identities tokens list <identity-id>
gestalt identities tokens create <identity-id> --name <name> --permission plugin[:op][,op] ...
gestalt identities tokens revoke <identity-id> <token-id>

gestalt identities members list <identity-id>
gestalt identities members put <identity-id> --email <email> --role <viewer|editor|admin>
gestalt identities members revoke <identity-id> <user-id>

gestalt identities connect <identity-id> <plugin> [--connection ...] [--instance ...]
gestalt identities disconnect <identity-id> <plugin> [--connection ...] [--instance ...]
```

Notes:

- Keep existing `tokens` and `plugins` user-scoped.
- Avoid adding identity targeting flags to existing user-scoped commands in PR1.
- Add identity-targeted connect/disconnect in PR3 once the backend exists.

### Files to change

- `gestalt/cli/src/cli.rs`
- `gestalt/cli/src/commands/mod.rs`
- new `gestalt/cli/src/commands/identities.rs`
- `gestalt/cli/src/api.rs`
- `gestalt/cli/tests/integration.rs`

## Default Web Client Plan

### PR1 pages

Add new identity-focused routes rather than overloading `/tokens` and `/integrations`.

Recommended pages:

- `/identities`
- `/identities/[id]`
- `/identities/[id]/tokens`
- `/identities/[id]/grants`

### PR2 pages

- `/identities/[id]/members`

### PR3 pages

- `/identities/[id]/integrations`

### Web API client changes

Add identity-focused API helpers in:

- `src/lib/api.ts`

Examples:

- `getIdentities()`
- `createIdentity()`
- `getIdentity(id)`
- `getIdentityTokens(id)`
- `createIdentityToken(id, ...)`
- `getIdentityGrants(id)`
- `putIdentityGrant(id, plugin, operations)`
- `getIdentityMembers(id)`
- `putIdentityMember(id, email, role)`
- `getIdentityIntegrations(id)`
- `startIdentityIntegrationOAuth(...)`

### Reusable components

Reuse from current UI where practical:

- token table/create form
- integration card/settings modal

But do not try to force identity management into the existing user-only pages.

### Files likely involved

- `src/components/Nav.tsx`
- `src/lib/api.ts`
- new identity page routes under `src/app/identities/...`
- token/grant/member/integration focused components
- e2e specs in `e2e/*`

## Test Plan

The user requested:

- no unit tests
- prefer augmenting similar existing tests
- new tests only when behavior is truly new

### PR1 tests

Use:

- `gestaltd/cmd/gestaltd/e2e_test.go`
- `gestalt/cli/tests/integration.rs`
- default web client mocked e2e tests

PR1 scenarios:

- create identity
- create plugin grant
- create identity token with explicit permissions
- invoke a `mode:none` plugin using the identity token
- reject identity token creation with empty permissions
- show identity list/detail/tokens/grants in UI

### PR2 tests

Use the same suites.

PR2 scenarios:

- creator seeded as admin
- admin can share
- viewer can list/create tokens but not revoke or mutate grants
- editor can mutate grants and revoke tokens but not share/update/delete
- hidden plugin config when the human no longer has authorization to manage that plugin

### PR3 tests

Use the same suites and extend existing similar connect-flow coverage.

PR3 scenarios:

- connect manual on behalf of identity
- connect OAuth on behalf of identity
- list identity integrations / instances
- invoke a credentialed plugin through identity-owned credentials
- disconnect an identity-owned integration

## PR Breakdown

### PR1: Identity Core

Scope:

- identity metadata store
- identity grant store
- generalized API token ownership
- managed identity principal resolution
- identity token permission model
- runtime permission intersection
- identity CRUD, grants, tokens API
- CLI identities + identity tokens/grants
- web identity list/detail/tokens/grants
- mode-`none` functional coverage

Definition of done:

- a user can create an identity
- grant a `mode:none` plugin
- mint a token for that identity
- invoke the granted operation with that token

### PR2: Sharing and Management Roles

Scope:

- identity membership store + API
- viewer/editor/admin enforcement
- seeded creator admin
- identity metadata update/delete
- visibility filtering for plugin management
- CLI members/admin workflows
- web members and role-gated actions

Definition of done:

- two humans can share and manage the same identity according to role
- plugin grants and connections are hidden from humans who lose plugin authorization

### PR3: Identity-Owned Connections

Scope:

- generalized integration token ownership
- identity-scoped integration list/connect/disconnect
- identity OAuth/manual connect flows
- credentialed invocation using identity-owned tokens
- CLI identity connect/disconnect
- web identity integrations page

Definition of done:

- a shared identity can own upstream OAuth/manual credentials independently of any human user and use them for invocation

## Branch / PR Process

The user asked for:

- parallel agents/worktrees if useful
- `gpt-5.4` + `xhigh` reviewer/worker agents
- one squash commit per PR
- every finished PR to be reviewed by a separate reviewer agent
- PR descriptions to include:
  - new Go interfaces
  - YAML/config surface
  - example usage

Recommended working process:

1. Create one branch per PR slice.
2. Use worker agents for disjoint implementation areas:
   - backend/API
   - CLI
   - default web client
3. When a branch is functionally complete:
   - run the relevant integration / e2e suites
   - squash to a single commit
   - open a PR
4. Spawn a fresh reviewer agent on that branch.
5. Apply reviewer feedback.
6. Pull PR review comments, address them, and push updates.

## Go Interface Examples

These are planning targets, not yet locked code.

```go
type ManagedIdentity struct {
    ID          string
    DisplayName string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type ManagedIdentityMembership struct {
    IdentityID string
    SubjectID  string
    Email      string
    Role       string
}

type AccessPermission struct {
    Plugin     string
    Operations []string
}
```

## YAML / Config Examples

Managed identities are runtime-managed, so they do not require new YAML.

The relevant YAML context that remains valid is:

```yaml
server:
  providers:
    auth: google

plugins:
  weather:
    source:
      path: ./plugins/weather/manifest.yaml
    authorizationPolicy: weather_access
    connections:
      default:
        mode: user
        auth:
          type: manual
```

Planning note:

- no new YAML is required to create managed identities
- the semantic change is that plugin modes like `user` and `either` will eventually be usable by managed identities once PR3 lands

## Open Implementation Questions

These are engineering questions, not product blockers:

- whether to introduce a small internal `permissions` package for shared intersection logic
- whether identity delete should hard-delete immediately or first validate that all child cleanup succeeded transactionally
- whether identity route responses should include explicit capabilities booleans in addition to the caller role
- whether PR1 should include delete or defer it to PR2 when memberships/roles are present

Current recommendation:

- keep permissions logic internal but shared
- fail identity delete if any child cleanup fails
- include `role` first, add explicit booleans only if the web client needs them
- defer update/delete to PR2 if it keeps PR1 smaller
