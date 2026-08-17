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

## Implementation plan

### PR 1 — Gestalt design

- Repository: `gestalt`
- Status: this plan PR
- Base: `main`

Land this design before runtime work. No behavior or deployment changes.

### PR 2 — Gestalt selector contract and configuration

- Repository: `gestalt`
- Base: `main`
- Depends on: PR 1 for design approval only

- Add `SubjectSelector` as a `RelationshipTarget` kind in [`sdk/proto/v1/authorization.proto`](../../sdk/proto/v1/authorization.proto). A selector contains a subject type and exact property constraints.
- Regenerate every SDK binding and conversion. Preserve unknown-field compatibility across the RPC boundary.
- Add `authorization.groups.<id>.verifiedEmailDomains` to [`gestaltd/internal/config/config.go`](../../gestaltd/internal/config/config.go).
- Normalize domains to lowercase exact DNS names. Reject wildcards, email addresses, URLs, empty values, and malformed labels.
- Extend strict configuration validation. Keep the current config API version for an additive field unless repository compatibility policy requires a bump; an older daemon must reject the unknown field rather than silently ignore it.
- Compile each domain in [`gestaltd/internal/bootstrap/authorization_bootstrap.go`](../../gestaltd/internal/bootstrap/authorization_bootstrap.go) into a static `group#member` selector relationship. Preserve runtime relationships and deterministic authorization-state digests.
- Test protocol round trips, multiple domains, explicit plus derived membership, malformed domains, deterministic state, and static/runtime relationship preservation.

This PR may merge independently, but it must not be pinned by Toolshed until PRs 3 and 4 are available.

### PR 3 — Gestalt trusted authorization attributes

- Repository: `gestalt`
- Base while in review: the PR 2 branch
- Final base: `main` after PR 2 merges
- Depends on: PR 2

- Add reserved `identity.email_domain` authorization metadata for bearer-authenticated users enriched through identity-provider UserInfo.
- Refactor the subject-property builder currently in [`gestaltd/services/invocation/broker.go`](../../gestaltd/services/invocation/broker.go) into one shared path.
- Use the same trusted properties for operation invocation, single and batched listings, mounted UIs, MCP, and admin HTTP checks.
- Include selector-relevant properties in listing decision cache keys.
- Derive only the lowercase domain, not the full email address. Do not emit the property for environment identities, service accounts, missing UserInfo, or failed enrichment.
- Test every authorization surface, API-token and browser-session principals, missing UserInfo, cache separation, and single/batched request parity.

The OIDC provider already validates issuer and audience, requires UserInfo `sub` to match the token subject, and rejects `email_verified != true`. No OIDC provider change is expected.

### PR 4 — IndexedDB selector evaluation

- Repository: `gestalt-providers`
- Base: `main`
- Depends on: Gestalt PR 2 merged so the provider can consume the released SDK contract

- Update `authorization/indexeddb/relationships.go` and `storage_json.go` to normalize, hash, persist, list, filter, and round-trip selector targets.
- Update `authorization/indexeddb/access.go` so selectors participate in direct and nested subject-set traversal using exact property equality.
- Fail closed for missing properties, malformed selectors, and subject-type mismatches.
- Test matching and non-matching domains, uppercase input normalization, `valon.com.evil`, nested groups, explicit membership fallback, cycles, persistence, filtering, and single/batched decision parity.
- Publish an immutable provider snapshot after merge and record its commit SHA.

### PR 5 — Toolshed compatibility rollout

- Repository: `toolshed`
- Base: `main`
- Depends on: Gestalt PRs 2 and 3 merged with CI artifacts published; provider PR 4 merged with a published snapshot

- Pin `valon-tools/deploy/config.yaml` to the PR 4 authorization-provider snapshot.
- Add `valon-employees.verifiedEmailDomains: [valon.com]` to `valon-tools/deploy/prod/config.yaml`.
- Retain every explicit `valon-employees` membership during this deployment.
- Regenerate `valon-tools/deploy/gestalt.lock.json` using the exact candidate Gestalt build.
- Add deployment-readiness coverage for the selector relationship, exact-domain behavior, retained explicit memberships, and the Workplace Hub group grant.

This is the first production-changing PR. It installs the selector without making existing employee access depend on it.

### PR 6 — Toolshed production canary

