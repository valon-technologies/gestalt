# Dynamic Domain Group Membership

## Decision

Add verified-domain group membership:

```yaml
authorization:
  groups:
    valon-employees:
      verifiedEmailDomains:
        - valon.com
```

Bootstrap compiles this into a static selector:

```text
group:valon-employees#member <- subject where identity.email_domain == valon.com
```

The authorization provider evaluates the selector per request. Explicit memberships remain valid. Bootstrap does not enumerate users because that would miss users created after deployment.

## Implementation plan

### PR 1 — Gestalt design

Repository: `gestalt`; base: `main`.

Land this plan. No runtime changes.

### PR 2 — Gestalt selector contract and configuration

Repository: `gestalt`; base: `main`; depends on PR 1 approval.

- Add `SubjectSelector` to [`authorization.proto`](../../sdk/proto/v1/authorization.proto) and regenerate SDKs.
- Add and validate `authorization.groups.<id>.verifiedEmailDomains` in [`config.go`](../../gestaltd/internal/config/config.go). Normalize exact domains; reject wildcards and malformed values.
- Compile domains into static `group#member` selectors in [`authorization_bootstrap.go`](../../gestaltd/internal/bootstrap/authorization_bootstrap.go). Preserve runtime relationships and deterministic digests.
- Test serialization, validation, multiple domains, explicit membership, and state preservation.

Do not pin this build until PRs 3 and 4 land.

### PR 3 — Gestalt trusted authorization attributes

Repository: `gestalt`; stack on PR 2, then retarget `main`.

- Add reserved `identity.email_domain` authorization metadata for bearer-authenticated users enriched through identity-provider UserInfo.
- Share the property builder from [`broker.go`](../../gestaltd/services/invocation/broker.go) across invocation, listings, mounted UIs, MCP, and admin checks.
- Add selector properties to listing cache keys.
- Test browser sessions, API tokens, missing UserInfo, and single/batch parity.

Emit only a lowercase domain. Skip environment identities, service accounts, and failed enrichment. OIDC already verifies the email.

### PR 4 — IndexedDB selector evaluation

Repository: `gestalt-providers`; base: `main`; depends on PR 2.

- Persist and list selector targets in `relationships.go` and `storage_json.go`.
- Evaluate selectors during subject-set traversal in `access.go`.
- Fail closed on missing properties, malformed selectors, and type mismatches.
- Test exact matching, lookalike domains, nested groups, cycles, explicit fallback, persistence, and batch parity.

Publish the provider snapshot after merge.

### PR 5 — Toolshed compatibility rollout

Repository: `toolshed`; base: `main`; depends on PRs 2–4.

- Pin the authorization provider and Gestalt SHA.
- Add `valon-employees.verifiedEmailDomains: [valon.com]` to `valon-tools/deploy/prod/config.yaml`.
- Keep all explicit memberships.
- Regenerate `gestalt.lock.json` and add deployment-readiness tests.

### PR 6 — Toolshed production canary

Repository: `toolshed`; base: `main` after PR 5 deploys.

- Remove Avinash's explicit membership.
- Verify browser, CLI, MCP, and Workplace Hub access.
- Confirm an authenticated `@example.com` probe remains outside `valon-employees`.

### PR 7 — Toolshed roster cleanup

Repository: `toolshed`; base: `main` after the canary passes.

- Remove explicit `@valon.com` rows and stale roster comments.
- Keep manual exceptions.
- Update onboarding docs and prevent roster reintroduction.

## Stacking

```text
gestalt/main
 ├── PR 1 — design
 └── PR 2 — selector contract and configuration
      └── PR 3 — trusted authorization attributes

gestalt-providers/main
 └── PR 4 — selector evaluation
      depends on Gestalt PR 2

toolshed/main
 └── PR 5 — pin compatible builds and enable selector
      └── PR 6 — remove one explicit canary membership
           └── PR 7 — remove the remaining generated roster
```

PRs 3 and 4 can proceed in parallel after PR 2 merges. PR 5 waits for both. PRs 6 and 7 are sequential production gates, not review-only stacks.

## Deployment process

1. Merge PRs 2–4 and wait for Gestalt and provider artifacts.
2. Validate both Toolshed configs and regenerate the lockfile with the candidate Gestalt SHA and provider snapshot.
3. Set `GESTALTD_PINNED_SHA`; merge and deploy PR 5.
4. Confirm artifact versions, existing employee access, exact `valon.com` matching, external denial, and cross-surface parity.
5. Deploy PR 6. Have Avinash reauthenticate and test browser, CLI, MCP, and Workplace Hub.
6. After the canary window, deploy PR 7 and monitor authorization denials.

## Rollback

- PR 6 failure: revert PR 6.
- PR 5 failure: restore the prior provider ref, lockfile, Gestalt SHA, and config.
- After PR 7: restore the roster before rolling back the runtime.

## Exit gate

`valon-employees` means a bearer-authenticated identity with a verified `@valon.com` mailbox, not confirmed active employment. Missing identity metadata denies membership. Token revocation after offboarding is out of scope.

Complete when new `@valon.com` users receive access without config changes, external domains remain denied, all surfaces agree, and the static roster is removed.
