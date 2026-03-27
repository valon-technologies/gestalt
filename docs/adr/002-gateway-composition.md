# ADR 002: Gateway Composition

## Status

Accepted

## Context

Gestalt currently assumes every proxy binding has exactly one provider configured. However, a gateway-only deployment (auth + datastore + secret manager + proxy binding + static egress policy) is a useful shape that does not require any provider. We need to clarify which components are required, which are optional, and how the gateway relates to the full Gestalt deployment.

## Decision

1. **Gateway-only is a valid deployment shape.** A deployment with auth, datastore, secret manager, proxy binding, and static egress policy is a supported composition of existing parts. It does not require providers.

2. **Full Gestalt reuses the same substrate.** The full deployment uses the exact same auth and egress infrastructure as the gateway-only shape. Providers are additive.

3. **Providers are optional capability adapters.** Providers supply credentials and policy for specific upstream services. A proxy binding with zero providers forwards requests without injecting provider credentials. A proxy binding with one provider resolves credentials through that provider. Two or more providers on a single binding are rejected.

4. **Egress policy is config-defined.** Policy rules are declared in the `egress:` config block and evaluated at request time. There is no runtime API for creating or mutating rules.

5. **Proxy credentials are secret-backed.** Credential grants reference secrets via `secret_ref` and are resolved through the configured secret manager at request time.

6. **Gateway-only is a composition, not a separate runtime.** A later gateway-service step can improve listener and routing ergonomics, but the gateway is not a separately optimized runtime surface.

## Consequences

- The proxy factory must accept zero providers (not just exactly one).
- Tests must cover the zero-provider path.
- Future PRs can layer additional gateway capabilities on this foundation without restructuring.
