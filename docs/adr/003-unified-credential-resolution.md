# ADR 003: Unified Credential Resolution

## Status

Proposed

## Context

Gestalt resolves credentials through two independent paths. The data plane (egress proxy) resolves credentials via subject+target grant matching through `CredentialSourceChain`, where a `ProviderCredentialResolver` iterates `CredentialGrant` entries with `MatchCriteria` to find a (provider, instance) pair and then materializes a token. The capability plane resolves credentials via the provider's `ConnectionMode` enum (`none`, `user`, `identity`, `either`) and per-user token lookup in `internal/invocation/broker.go`. Both paths ultimately call into the same datastore token storage, but they arrive there through different control structures: grants with match criteria vs. connection mode switch statements.

This divergence means that adding a new credential source (e.g., secret-backed credentials, workload identity) requires updating both paths independently. It also means the capability plane cannot benefit from host/path-based grant targeting, and the egress plane cannot express "use the identity-level token when no user token exists" without reimplementing the `ConnectionModeEither` fallback logic.

## Decision

1. **Capability invocation should eventually use the same grant source chain as egress.** The `Broker.resolveToken` method's `ConnectionMode` switch would be replaced by grant evaluation, where provider connection modes become one credential source (an adapter that produces grants from provider config) rather than the root control model.

2. **Provider (provider, instance) maps to host/tenant via grant match criteria.** A capability invocation for `(provider=shopify, instance=shop-1)` is equivalent to an egress grant matching `host=shop-1.myshopify.com`. The grant abstraction unifies both addressing schemes.

3. **Migration is incremental, not a big-bang rewrite.** The broker continues to use `ConnectionMode` today. Each provider's connection mode can be wrapped as a credential source adapter one at a time. The grant chain accepts multiple sources, so old and new paths coexist.

4. **No new credential source types are introduced by this ADR.** This decision covers the convergence direction, not new features. Secret-backed credentials and workload identity are separate decisions that benefit from this convergence.

## Consequences

- Provider `ConnectionMode` stays as-is for now. No immediate code changes are required.
- Future work can wrap each `ConnectionMode` variant as a `CredentialResolver` adapter, allowing the broker to delegate to `CredentialSourceChain` instead of switching on mode directly.
- The `ProviderTokenResolver` interface already bridges the two planes (the broker satisfies it for egress). Unification would make this bridge the primary path rather than a secondary one.
- Grant match criteria may need to be extended to support provider/instance addressing alongside host/path addressing.
