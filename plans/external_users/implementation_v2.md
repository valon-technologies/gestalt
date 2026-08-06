# External Users — Implementation v2

## Goal

Allow verified users from approved external domains to log in without changing employee access. Ship authorization, employee-only Auth0, and external access as separate releases.

## What failed

1. **Incomplete employee migration.** The roster was derived from subjects already present in production authorization relationships instead of reconciling an employee authority against the Gestalt `users` table. Its validator checked that same incomplete source. The deployed roster contained 67 UUIDs and six email-form subjects.
2. **Inconsistent authorization.** Operation `CheckAccess` supported subject sets, but mounted UI, catalog, and app-admin paths did not share that evaluator. [#3061](https://github.com/valon-technologies/gestalt/pull/3061) introduced direct-only catalog checks. [#3062](https://github.com/valon-technologies/gestalt/pull/3062) added subject-set traversal to the mounted-role helper, which the catalog reused, but app-admin checks remained direct-only.
3. **Bad policy migration.** Setting the shared app wildcard to `[nobody]` denied 713 operations that previously relied on wildcard viewer access. [Toolshed #4062](https://github.com/valon-technologies/toolshed/pull/4062) restored `[viewer, user, editor, admin]`.
4. **Untested Auth0 configuration.** The cutover exposed an issuer mismatch and missing connection/HRD setup, followed by HS256 tokens. Tenant changes were mutable and partly out of band.
5. **Unsafe deployment state.** No-traffic Cloud Run candidates could overwrite shared authorization state during startup. Traffic rollback did not restore that state.
6. **Non-reproducible binaries.** `GESTALTD_PINNED_SHA` was mutable and outside the Toolshed commit.
7. **Oversized hotfix.** [#4071](https://github.com/valon-technologies/toolshed/pull/4071) added 5,762 direct grants and failed startup probes. [#4073](https://github.com/valon-technologies/toolshed/pull/4073) restored the prior repository tree and deployed successfully; its employee UI/CLI verification was not recorded.

The current production users-table comparison finds 358 users, of whom 64 match the 67 roster UUIDs; 294 are absent and three roster UUIDs are orphaned. All six email-form entries duplicate UUID-backed users. These counts prove that the inventory process was incomplete, not that all 294 records were active employees.

The current users table maps Dave and Kevon to canonical UUIDs absent from the historical roster. That is the leading explanation for their post-[#4072](https://github.com/valon-technologies/toolshed/pull/4072) authorization failures: subject-set traversal cannot grant access to a UUID outside the set, and a focused regression test reproduces that behavior. The incident did not preserve a versioned users-table snapshot and request-subject report, so the person-specific conclusion is not treated as proven.

## Non-negotiables

- Keep Google login until explicit employee authorization passes and soaks.
- Build employee eligibility from an approved People/Identity authority, joined to canonical Gestalt users; never infer employment from authorization rows or email domain alone.
- Reconcile every users-table record to either an active employee UUID or an explicitly reviewed exclusion.
- Store the restricted inventory outside source control; commit only its digest, counts, and reviewed exception identifiers.
- Recheck inventory immediately before activation and provision future employee membership before first login.
- Never use `[nobody]` as the shared app wildcard.
- Do not replace group access with generated per-user/per-app grants.
- A no-traffic candidate must not mutate active authorization state.
- Pin runtime, providers, config, static authorization, Auth0 tenant state, and GSM secret version IDs; audit runtime grant changes.
- Roll back on the first failed gate; do not stack hotfixes.
- Keep federated logout out of this rollout.

## Release controls

- Allow one production deployment at a time with workflow concurrency and a protected-environment approval.
- Resolve all high- and medium-severity review findings on the final SHA, then wait for the agreed review-settle period.
- Define maximum relationship-count growth, authorization-state diff size, startup duration, and readiness duration before deployment.
- A failed gate freezes merges and queued deployments. A named incident commander must approve resumption.

## Verifiers

- **Michael:** temporary group-only canary after the no-default snapshot is active.
- **Dave:** fresh browser and CLI session.
- **Kevon:** fresh browser and MCP session.
- **Named employee app admin:** management and user-lookup paths.
- **External probe:** verified, pre-created database user; public signup disabled.

Required employee checks:

- `/apps` includes expected integrations.
- Meal Swap, AI Spend, and Build load.
- Fresh CLI session can run read-only Linear and Slack operations.
- MCP lists and successfully calls read-only Linear and Slack operations.
- Employee app-admin management and authorized lookup paths work.

Required negative checks:

- An ungranted identity cannot see or invoke internal apps.
- An external probe sees home only before a grant.
- A grant exposes only the selected app role and the actions that role permits.
- An external app admin cannot enumerate users or manage another app.
- Revoke removes access promptly.

## Stack A — Gestalt correctness

### G1 — Deployment-safe authorization state

- Remove implicit active-state writes from normal server startup before any production candidate is started.
- Add explicit authorization `plan`, `apply`, and `rollback`.
- Record model digest, relationship digest, previous snapshot, and exact rollback command.
- Make activation atomic and prevent candidate, old-revision, and concurrent runtime-grant races.
- Store the gestaltd SHA and resolved image digest in the deployment bundle, not a mutable repository variable.
- Reject authorization plans that exceed the predeclared count, diff, startup, or readiness budgets.

### G2 — Canonical identity

- Resolve verified `user:<email>` to persisted `user:<uuid>` before every authorization boundary.
- Fail closed when user-store resolution fails.
- Reject or explicitly map opaque subjects such as `user:auth0|...`.
- Prove Google and Auth0 tokens for the same verified email resolve to the same existing UUID across browser, CLI, API token, and MCP.

### G3 — Shared effective-access contract

- Keep provider `CheckAccess` authoritative for operation actions.
- Replace direct-only UI, catalog, and admin scans with one effective-role contract that supports subject sets.
- Centralize app key, resource type/ID, and `authorizationPolicy` mapping.
- Cover mounted UI, catalog, management paths, app admin, lookup, operation invocation, and MCP tool listing/calls.
- Restrict user lookup to an explicit employee operator role; app-scoped administration alone must not permit user enumeration.
- Bound any relationship traversal by pages, depth, and query count; fail closed on malformed sets.

### G4 — Conformance suite

Use the real authorization evaluator for at least one integration suite. Cover:

- direct, one-level, nested, cyclic, malformed, and paginated grants;
- group-derived admin;
- custom policy aliases and dedicated resource types;
- employee, elevated admin, ungranted external, granted external, and revoked external;
- external app-admin isolation and user-lookup denial;
- browser, CLI exchange, API token, MCP `tools/list`, and MCP calls.

### Gate A

1. Complete G1 before starting any candidate connected to production authorization storage.
2. Pin the exact G4 image digest in source-controlled deployment input.
3. Deploy under Google with current default-role behavior unchanged.
4. Run the current-access regression matrix for all employee verifiers.
5. Run Michael group-only in a production-shaped isolated environment with `defaultRole` removed.
6. Stop and restore the prior immutable bundle on any failure.

## Stack B — Employee authorization under Google

### T1 — Inventory and roster validation

- Resolve every app’s key, resource type/ID, policy alias, valid relations, mounted roles, management path, and registered operations.
- Explicitly cover Talent Team, Rippling, Traffic Cop, agent-trace-viewer, and other dedicated policies.
- Export a restricted, point-in-time users-table inventory containing canonical UUID and normalized email.
- Join the approved People/Identity employee source to that inventory.
- Classify every unmatched record as a reviewed exclusion. Require an owner and reason for non-Valon, test, duplicate, stale, and service accounts.
- Verify every employee entry is a unique, canonical Gestalt UUID and every roster UUID resolves to exactly one users-table record.
- Generate the employee membership file from the reviewed inventory, never from existing authorization relationships.
- Produce a set-difference report and require zero unclassified users, zero missing employees, zero duplicate UUIDs, and zero orphaned roster UUIDs.
- Commit the inventory digest and reconciliation summary, not raw email/UUID pairs.
- Document lookup, classification, onboarding, removal, and emergency correction. Employee onboarding must add membership before first Gestalt login.

### T2 — Preseed, shadow, activate

1. Add compact employee memberships and subject-set grants while `defaultRole` remains active.
2. Capture the current effective-access matrix for every approved employee, resource, and registered action.
3. Shadow-evaluate the proposed no-default model against that baseline across provider and server authorization paths.
4. Require zero unexplained employee denials and zero unexpected control-user allows.
5. Re-export and reconcile the users table immediately before apply; stop on any digest or classification change.
6. Apply the no-default snapshot under Google.
7. Keep:

   ```yaml
   actions:
     "*":
       relations: [viewer, user, editor, admin]
   ```

### Gate B

1. Verify the users-table reconciliation, previous authorization snapshot, and rollback command.
2. Require every approved employee UUID to pass the shadow evaluation before removing `defaultRole`.
3. Require the authorization plan to remain within the declared count, diff, startup, and readiness budgets.
4. Apply T2.
5. Temporarily remove Michael’s direct grants and run all verifier and negative checks.
6. Restore Michael’s original direct grants and verify restoration.
7. Repeat checks after 30 minutes and the next business morning.
8. Soak for one business day.
9. On failure, reactivate the previous authorization snapshot and runtime bundle.

## Stack C — Employee-only Auth0

### T3 — Versioned Auth0 state and live preflight

Create a reviewed desired-state manifest and rollback export for the exact Auth0 client, enabled connections, HRD domains, callback/logout URLs, signing algorithm, database signup setting, and GSM secret version IDs. Diff live state against it, then run a real authorization-code flow. Verify:

- configured issuer exactly matches discovery;
- ID token uses RS256 and passes JWKS verification;
- audience is correct;
- ID-token and userinfo subjects match;
- email is verified and normalizes correctly;
- callbacks and logout URLs are allowlisted;
- Google Workspace connection and HRD are enabled;
- database login and public signup are disabled;
- pinned GSM versions and IAM are correct;
- `allowedDomains` is `[valon.com]`.

Do not weaken issuer or algorithm validation. Add provider tests only for a concrete gap.

### T4 — Isolated candidate

- Deploy the exact production candidate on a separate hostname.
- Preserve a controlled copy of production user UUIDs.
- Isolate authorization relationships, grants, and sessions.
- Seed the exact production authorization snapshot.
- Run all employee, identity-continuity, renewal, account-switching, and domain-rejection checks.

This proves artifact compatibility, not production routing, cookies, or shared-state behavior.

### T5 — Production cutover

- Deploy the exact immutable T4 bundle as a tagged no-traffic candidate.
- Verify its runtime digest, provider refs, config, Auth0 manifest, secret versions, readiness, and lack of authorization-state mutation.
- Change identity configuration only.
- Keep external/database login disabled.

### Gate C

1. Rehearse the complete Google, Auth0 tenant, secret-reference, and authorization-state rollback bundle in a production-shaped environment.
2. Reverify the tagged candidate and all desired/live-state digests.
3. Record each verifier’s normalized email subject and canonical UUID without logging tokens.
4. Shift atomically; never split traffic across issuers.
5. Run all employee checks within 15 minutes.
6. Roll back immediately on login failure, UUID change, empty catalog, UI denial, or CLI/MCP denial.
7. Soak for one business day.

## Stack D — External pilot

### T6 — Probe

- Enable the database connection for one pre-created verified user with public signup disabled.
- Add one temporary domain to `allowedDomains`.
- Run no-grant, app-role grant, cross-app/admin isolation, revoke, and employee-regression checks.
- Use runtime relationships for grant/revoke; do not redeploy production config per user.
- Disable the probe user and remove the temporary domain after the test.

### T7 — First partner

- Add one approved domain and a small named cohort.
- Keep public signup disabled.
- Repeat employee and external checks before expanding.

## Evidence required at every gate

- Immutable runtime image digest, provider refs, Toolshed commit, config digest, authorization digests, and users-table inventory digest.
- People/Identity-to-users-table-to-roster reconciliation counts, freshness timestamp, and reviewed exclusions.
- Auth0 desired/live-state digest, rollback export digest, and pinned GSM secret version IDs.
- Verifier UUIDs and pass/fail results.
- Denials segmented by login, catalog, UI, HTTP, CLI, MCP discovery/call, and admin.
- Authorization plan diff, apply/rollback duration, relationship count, startup duration, and budget results.
- Final reviewed SHA, resolved high/medium findings, settle-period end, deployment run, and candidate revision.
- Two approvals: Gestalt authorization owner and Toolshed/on-call owner.

Rollback immediately for any verifier failure, UUID change, employee access loss, unexpected external access, candidate readiness failure, inventory/Auth0/authorization drift, or exceeded deployment budget.
