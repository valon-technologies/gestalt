# Managed Agent Identities

Status: draft planning doc
Last updated: 2026-04-15
Audience: product + engineering

## Summary

Gestalt should support runtime-managed non-human identities that:

- are persisted workspace-owned resources
- authenticate as true non-human principals
- own their own integration credentials
- own their own API tokens
- can be shared with humans using `viewer`, `editor`, and `admin` roles
- can be granted explicit access to plugins, optionally narrowed to specific operations

This is intentionally additive in V1. Existing static YAML workloads and the current deployment-wide shared `identity` owner should continue to work unchanged.

## Why This Exists

The current model has three separate concepts:

- human users with user-owned API tokens
- static YAML workloads under `authorization.workloads`
- a single deployment-wide shared plugin `identity` owner

That model is too rigid for agentic use cases. We need a managed resource that represents a reusable non-human actor, can be shared between humans, and can own plugin credentials independently from any single person.

## Current State

Today:

- workload callers are static config entries, not runtime-managed resources
- workload callers are limited to `identity` and `none` providers
- plugin `identity` credentials are stored against one fixed internal owner (`__identity__`)
- API tokens are user-owned
- plugin authorization sharing exists only for human users, not for non-human identities
- the default web client and CLI only expose user-scoped token and integration management

## Target Resource Model

An agent identity is a persisted workspace-owned resource with:

- `id`
- `displayName`
- timestamps
- human memberships
- plugin grants
- plugin connections / stored upstream credentials
- API tokens

Properties:

- It is not a human user.
- It is not just a relabeled user API token.
- It is not required to have a single owner.
- It is managed at runtime through API/CLI/web UI.

## Locked Product Requirements

### Identity lifecycle

- Identities are runtime-managed.
- Identities are workspace-owned resources.
- Platform auth must be enabled for identity management.
- V1 should coexist with:
  - static YAML workloads
  - the legacy deployment-wide shared `identity` credential owner

### Human sharing model

- Membership is user/email-based only in V1.
- Membership roles are `viewer`, `editor`, and `admin`.
- The creator should be seeded as an `admin`.

### Identity runtime model

- Identity API calls authenticate as a true non-human principal.
- Anyone with a valid identity API token may invoke using that identity.
- Invocation is not further constrained by the current human viewer/editor/admin memberships.

### Plugin access model

- Identity plugin access is explicit.
- Even `mode: none` plugins must be explicitly granted.
- Identity grants may be:
  - plugin-wide
  - operation-scoped
- If no operations are specified for a granted plugin, the identity has plugin-wide access for that plugin.
- If operations are specified, the identity only has access to those operations.

### Token model

- Identity API tokens should behave like existing `gst_api_...` tokens:
  - one-time plaintext display at creation
  - hashed at rest
- Identity API tokens do not inherit full identity access automatically.
- Token permissions must be explicitly declared.
- Token permissions are plugin + operation granular.
- Creating an identity token with no permissions should be rejected.
- Effective runtime access should be the intersection of:
  - the identity's own grants
  - the token's declared permissions

### Connection model

- Identities own their own stored upstream credentials.
- Those credentials are independent from any human user's stored credentials.
- Identities should support the same multi-connection / multi-instance behavior users have today.
- OAuth and manual connect should both work when performed on behalf of an identity.
- Identity `editors` and `admins` can complete OAuth/manual connect flows on behalf of the identity.

### Human authorization ceiling

- When a human configures an identity's plugin access, they may only grant access to plugins that the human is themselves authorized for.
- When a grant is operation-scoped, the selected operations must also be invokable by the configuring human.
- A plugin-wide grant is only valid when the configuring human can invoke every visible operation on that plugin.
- If a human later loses authorization for a plugin, the identity should keep working until explicitly changed.
- In that case the plugin's config should be hidden from that human rather than auto-revoked.

### Deletion semantics

- Delete is a hard delete.
- Deleting an identity should cascade and remove:
  - memberships
  - plugin grants
  - stored integration credentials
  - API tokens

## Role And Capability Matrix

This is the locked product behavior.

| Capability | Viewer | Editor | Admin |
| --- | --- | --- | --- |
| View identity metadata | Yes | Yes | Yes |
| View memberships | Yes | Yes | Yes |
| View plugin grants | Yes | Yes | Yes |
| View connections | Yes | Yes | Yes |
| List tokens | Yes | Yes | Yes |
| Create token | Yes | Yes | Yes |
| Revoke token | No | Yes | Yes |
| Connect plugin | No | Yes | Yes |
| Disconnect plugin | No | Yes | Yes |
| Create/update/remove plugin grants | No | Yes | Yes |
| Rename/update identity metadata | No | No | Yes |
| Manage sharing | No | No | Yes |
| Delete identity | No | No | Yes |

Planning note:

- This doc treats plugin grant management as `editor`+`admin`, because the product direction is that `editor` can manage the operational state of the identity and was only explicitly excluded from sharing, rename/update, and delete.

## Effective Access Matrix

| Identity grant | Token permission | Effective result |
| --- | --- | --- |
| none | any | deny |
| plugin-wide | plugin-wide | allow full plugin access |
| plugin-wide | operation subset | allow only token subset |
| operation subset | plugin-wide | allow only identity subset |
| operation subset A | operation subset B | allow intersection of A and B |
| any | empty token permissions | token creation rejected |

Additional rules:

- `mode: none` plugins still require an identity grant.
- Identity grants do not auto-expand just because the identity has a connection.
- Token permissions never exceed the identity's own grants.

## Feature / Product Matrix

This matrix captures the minimum V1 surface area.

