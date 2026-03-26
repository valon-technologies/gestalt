# ADR 002: Gateway Composition

## Status

Accepted

## Context

Gestalt currently assumes every proxy binding has exactly one provider configured. However, a gateway-only deployment (auth + datastore + secret manager + proxy binding + saved deny rules) is a useful shape that does not require any provider. We need to clarify which components are required, which are optional, and how the gateway relates to the full Gestalt deployment.

## Decision

1. **Gateway-only is a valid deployment shape.** A deployment with auth, datastore, secret manager, proxy binding, and saved deny rules is a supported composition of existing parts. It does not require providers.

2. **Full Gestalt reuses the same substrate.** The full deployment uses the exact same auth and egress infrastructure as the gateway-only shape. Providers are additive.

3. **Providers are optional capability adapters.** Providers supply credentials and policy for specific upstream services. A proxy binding with zero providers forwards requests without injecting provider credentials. A proxy binding with one provider resolves credentials through that provider. Two or more providers on a single binding are rejected.

4. **v1 saved rules are deny-only.** The first version of saved rules supports deny rules only. Allow rules are deferred.

5. **v1 admin control uses `admin_emails`.** Administrative access is governed by the `admin_emails` configuration field.

6. **Shared/workload EgressClient scope is deferred.** Scoping EgressClient to shared or per-workload contexts is out of scope for v1.

7. **Gateway-only is a composition, not a separate runtime.** A later gateway-service step can improve listener and routing ergonomics, but the gateway is not a separately optimized runtime surface.

## Consequences

- The proxy factory must accept zero providers (not just exactly one).
- Tests must cover the zero-provider path.
- Future PRs can layer provider-backed and gateway-service capabilities on this foundation without restructuring.
