# Plan: consolidate provider-gateway metric tags into a single `gd.origin`

## Goal

Replace the two overlapping tags on `gestaltd.provider_gateway.*` metrics
(`gd.source` and `gd.invocation_surface`) with **one** tag, `gd.origin`, that
classifies *what kind of caller initiated the invocation*:

| `gd.origin` | Meaning |
|---|---|
| `external` | Entered via an external request boundary — the HTTP invoke API (`gestalt invoke`), HTTP bindings/webhooks, MCP clients |
| `workflow` | Driven by the workflow manager |
| `app_sdk` | An app calling back into gestaltd via the SDK over gRPC |
| `internal` | Server-side system invocations (bootstrap, state setup, background) |
| `unknown` | Fallback — a path that has not been instrumented yet |

Applies to every metric emitted under `gestaltd.provider_gateway.*`
(`operation.count`, `operation.duration`, `operation.error_count`,
`authorization.count`).

## Reasoning

- **Today the two tags are redundant and confusing.** `gd.source` only ever
  takes `sdk_grpc` / `internal` / `N/A` and describes how the *transport* was
  entered; `gd.invocation_surface` describes the originating *surface*
  (`http`/`http_binding`/`mcp`/`unknown`). They answer overlapping questions and
  neither alone tells you "who called this."
- **The question we actually want to answer is caller class.** External API
  traffic vs. workflow-driven vs. app-SDK callbacks vs. internal system calls.
  That is a single, mutually-exclusive dimension, so it belongs in one tag.
- **Most volume is currently `unknown`/`N/A`** because internal originators
  (bootstrap, workflows) and the SDK path go through the same gateway but never
  decorate their context. They *do* flow through the gateway — they just don't
  declare themselves. This plan makes them declare.
- **Keep an explicit `unknown` fallback.** Do **not** let un-instrumented paths
  silently collapse into `internal`. The `unknown` bucket is the signal that a
  code path still needs a stamp; losing it is how we got here.
- **Caller class is the *nearest* caller, not the ultimate origin.** An app SDK
  call that was itself triggered by an external request is reported as
  `app_sdk`. This is intentional and defined by the precedence below.

## Design

### Single context value + resolver

Introduce one context key and a resolver that both metric recorders call.

Precedence (first match wins):

1. `workflow` — set by the workflow manager
2. `app_sdk` — set at SDK-facing gRPC entry points
3. `external` — set at external request boundaries
4. `internal` — explicitly set at system/bootstrap originators
5. `unknown` — default when nothing was stamped

A single explicit stamp per entry point keeps this unambiguous; the resolver
never has to "guess" — it just reads the one value, defaulting to `unknown`.

### Tradeoff (accepted)

`gd.origin` collapses `http` / `http_binding` / `mcp` into `external`, so the
protocol breakdown is lost. If protocol granularity is needed later, add it back
as a *separate* tag rather than sub-typing `external`.

## Code pointers

### New: the origin type + context plumbing

- Define an `Origin` string type with the five constants. Natural home is the
  `invocation` package next to the existing surface enum:
  - `gestaltd/services/invocation/context.go:108-111` — existing
    `InvocationSurface` enum (model the new type after this)
  - `gestaltd/services/invocation/context.go:324-330` — existing
    `WithInvocationSurface` / `InvocationSurfaceFromContext` (model
    `WithOrigin` / `OriginFromContext` after these)
