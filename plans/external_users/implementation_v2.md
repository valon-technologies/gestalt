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
- Block only on active employees without one canonical user, ambiguous internal identities, and roster entries not backed by active employment.
- Store the restricted employee-to-user join outside source control; commit only its digest and aggregate reconciliation results.
- Recompute that join immediately before activation and provision future employee membership before first login.
- Never use `[nobody]` as the shared app wildcard.
- Do not replace group access with generated per-user/per-app grants.
- A no-traffic candidate must not mutate active authorization state.
- Pin runtime, providers, config, static authorization, Auth0 tenant state, and GSM secret version IDs; audit runtime grant changes.
- Roll back on the first failed gate; do not stack hotfixes.
- Keep federated logout out of this rollout.

## Rollout manifest

Before each stack, commit one machine-readable manifest with:

- final reviewed SHA, immutable input digests, secret version IDs, and previous-state references;
- named owner, incident commander, approvers, and protected production environment;
- literal plan/apply/verify/rollback commands and expected output artifacts;
- numeric relationship-count, authorization-diff, startup, readiness, and denial thresholds;
- verifier IDs, checks, settle-period end, and explicit success criteria.

CI rejects missing values and unresolved high/medium findings. The deployment workflow consumes the manifest, allows one production run at a time, and cancels queued runs after failure. Resumption requires an incident-commander approval recorded in a new manifest revision.

## Verifiers

- **Dedicated employee canary:** canonical UUID with group-only access and no direct grants.
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

### G3 — One authorization decision engine

- Put direct grants, subject sets, default roles, policy aliases, and action precedence in one provider-owned evaluator.
- Make `CheckAccess`, batch checks, and effective-role queries thin views over that evaluator; no server path may traverse relationships independently.
- Implement catalog and MCP listing as batched decisions from the same evaluator used for invocation.
- Centralize app key, resource type/ID, and `authorizationPolicy` mapping.
- Cover mounted UI, catalog, management paths, app admin, lookup, operation invocation, and MCP tool listing/calls.
- Restrict user lookup to an explicit employee operator role; app-scoped administration alone must not permit user enumeration.
- Bound evaluator traversal by depth and work count; fail closed on malformed sets.

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
2. Validate the Stack A rollout manifest and pin the exact G4 image digest.
3. Deploy under Google with current default-role behavior unchanged.
4. Run the current-access regression matrix for all employee verifiers.
5. Run the dedicated group-only canary in a production-shaped isolated environment with `defaultRole` removed.
6. Execute the manifest rollback command on any failure.

## Stack B — Employee authorization under Google

### T1 — Inventory and roster validation

- Resolve every app’s key, resource type/ID, policy alias, valid relations, mounted roles, management path, and registered operations.
- Explicitly cover Talent Team, Rippling, Traffic Cop, agent-trace-viewer, and other dedicated policies.
- Export the approved active-employee authority and join normalized employee emails to canonical Gestalt users.
- Verify every employee entry is a unique, canonical Gestalt UUID and every roster UUID resolves to exactly one users-table record.
- Generate membership from that join, never from existing authorization relationships.
- Require zero unmapped employees, zero ambiguous internal identities, zero duplicate UUIDs, and zero roster entries absent from the employee authority.
- Commit the source and join digests plus aggregate results, not raw email/UUID pairs. Unrelated external and stale users require no classification.
- Document lookup, classification, onboarding, removal, and emergency correction. Employee onboarding must add membership before first Gestalt login.

### T2 — Preseed, shadow, activate

1. Add compact employee memberships and subject-set grants while `defaultRole` remains active.
2. Capture the current effective-access matrix for every approved employee, resource, and registered action.
3. Shadow-evaluate the proposed no-default model against that baseline through the canonical evaluator and each server surface.
4. Require zero unexplained employee denials and zero unexpected control-user allows.
5. Recompute the active-employee-to-user join immediately before apply; stop on any employee-relevant change.
6. Apply the no-default snapshot under Google.
7. Keep:

   ```yaml
   actions:
     "*":
       relations: [viewer, user, editor, admin]
   ```

### Gate B

1. Validate the Stack B rollout manifest, employee join, previous authorization snapshot, and rollback command.
2. Require every approved employee UUID to pass the shadow evaluation before removing `defaultRole`.
3. Require the authorization plan to remain within the declared count, diff, startup, and readiness budgets.
4. Apply T2.
5. Run all verifier and negative checks, including the permanently group-only canary.
6. Repeat checks after 30 minutes and the next business morning.
7. Soak for one business day.
8. On failure, execute the manifest rollback command to reactivate the previous authorization snapshot and runtime bundle atomically.