| Feature | Backend / Data Model | HTTP API | CLI | Default Web UI | Notes |
| --- | --- | --- | --- | --- | --- |
| Identity CRUD | Required | Required | Required | Required | Workspace-owned runtime resources |
| Membership sharing | Required | Required | Required | Required | User/email-based only |
| Viewer/editor/admin enforcement | Required | Required | Required | Required | UI should mirror server enforcement |
| Plugin grant management | Required | Required | Required | Required | Plugin-wide or operation-scoped |
| Visibility filtering by human auth | Required | Required | Required | Required | Hidden, not auto-revoked |
| Identity-owned integration credentials | Required | Required | Required | Required | Independent from human credentials |
| OAuth on behalf of identity | Required | Required | Required | Required | Editor/admin only |
| Manual connect on behalf of identity | Required | Required | Required | Required | Editor/admin only |
| Multi-instance connection support | Required | Required | Required | Required | Same model as users today |
| Identity API token creation/list/revoke | Required | Required | Required | Required | Viewer can list/create only |
| Token permission model | Required | Required | Required | Required | Plugin + operation granular |
| True non-human principal resolution | Required | Implicit | Implicit | Implicit | Not a user alias |
| Invocation permission intersection | Required | Implicit | Implicit | Implicit | Identity grant intersect token permission |
| Hard delete cascade | Required | Required | Required | Required | Remove tokens, grants, memberships, creds |
| Legacy workload coexistence | Required | N/A | N/A | N/A | Additive V1, no forced migration |
| Docs and tests | Required | Required | Required | Required | Include user-facing docs + e2e |

## Recommended API Shape

Endpoint names are not locked, but V1 likely needs resource families like:

- `/api/v1/identities`
- `/api/v1/identities/{identityId}`
- `/api/v1/identities/{identityId}/members`
- `/api/v1/identities/{identityId}/grants`
- `/api/v1/identities/{identityId}/tokens`
- `/api/v1/identities/{identityId}/integrations`
- identity-targeted connect/disconnect OAuth/manual flows

Important constraint:

- Existing user-scoped routes such as `/api/v1/tokens` and `/api/v1/integrations` should continue to work for human users without breaking changes.

## Implementation Implications

### Storage and ownership

The current data model is user-centric. V1 likely needs:

- a new identities store
- a new identity memberships store
- a new identity grants store
- generalized credential ownership for:
  - integration tokens
  - API tokens

The current `user_id`-only ownership model is not sufficient for managed identities.

### Principal model

The current principal model distinguishes users and workloads. Managed identities should be treated as a true non-human principal. The implementation can:

- introduce a new principal kind
- or generalize the existing non-human model cleanly

The implementation should not keep pretending a managed identity is just the legacy fixed shared identity owner.

### Permission representation

The current user API token `scopes` field is plugin-only and string-based. Managed identities require structured permission data that can represent:

- plugin-wide permission
- operation subsets

That likely means a new structured permissions representation for identity tokens, and possibly eventually for user tokens too.

### Visibility vs runtime validity

There are two separate checks:

- runtime invocation validity for an identity token
- human viewer visibility of an identity's configuration

Those checks should remain separate:

- identity runtime access keeps working unless explicitly changed
- humans who lose plugin authorization should simply stop seeing that plugin in the identity management surface

## Non-Goals For V1

- replacing static YAML workloads
- removing the existing deployment-wide shared `identity` owner
- team/group-based sharing
- auto-revocation when human permissions change
- implicit plugin access for `mode: none`
- identity tokens with blank permissions

## Default UX Expectations

### CLI

The CLI should add identity-scoped management rather than force everything through the user-scoped commands.

Likely families:

- `gestalt identities list`
- `gestalt identities create`
- `gestalt identities get`
- `gestalt identities update`
- `gestalt identities delete`
- `gestalt identities members ...`
- `gestalt identities grants ...`
- `gestalt identities tokens ...`
- `gestalt identities connect ...`
- `gestalt identities disconnect ...`

### Default web client

The default web client should gain identity management pages rather than trying to overload the current user-only:

- `/integrations`
- `/tokens`

Expected V1 web surfaces:

- identity list
- identity detail
- identity members
- identity grants
- identity connections
- identity tokens

The UI should hide actions the current member role cannot perform and hide plugins the current human is no longer authorized to manage.

## Phased Delivery

Recommended order:

1. Backend data model and principal generalization
2. Authorization and invocation enforcement
3. Identity HTTP API
4. CLI support
5. Default web client support
6. Docs and e2e / integration tests

## Migration / Compatibility Strategy

V1 should be additive:

- keep `authorization.workloads`
- keep the legacy shared `identity` owner
- introduce managed identities alongside both

That is the simplest migration path and avoids forcing existing deployments to rework current identity-mode plugins before the new model is stable.

## Risks To Watch

- overloading the existing `user_id` ownership model too far instead of introducing a clearer principal-owner abstraction
- mixing human visibility rules with token runtime authorization
- building plugin grants only at plugin-level and then backfilling operation scoping later
- leaking hidden plugin config to humans who no longer have authorization to manage it
- making CLI and web UX user-centric in a way that blocks identity management workflows

## Decision Log

Locked decisions captured here:

- true non-human principal, not user-token aliasing
- runtime-managed identities
- independent identity-owned credentials
- workspace-owned identities
- user/email-based sharing only in V1
- plugin grants explicit for all plugins, including `mode: none`
- operation scoping supported at both identity-grant and token-permission layers
- token creation rejected when permissions are empty
- plugin-level human authorization ceiling when configuring identity access
- no auto-revocation when human authorization later changes
- hard-delete cascade
- additive coexistence with legacy workloads and legacy shared identity owner