- Repository: `toolshed`
- Base: `main` after PR 5 is deployed and verified
- Depends on: successful PR 5 production checks

- Remove one explicit `valon-employees` membership for a consenting canary, initially `avi.maheshwari@valon.com`.
- Keep all other explicit memberships.
- Deploy and verify browser, CLI/API-token, MCP listing/invocation, and Workplace Hub desk booking for the canary.
- Confirm an authenticated `@example.com` probe remains outside `valon-employees`.

Revert this PR to restore the canary's explicit membership if any surface disagrees.

### PR 7 — Toolshed roster cleanup

- Repository: `toolshed`
- Base: `main` after the PR 6 observation window
- Depends on: successful canary across all authorization surfaces

- Delete the explicit `@valon.com` membership rows and stale roster reconciliation comments.
- Retain genuine manual exceptions that cannot be expressed by the domain selector.
- Update onboarding documentation so routine new-hire access no longer requires an on-call request.
- Add a regression test that rejects reintroduction of a generated `@valon.com` roster.

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

| PR | Repository | Production behavior change | Required before merge |
| --- | --- | --- | --- |
| 1 | gestalt | No | Design approval |
| 2 | gestalt | No until pinned | Protocol/config tests |
| 3 | gestalt | No until pinned | Cross-surface authorization tests |
| 4 | gestalt-providers | No until pinned | Provider conformance tests and published snapshot |
| 5 | toolshed | Installs derived membership alongside roster | Candidate artifact validation and deployment-readiness tests |
| 6 | toolshed | One user depends on derived membership | PR 5 deployed and verified |
| 7 | toolshed | All `@valon.com` users depend on derived membership | Canary observation complete |

## Deployment process

1. **Publish compatible artifacts.** Merge Gestalt PRs 2 and 3, wait for the `gestaltd-ci` artifact for the final Gestalt SHA, merge provider PR 4, and wait for its immutable snapshot.
2. **Validate the candidate locally.** Install the candidate `gestaltd` build, load the shared and production Toolshed configs, regenerate the lockfile, and run deployment-readiness tests against the new provider snapshot.
3. **Advance the Toolshed daemon pin.** Set `GESTALTD_PINNED_SHA` to the validated Gestalt SHA immediately before PR 5 CI. The repository variable controls CI and production builds but does not deploy by itself. Restore the previous SHA if PR validation fails.
4. **Deploy PR 5.** Merge PR 5 and wait for the Valon Tools deployment workflow. Confirm the runtime heartbeat reports the expected Gestalt SHA and the authorization provider reports the expected snapshot.
5. **Verify the additive deployment.**
   - Existing explicitly listed employees retain app access.
   - The static authorization state contains the `valon-employees` domain selector.
   - A verified `@valon.com` authorization request matches the selector.
   - A verified `@example.com` request and `@valon.com.evil` control do not match.
   - Invocation, app listing, mounted UI, MCP, and admin decisions agree.
6. **Deploy PR 6.** Remove the canary's explicit row, deploy, have the canary reauthenticate, and run browser, CLI, MCP, and Workplace Hub checks. Observe normal production traffic before cleanup.
7. **Deploy PR 7.** Remove the generated roster only after the canary gate passes. Repeat employee and external probes and monitor authorization denials.

## Rollback

- **Before PR 7:** revert PR 6 to restore the canary's explicit relationship. No daemon or provider rollback is required.
- **PR 5 runtime rollback:** remove the group-domain config, restore the previous authorization-provider ref and lockfile, reset `GESTALTD_PINNED_SHA`, and redeploy. The retained roster keeps employee access available throughout.
- **After PR 7:** revert the cleanup PR to restore the explicit roster before rolling back the daemon or provider.

## Security and exit gates

`valon-employees` means “a bearer-authenticated identity controlling a verified `@valon.com` mailbox,” not an HR assertion of active employment. Production currently admits both `valon.com` and `example.com`; only exact `valon.com` derives membership. Missing or untrusted identity metadata denies derived membership.

This project does not revoke already-issued tokens after offboarding. If active-employment semantics are required, a later selector should consume an IdP- or HR-managed employee-group claim.

The rollout is complete when a newly authenticated `@valon.com` user receives group-derived access without a config edit or deployment, external domains remain denied, explicit exceptions continue to work, every authorization surface agrees, and the static generated roster has been removed after a successful canary.