- Import note: `providergateway` already imports `invocation` (added in PR
  #2493), and `invocation` does not depend on `providergateway`, so this stays
  cycle-free.

### The resolver + metric emission

- `gestaltd/services/providergateway/metrics.go:15-25` — tag-key declarations.
  Add `attrProviderGatewayOrigin = attribute.Key("gd.origin")`; **remove**
  `attrProviderGatewaySource` and `attrProviderGatewayInvocationSurface`.
- `gestaltd/services/providergateway/metrics.go:109-129`
  (`recordProviderGatewayOperation`) — replace the `gd.source` and
  `gd.invocation_surface` attributes with a single
  `attrProviderGatewayOrigin.String(resolveOrigin(ctx))`.
- `gestaltd/services/providergateway/metrics.go:72-90`
  (`recordProviderGatewayAuthorizationCheck`) — add the same
  `gd.origin` attribute so all `provider_gateway.*` metrics are consistent.
- Retire `GatewaySource` once `gd.source` is gone:
  - `gestaltd/services/providergateway/types.go:15-19,47` — `GatewaySource`
    type/constants and the `Source` field on `ProviderGatewayRequest`
  - `gestaltd/services/providergateway/context.go:8,13-24` — `WithSource` /
    `SourceFromContext`
  - `gestaltd/rpc/authorization/client.go:151` — currently sets
    `Source: SourceFromContext(ctx)` on the request; drop it
  - `gestaltd/services/authorization/server.go:17-24,105` — `WithGatewaySource`
    option + `providerGatewayContext` that calls `WithSource`; replace with a
    `WithOrigin(ctx, OriginAppSDK)` stamp (see app_sdk below)

### Stamping each origin

- **external** — already partially done; switch these from
  `WithInvocationSurface` to `WithOrigin(ctx, OriginExternal)`:
  - `gestaltd/internal/server/handlers.go:829` (HTTP invoke API / `gestalt invoke`)
  - `gestaltd/internal/server/http_binding_dispatch.go:75` (webhooks)
  - `gestaltd/services/apps/mcp/stateless_http.go:260` (MCP)
- **workflow** — add a stamp on the ctx before the workflow manager invokes:
  - `gestaltd/services/workflows/workflowmanager/manager_target.go` (the target
    execution path that holds `m.invoker`; stamp `WithOrigin(ctx, OriginWorkflow)`
    before the invoke). Confirm exact invoke line during implementation.
- **app_sdk** — stamp at the SDK-facing gRPC entry points:
  - `gestaltd/services/authorization/server.go:105` (authz host service — today's
    `sdk_grpc`)
  - `gestaltd/services/appaccess/app_server.go:62-96` (app-to-app `Invoke`) and
    `:100` (`InvokeGraphQL`). Note `app_server.go:372-373` currently restores a
    propagated surface; reconcile so app-access reports `app_sdk` as the
    immediate caller class.
- **internal** — stamp system/bootstrap originators:
  - `gestaltd/internal/bootstrap/authorization_bootstrap.go:24-61`
    (`bootstrapAuthorizationProviderState` → `SetAuthorizationState`)
  - audit other in-process callers of the invoker that aren't covered above and
    stamp `OriginInternal`; anything missed correctly falls through to `unknown`.

### Cross-process propagation (follow-up, optional)

The `RequestContext` proto already carries `Invocation.Surface`
(`appaccess/app_server.go:372`, `apps/provider_server.go:165`). If we want origin
to survive app→app / plugin hops the same way, add an `origin` field to that
proto and restore it on the receiving side. Not required for the first cut —
without it, hops resolve to the nearest caller class, which is acceptable.

## Rollout / sequencing

1. Add `Origin` type + `WithOrigin`/`OriginFromContext` + `resolveOrigin`.
2. Swap emission in both recorders to `gd.origin`; delete the two old tags.
3. Add stamps at all entry points (external, workflow, app_sdk, internal).
4. Remove the now-dead `GatewaySource` plumbing.
5. `go build ./...`; update any dashboards/monitors referencing `gd.source` or
   `gd.invocation_surface` to `gd.origin`.

Because `gd.invocation_surface` is only days old (PR #2493) and not yet relied
on, retiring it now is low-cost.

## Verification

- After deploy, `sum:gestaltd.provider_gateway.operation.count{*} by {gd.origin}`
  should show `external` / `workflow` / `app_sdk` / `internal` populated and
  `unknown` trending toward zero.
- A non-zero `unknown` is the signal that an invocation entry point still needs a
  stamp — treat it as a TODO, not noise.