## Stack C — Employee-only Auth0

### T3 — Versioned Auth0 state and live preflight

Provision a dedicated Auth0 client and its connections additively before building the candidate. Record the exact client, enabled connections, HRD domains, callback/logout URLs, signing algorithm, database signup setting, and GSM secret version IDs in the Stack C rollout manifest. Diff live state against it, then run a real authorization-code flow. Verify:

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

Freeze this Auth0 client and its secret references after preflight. Later steps may only route traffic between the immutable Google and Auth0 revisions. Do not weaken issuer or algorithm validation.

### T4 — Isolated candidate

- Build the production candidate already bound to the frozen Auth0 client, config, and secret versions; deploy it on a separate hostname.
- Preserve a controlled copy of production user UUIDs.
- Isolate authorization relationships, grants, and sessions.
- Seed the exact production authorization snapshot.
- Run all employee, identity-continuity, renewal, account-switching, and domain-rejection checks.

This proves artifact compatibility, not production routing, cookies, or shared-state behavior.

### T5 — Production candidate

- Deploy the exact T4 image and frozen configuration as a tagged no-traffic production revision.
- Verify its runtime digest, provider refs, config, Auth0 state, secret versions, readiness, and lack of authorization-state mutation.
- Keep external/database login disabled.
- Do not mutate Auth0, GSM references, authorization, or runtime configuration after verification.

### Gate C

1. Validate the Stack C rollout manifest and rehearse its single-step traffic rollback to the immutable Google revision.
2. Reverify the tagged candidate and all frozen-state digests.
3. Record each verifier’s normalized email subject and canonical UUID without logging tokens.
4. Cut over with one traffic-routing update to the verified Auth0 revision; never split traffic across issuers.
5. Run all employee checks within 15 minutes.
6. Reverse only that routing update on login failure, UUID change, empty catalog, UI denial, or CLI/MCP denial.
7. Soak for one business day.

## Stack D — External pilot

### T6 — Immutable probe candidate

- Provision a separate pilot Auth0 client additively with Google Workspace, a database connection, public signup disabled, one pre-created verified user, and one temporary allowed domain.
- Freeze that client and its secret versions, then build and deploy a tagged no-traffic revision bound to them.
- Verify employee login, probe login, no-grant behavior, and frozen-state digests on the candidate hostname.
- Do not mutate the active employee-only Auth0 client or revision.

### Gate D

1. Validate the Stack D rollout manifest and rehearse one-step traffic rollback to the employee-only Auth0 revision.
2. Route traffic to the verified probe revision.
3. Run app-role grant, cross-app/admin isolation, revoke, and employee-regression checks. Use runtime relationships for grant/revoke.
4. Route back to the employee-only revision after the probe, then disable the unused pilot client.

### T7 — First partner

- Build a new frozen client/configuration and runtime revision for one approved domain and a small pre-created cohort; keep public signup disabled.
- Add later users from that domain by pre-creating identities and runtime grants; this does not require a new client.
- Require a new frozen client and runtime revision only when domains or authentication policy change.

### Gate E

1. Validate the partner rollout manifest and rehearse traffic rollback to the employee-only or previous partner revision.
2. Route traffic to the verified partner revision.
3. Run employee, no-grant, grant, isolation, revoke, renewal, and account-switching checks.
4. Roll back only on failure; retain the partner revision after the defined soak succeeds.

## Gate outputs

- **Gate A:** Gestalt image digest; conformance report; server-surface report; startup/readiness results; rollback rehearsal.
- **Gate B:** employee-source and join digests; aggregate reconciliation; baseline/shadow diff; authorization plan and snapshot digests; canary/verifier results.
- **Gate C:** frozen Auth0-state and GSM-version digests; Google/Auth0 revision IDs; identity-continuity results; traffic rollback rehearsal.
- **Gate D:** frozen pilot Auth0/GSM/runtime digests; named cohort; domain and connection state; revision IDs; traffic rollback rehearsal; exact runtime grant diff; no-grant/grant/isolation/revoke results; employee regression.
- **Gate E:** frozen partner Auth0/GSM/runtime digests; named cohort and domain; previous/partner revision IDs; rollback rehearsal; full verifier results; soak result.

Every output is produced by a literal manifest command and checked against that stack's numeric thresholds. Gate A does not require later-stack inventory or Auth0 evidence.

Rollback immediately for any verifier failure, UUID change, employee access loss, unexpected external access, candidate readiness failure, inventory/Auth0/authorization drift, or exceeded deployment budget.
