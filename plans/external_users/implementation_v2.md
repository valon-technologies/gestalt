# External Users — Implementation v2

## Goal

Allow verified users from approved external domains to log in without changing employee access. Ship authorization, employee-only Auth0, and external access as separate releases.

## What failed

1. **Inconsistent authorization.** Operation `CheckAccess` supported subject sets, but mounted UI, catalog, and app-admin paths did not share that evaluator. [#3061](https://github.com/valon-technologies/gestalt/pull/3061) broke group-only catalog access. [#3062](https://github.com/valon-technologies/gestalt/pull/3062) added subject-set traversal for mounted UI and catalog, but app-admin checks remained direct-only.
2. **Bad policy migration.** Setting the shared app wildcard to `[nobody]` denied 713 operations that previously relied on wildcard viewer access. [Toolshed #4062](https://github.com/valon-technologies/toolshed/pull/4062) restored `[viewer, user, editor, admin]`.
3. **Untested Auth0 configuration.** The cutover exposed, in sequence, an issuer mismatch, HS256 tokens, and missing connection/HRD setup.
4. **Unsafe deployment state.** No-traffic Cloud Run candidates could overwrite shared authorization state during startup. Traffic rollback did not restore that state.
5. **Non-reproducible binaries.** `GESTALTD_PINNED_SHA` was mutable and outside the Toolshed commit.
6. **Oversized hotfix.** [#4071](https://github.com/valon-technologies/toolshed/pull/4071) generated 5,762 direct grants and failed startup probes. [#4073](https://github.com/valon-technologies/toolshed/pull/4073) restored service.

The 67-user roster intentionally covered previously logged-in users and included Dave and Kevon. Roster completeness was not the cause.

The `[nobody]` policy explains earlier operation denials, and a focused regression test confirms that #3062 resolves a canonical UUID’s group-derived mounted-UI access. Neither explains Dave’s UI, catalog, and CLI failures after [Toolshed #4072](https://github.com/valon-technologies/toolshed/pull/4072) deployed #3062 with the restored wildcard. The exact post-#4072 failure remains unproven; stale or unexpected authorization state, artifact mismatch, and an uncovered production path remain hypotheses.

## Non-negotiables

- Keep Google login until explicit employee authorization passes and soaks.
- Keep the reviewed roster of previously logged-in users as canonical Gestalt UUIDs.
- Add future users through an explicit, validated manual process.
- Never use `[nobody]` as the shared app wildcard.
- Do not replace group access with generated per-user/per-app grants.
- A no-traffic candidate must not mutate active authorization state.
- Pin runtime, providers, config, and the static authorization baseline immutably; audit runtime grant changes.
- Roll back on the first failed gate; do not stack hotfixes.
- Keep federated logout out of this rollout.

## Verifiers

- **Michael:** temporary group-only canary after the no-default snapshot is active.
- **Dave:** fresh browser and CLI session.
- **Kevon:** fresh browser and MCP session.
- **App admin:** management and user-lookup paths.
- **External probe:** verified, pre-created database user; public signup disabled.

Required employee checks:

- `/apps` includes expected integrations.
- Meal Swap, AI Spend, and Build load.
- Fresh CLI session can run read-only Linear and Slack operations.
- MCP lists and successfully calls read-only Linear and Slack operations.
- App-admin management and lookup paths work.

Required negative checks:

- An ungranted identity cannot see or invoke internal apps.
- An external probe sees home only before a grant.
- Grant exposes only the selected app/action.
- Revoke removes access promptly.

## Stack A — Gestalt correctness

### G1 — Canonical identity

- Resolve verified `user:<email>` to persisted `user:<uuid>` before every authorization boundary.
- Fail closed when user-store resolution fails.
- Reject or explicitly map opaque subjects such as `user:auth0|...`.
- Prove Google and Auth0 tokens for the same verified email resolve to the same existing UUID across browser, CLI, API token, and MCP.

### G2 — Shared effective-access contract

- Keep provider `CheckAccess` authoritative for operation actions.
- Replace direct-only UI, catalog, and admin scans with one effective-role contract that supports subject sets.
- Centralize app key, resource type/ID, and `authorizationPolicy` mapping.
- Cover mounted UI, catalog, management paths, app admin, lookup, operation invocation, and MCP tool listing/calls.
- Bound any relationship traversal by pages, depth, and query count; fail closed on malformed sets.

### G3 — Conformance suite

Use the real authorization evaluator for at least one integration suite. Cover:

- direct, one-level, nested, cyclic, malformed, and paginated grants;
- group-derived admin;
- custom policy aliases and dedicated resource types;
- employee, elevated admin, ungranted external, granted external, and revoked external;
- browser, CLI exchange, API token, MCP `tools/list`, and MCP calls.

### Gate A

1. Pin the exact G3 image digest in source-controlled deployment input.
2. Deploy under Google with current default-role behavior unchanged.
3. Run the current-access regression matrix for all employee verifiers.
4. Run Michael group-only in a production-shaped isolated environment with `defaultRole` removed.
5. Stop and restore the prior immutable bundle on any failure.

## Stack B — Employee authorization under Google

### T1 — Inventory and roster validation

- Resolve every app’s key, resource type/ID, policy alias, valid relations, mounted roles, management path, and registered operations.
- Explicitly cover Talent Team, Rippling, Traffic Cop, agent-trace-viewer, and other dedicated policies.
- Verify every reviewed roster entry is a unique, canonical Gestalt UUID.
- Document the manual lookup, validation, addition, and removal workflow.
- Block on missing users, duplicates, or noncanonical subjects.

### T2 — Versioned authorization state

- Remove implicit active-state writes from normal server startup.
- Add explicit authorization `plan`, `apply`, and `rollback`.
- Record model digest, relationship digest, previous snapshot, and exact rollback command.
- Make activation atomic and prevent candidate/old-revision races.
- Store the gestaltd SHA and resolved image digest in the deployment bundle, not a mutable repository variable.

### T3 — Preseed, shadow, activate

1. Add compact employee memberships and subject-set grants while `defaultRole` remains active.
2. Shadow-evaluate the proposed no-default model for every employee and inventoried access path.
3. Require zero unexpected employee denials and zero unexpected control-user allows.
4. Apply the no-default snapshot under Google.
5. Keep:

   ```yaml
   actions:
     "*":
       relations: [viewer, user, editor, admin]
   ```

### Gate B

1. Verify the previous authorization snapshot and rollback command.
2. Apply T3.
3. Temporarily remove Michael’s direct grants and run all verifier and negative checks.
4. Restore Michael’s original direct grants and verify restoration.
5. Repeat checks after 30 minutes and the next business morning.
6. Soak for one business day.
7. On failure, reactivate the previous authorization snapshot and runtime bundle.

## Stack C — Employee-only Auth0

### T4 — Live preflight

Run a real authorization-code flow against the exact candidate application and artifacts. Verify:

- configured issuer exactly matches discovery;
- ID token uses RS256 and passes JWKS verification;
- audience is correct;
- ID-token and userinfo subjects match;
- email is verified and normalizes correctly;
- callbacks and logout URLs are allowlisted;
- Google Workspace connection and HRD are enabled;
- database login and public signup are disabled;
- GSM values and IAM are correct;
- `allowedDomains` is `[valon.com]`.

Do not weaken issuer or algorithm validation. Add provider tests only for a concrete gap.

### T5 — Isolated candidate

- Deploy the exact production candidate on a separate hostname.
- Preserve a controlled copy of production user UUIDs.
- Isolate authorization relationships, grants, and sessions.
- Seed the exact production authorization snapshot.
- Run all employee, identity-continuity, renewal, account-switching, and domain-rejection checks.

This proves artifact compatibility, not production routing, cookies, or shared-state behavior.

### T6 — Production cutover

- Deploy the exact immutable T5 bundle.
- Change identity configuration only.
- Keep external/database login disabled.
- Shift atomically; never split traffic across issuers.

### Gate C

1. Retain verified Google and authorization-state rollback commands.
2. Record each verifier’s normalized email subject and canonical UUID without logging tokens.
3. Run all employee checks within 15 minutes.
4. Roll back immediately on login failure, UUID change, empty catalog, UI denial, or CLI/MCP denial.
5. Soak for one business day.

## Stack D — External pilot

### T7 — Probe

- Enable the database connection for one pre-created verified user with public signup disabled.
- Add one temporary domain to `allowedDomains`.
- Run no-grant, grant, revoke, and employee-regression checks.
- Use runtime relationships for grant/revoke; do not redeploy production config per user.
- Disable the probe user and remove the temporary domain after the test.

### T8 — First partner

- Add one approved domain and a small named cohort.
- Keep public signup disabled.
- Repeat employee and external checks before expanding.

## Evidence required at every gate

- Immutable runtime image digest, provider refs, Toolshed commit, config digest, and authorization digests.
- Verifier UUIDs and pass/fail results.
- Denials segmented by login, catalog, UI, HTTP, CLI, MCP discovery/call, and admin.
- Authorization apply/rollback duration and relationship count.
- Two approvals: Gestalt authorization owner and Toolshed/on-call owner.

Rollback immediately for any verifier failure, UUID change, employee access loss, unexpected external access, candidate readiness failure, or authorization digest change outside explicit apply/rollback.
