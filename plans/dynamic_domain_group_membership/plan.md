# Dynamic Domain Group Membership

## Decision

Add a high-level Gestalt configuration primitive for groups whose membership is derived from a verified identity domain:

```yaml
authorization:
  groups:
    valon-employees:
      verifiedEmailDomains:
        - valon.com
```

Bootstrap compiles this into a static selector relationship, conceptually:

```text
group:valon-employees#member <- subject where identity.email_domain == valon.com
```

The authorization provider evaluates the selector for each request. Bootstrap must not enumerate the users that currently have matching email addresses: that would still miss users whose first login happens after the latest deployment.

Explicit `group#member` relationships remain valid and are ORed with derived membership, preserving a manual exception mechanism.

## Authorization contract

Extend [`sdk/proto/v1/authorization.proto`](../../sdk/proto/v1/authorization.proto) with a `SubjectSelector` relationship target containing a subject type and exact property constraints. Regenerate all SDK bindings and conversions.

Extend [`gestaltd/internal/config/config.go`](../../gestaltd/internal/config/config.go) with `authorization.groups.<id>.verifiedEmailDomains`. Normalize domains to lowercase exact DNS names and reject wildcards, email addresses, URLs, and empty values. Update strict config validation and bump the configuration API version if needed so an older daemon fails clearly instead of ignoring the rule.

Update [`gestaltd/internal/bootstrap/authorization_bootstrap.go`](../../gestaltd/internal/bootstrap/authorization_bootstrap.go) to compile each configured domain into a static selector membership while preserving runtime relationships. The authorization state digest must remain deterministic.

Add configuration and bootstrap tests covering multiple domains, explicit plus derived membership, malformed domains, deterministic state, and static/runtime relationship preservation.

## Trusted identity attributes

The authorization evaluator currently receives a canonical user ID plus scope, client ID, and audience. Extend the shared subject-property builder in [`gestaltd/services/invocation/broker.go`](../../gestaltd/services/invocation/broker.go) with a reserved `identity.email_domain` property only for bearer-authenticated users enriched through identity-provider UserInfo.

Use one shared builder for operation invocation, batched listings, mounted UIs, MCP, and admin HTTP checks so every surface asks the evaluator the same question. Include selector-relevant properties in listing decision cache keys.

The identity provider remains responsible for establishing trust in the email. The current OIDC provider validates issuer and audience, requires the UserInfo subject to match the token subject, and rejects `email_verified != true` before exposing email. Domain comparison must be exact: `valon.com` matches, while `valon.com.evil` does not. Missing or untrusted identity metadata fails closed.

Login admission and authorization remain separate. Valon Tools currently admits both `valon.com` and `example.com`; only an exact `valon.com` selector derives `valon-employees`.

## IndexedDB evaluator

In `gestalt-providers`:

- Update [`authorization/indexeddb/relationships.go`](https://github.com/valon-technologies/gestalt-providers/blob/main/authorization/indexeddb/relationships.go) and `storage_json.go` to normalize, hash, persist, list, and filter selector targets.
- Update [`authorization/indexeddb/access.go`](https://github.com/valon-technologies/gestalt-providers/blob/main/authorization/indexeddb/access.go) so selectors participate in direct and nested subject-set traversal using exact property equality.
- Add tests for matching and non-matching domains, missing attributes, case normalization, lookalike domains, nested groups, explicit membership fallback, cycles, and single/batched decision parity.

## Toolshed rollout

1. Merge and release the Gestalt protocol, configuration, bootstrap, and trusted-property changes.
2. Publish an authorization-provider snapshot built against the updated SDK.
3. In Toolshed, pin the compatible authorization provider, regenerate `valon-tools/deploy/gestalt.lock.json`, and advance `GESTALTD_PINNED_SHA`.
4. Add `valon-employees.verifiedEmailDomains: [valon.com]` to `valon-tools/deploy/prod/config.yaml`, retaining the explicit employee roster for the first deployment.
5. Verify that a bearer-authenticated `@valon.com` user is allowed and an authenticated `@example.com` user is denied. Canary the derived path by removing one explicit employee membership and confirming Workplace Hub remains accessible.
6. In a follow-up Toolshed change, remove the explicit `@valon.com` membership rows and stale roster reconciliation comments, retaining only genuine manual exceptions. Update onboarding documentation so routine new-hire membership no longer requires on-call intervention.

## Security boundary

This defines `valon-employees` operationally as “a bearer-authenticated identity controlling a verified `@valon.com` mailbox,” not as an HR assertion of active employment.

The change removes roster drift, but it does not revoke already-issued long-lived tokens when an employee leaves. If active-employment semantics are required, the selector mechanism should consume an IdP- or HR-managed employee-group claim instead. Token and session revocation remain a separate identity-lifecycle project.

## Completion criteria

- A newly authenticated `@valon.com` user receives group-derived access without a config edit, restart, or deployment.
- External authenticated domains do not receive `valon-employees` access.
- Explicit membership continues to work.
- Invocation, listing, mounted UI, MCP, and admin decisions remain consistent.
- Missing, malformed, or untrusted identity properties deny derived membership.
- Production can remove the static employee roster after a successful canary.
