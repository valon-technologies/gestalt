# ADR 004: EgressClient Scope

## Status

Accepted

## Context

EgressClient currently has a `UNIQUE(created_by_id, name)` constraint, and `resolveOwnedEgressClient` enforces strict creator ownership: a user can only see and manage clients they created. There is no mechanism for operator-managed or shared clients. This means an operator cannot provision a client for a workload and hand off the token, and two users cannot share a client identity for the same external service.

The gateway deployment shape (ADR 002) defers shared/workload EgressClient scope, but the need is becoming concrete as operators want to pre-provision machine callers that are not tied to a specific user's session.

## Decision

1. **Add a scope column to EgressClient.** Scope values are `personal` (current behavior, default) and `global`. Personal clients retain `UNIQUE(created_by_id, name)` semantics. Global clients have `UNIQUE(name)` within the global scope.

2. **`admin_emails` gates global client management.** Only admin users (as determined by `admin_emails` configuration) can create, list, and delete global-scoped clients. Personal client management remains available to all authenticated users.

3. **`CreatedByID` stays as audit metadata.** Global clients still record who created them, but ownership checks are replaced by scope-based authorization. Personal clients continue to use creator-based ownership.

4. **No teams or groups scope.** The first non-personal scope is global. Organization, team, or group scoping is deferred until there is a concrete need and an identity model to support it.

5. **Listing behavior is scope-aware.** A non-admin user listing clients sees only their personal clients. An admin listing clients sees their personal clients plus all global clients (or can filter by scope).

## Consequences

- A future schema migration adds a `scope` column (default `personal`) to the `egress_clients` table and updates the uniqueness constraint.
- The API gains a `scope` parameter on create and a `scope` filter on list.
- `resolveOwnedEgressClient` evolves into scope-aware authorization: personal clients check creator, global clients check admin status.
- Token authentication for egress clients is unaffected; `gst_ec_` tokens resolve to a client regardless of scope.
