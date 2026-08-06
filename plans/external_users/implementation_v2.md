# External Users — Implementation v2

## Context

The original goal was to allow verified users from explicitly allowlisted external domains to log in to `valon.tools` through Auth0 while preserving existing Valon employee access. External users were expected to receive no app, MCP, CLI, or admin access until granted an authorization relationship.

Before the rollout, Google OIDC was the production identity provider and the shared `app` authorization type had `defaultRole: viewer`. That default implicitly gave every authenticated user broad access, so widening login to external domains first required replacing implicit access with explicit employee grants.

The initial plan attempted to:

1. remove the shared default role and close authorization fallbacks;
2. represent employees through a `valon-employees` group;
3. grant that group access through subject-set relationships;
4. move login from Google OIDC to Auth0;
5. filter the app catalog and add external-user grant operations.

These changes were individually reasonable, but they crossed authorization, identity, catalog, mounted UI, CLI/MCP, Auth0, and deployment behavior in one rollout window. The replacement authorization model was not proven across every request surface before the previous default access was removed.

## Attempted rollout stack

### Toolshed

#### Preparation and authorization

- [#4044 — Add Auth0 GSM secrets](https://github.com/valon-technologies/toolshed/pull/4044)
- [#4046 — Harden app authorization](https://github.com/valon-technologies/toolshed/pull/4046)
- [#4049 — Revert initial hardening](https://github.com/valon-technologies/toolshed/pull/4049)
- [#4050 — Add `valon-employees` authorization](https://github.com/valon-technologies/toolshed/pull/4050)
- [#4054 — Bump provider snapshot](https://github.com/valon-technologies/toolshed/pull/4054)
- [#4061 — Fix Traffic Cop authorization](https://github.com/valon-technologies/toolshed/pull/4061)
- [#4062 — Restore registry-app staff access](https://github.com/valon-technologies/toolshed/pull/4062)

#### Auth0 cutover

- [#4063 — Auth0 identity cutover](https://github.com/valon-technologies/toolshed/pull/4063)
- [#4064 — Retain rollback revisions](https://github.com/valon-technologies/toolshed/pull/4064)
- [#4066 — Fix Auth0 issuer](https://github.com/valon-technologies/toolshed/pull/4066)
- [#4067 — Fix Auth0 RS256 configuration](https://github.com/valon-technologies/toolshed/pull/4067)
- [#4068 — Deploy federated logout](https://github.com/valon-technologies/toolshed/pull/4068)

#### Recovery

- [#4071 — Direct employee-grant hotfix](https://github.com/valon-technologies/toolshed/pull/4071)
- [#4072 — Revert oversized hotfix](https://github.com/valon-technologies/toolshed/pull/4072)
- [#4073 — Full overnight rollback](https://github.com/valon-technologies/toolshed/pull/4073)

### Gestalt

- [#3052 — External-users plan](https://github.com/valon-technologies/gestalt/pull/3052)
- [#3054 — User lookup for grant authoring](https://github.com/valon-technologies/gestalt/pull/3054)
- [#3056 — Allow app admins to look up users](https://github.com/valon-technologies/gestalt/pull/3056)
- [#3058 — Add federated logout](https://github.com/valon-technologies/gestalt/pull/3058)
- [#3059 — Fix remote OIDC logout](https://github.com/valon-technologies/gestalt/pull/3059)
- [#3060 — Improve denied-login UX](https://github.com/valon-technologies/gestalt/pull/3060)
- [#3061 — Filter app catalog by grants](https://github.com/valon-technologies/gestalt/pull/3061)
- [#3062 — Resolve mounted UI subject sets](https://github.com/valon-technologies/gestalt/pull/3062)
- [#3063 — Roll back overnight external-user changes](https://github.com/valon-technologies/gestalt/pull/3063)

### Gestalt Providers

- [#1241 — Document authorization grant workflow](https://github.com/valon-technologies/gestalt-providers/pull/1241)
- [#1244 — OIDC federated logout](https://github.com/valon-technologies/gestalt-providers/pull/1244)
- [#1245 — Roll back overnight external-user provider changes](https://github.com/valon-technologies/gestalt-providers/pull/1245)

## Incident summary

The incident contained separate authorization and identity failures.

### Authorization failure

The first app-access incident began before the Auth0 cutover.

- Changing `app.actions["*"]` to `[nobody]` denied valid staff operations that were not explicitly enumerated. Later investigation counted 713 affected operations.
- Removing `defaultRole` made employee access depend on `valon-employees` subject-set grants.
- The authorization provider’s operation-level `CheckAccess` and Gestalt’s mounted UI/catalog code did not use one shared evaluation path. Mounted UI and catalog code initially looked only for direct user relationships.
- [#3061](https://github.com/valon-technologies/gestalt/pull/3061) reused that direct-only helper for `/api/v1/apps`, causing group-only employees to see an empty catalog.
- [#3062](https://github.com/valon-technologies/gestalt/pull/3062) added recursive subject-set handling to the mounted UI/catalog helper, but did not unify operation invocation, app-admin checks, policy mappings, or identity canonicalization.

### Auth0 failure

The Auth0 cutover then introduced independent login failures:

- Auth0 discovery returned an issuer with a trailing slash while the deployed configuration omitted it.
- The Auth0 application issued HS256 ID tokens while Gestalt required RS256/JWKS validation.
- Google Workspace connection enablement, Home Realm Discovery, callback URLs, and logout URLs also required out-of-band configuration that had not passed a complete preflight.

Fixing one Auth0 error exposed the next because the application was tested through production traffic rather than validated as a complete candidate beforehand.

### User impact

Dave and Kevon exposed failures that Michael’s account did not because Michael retained direct or elevated grants while they relied on the replacement employee path.

Observed symptoms included:

```text
{"error":"app access denied"}
```

```text
error: failed to invoke linear.viewer: authorization denied: linear.viewer
error: failed to invoke linear.issues: authorization denied: linear.issues
```

- Meal Swap, AI Spend, Build, and other mounted applications were inaccessible.
- `/apps` returned no integrations for affected users.
- CLI login succeeded, but read operations were denied.
- MCP connected and listed tools, but third-party operations returned `operation access denied`.
- Fresh login and redeployment of #3062 did not restore all surfaces.

### Recovery failure

[#4071](https://github.com/valon-technologies/toolshed/pull/4071) attempted to bypass subject sets by materializing direct grants for every UUID employee across every app. It added roughly 41,000 configuration lines and caused authorization bootstrap to exceed its startup timeout. The candidate failed before receiving traffic and was reverted by [#4072](https://github.com/valon-technologies/toolshed/pull/4072).

The final recovery strategy was to restore Toolshed to its pre-rollout tree through [#4073](https://github.com/valon-technologies/toolshed/pull/4073), with matching Gestalt and provider rollback PRs.

## Lessons applied in v2

- Authorization, employee-only identity cutover, and external enablement are separate releases.
- Google remains active while group authorization is proven and soaked.
- The shared app wildcard remains relationship-gated for valid staff roles; it is not changed to `[nobody]`.
- Group authorization must pass mounted UI, catalog, CLI, MCP, operation, and admin tests before implicit access is removed.
- Michael is tested as a temporary group-only employee, while Dave and Kevon provide identity-diversity confirmation.
- Auth0 is first tested with `allowedDomains: [valon.com]` and external database login disabled.
- Every production change has a named rollback revision, verifier signoff, and soak period.
- A failed gate triggers rollback rather than another stacked hotfix.

## Goal

Enable external users without changing authorization, employee identity, and Auth0 configuration in one production release. Each production-changing PR has its own deploy, employee verification, soak, and rollback decision.

## Verifier personas

Michael is the primary group-only canary. Dave and Kevon, or substitutes with the same access patterns, provide final confirmation that the result is not specific to Michael’s identity.

### Canary setup — Michael as a group-only employee

1. Confirm another person retains admin and rollback access for the duration of the test.
2. Record Michael’s persisted Gestalt UUID.
3. Export Michael’s current authorization relationships and prepare tested restoration commands before removing anything.
4. Remove Michael’s direct app and custom-policy grants through the authorization API/CLI or deployed configuration.
5. Leave only Michael’s `valon-employees#member` relationship.
6. Do not edit raw IndexedDB/Cloud SQL records; bootstrap may overwrite them and malformed protobuf records can corrupt authorization state.
7. Log out and create fresh browser, CLI, and MCP sessions so cached grants or old tokens cannot mask the result.
8. Restore Michael’s original relationships immediately after the test window or on any failure.

Run this only against the closed group canary or after `defaultRole` has been removed. The existing permissive `defaultRole` would otherwise make a successful test meaningless.

### Verifier A — Michael web and CLI

- Open `/apps`; confirm Linear, Slack, Notion, BigQuery, and GitHub are present.
- Open Meal Swap, AI Spend, and Build; confirm the shell and data load without `app access denied`.
- Run with no environment token override:

  ```sh
  export GESTALT_URL=https://valon.tools
  unset GESTALT_API_KEY
  gestalt auth login
  # Invoke one read-only Linear operation.
  ```

- Failure is any empty catalog, HTTP 401/403, `app access denied`, or `authorization denied`.

### Verifier B — Michael MCP and alternate web session

- Reauthenticate the `user-gestalt` MCP server.
- Confirm tools are discoverable and execute one read-only Slack operation and one read-only Linear operation.
- Open `/apps` and `/build` in a fresh browser session.
- Failure is “tools visible but every third-party operation is denied,” an empty catalog, or an inaccessible build screen.

### Identity-diversity confirmation — Dave and Kevon

- Dave repeats the web/CLI checks with his own fresh session.
- Kevon repeats the MCP and alternate-browser checks with his own fresh session.
- Their checks are required because changing Michael’s relationships proves group authorization but does not prove that Dave’s or Kevon’s OIDC email resolves to the expected persisted UUID.
- Neither person needs to share a token, password, or integration credential; they return only pass/fail and exact error text.

### External probe

- Use a verified database user on a temporary allowlisted domain.
- Before a grant: home only; internal catalog entries hidden; direct UI and operation access denied.
- After one grant: only the selected app and read operation succeed.
- After revoke: access is denied again without delay.

## PR stack

### Stack A — Gestalt authorization correctness

#### G1 — Shared effective-access evaluator

- **Repo:** `gestalt`
- **Base:** `main`
- **Files:** `gestaltd/internal/server/mounted_ui.go`, `integration_visibility.go`, and `handlers_app_admin_registry.go`.
- Replace per-surface direct relationship enumeration with one provider-backed evaluator for direct and subject-set grants.
- Include mounted UI, catalog, management path, group-derived app admin, and lookup authorization.
- Test direct grants plus one-level, nested, cyclic, malformed, and paginated subject sets.

#### G2 — Canonical user identity at every authorization boundary

- **Repo:** `gestalt`
- **Depends on:** G1
- Resolve OIDC `user:<email>` to persisted `user:<uuid>` before UI, catalog, invocation, CLI/API-token, and MCP authorization.
- Add an end-to-end fixture where Auth0-style introspection returns an email subject but relationships target the existing UUID.
- Assert browser, fresh CLI login, and MCP all use the same UUID and effective grants.

#### G3 — Cross-surface authorization conformance suite

- **Repo:** `gestalt`
- **Depends on:** G2
- Add table-driven employee, elevated-admin, ungranted-external, directly-granted-external, and revoked-external scenarios.
- Exercise `/api/v1/apps`, mounted UI, operation invocation, app-admin registry, API token, and MCP transport.
- Retain both direct and group-grant cases.

#### Deployment gate A

1. Publish and pin the G3 binary while production remains on the rolled-back Google configuration.
2. Treat this as a behavior-preserving release.
3. Put Michael into the group-only canary state.
4. Run Verifier A and Verifier B.
5. Restore Michael’s original grants.
6. If either test fails, restore the previous binary pin and stop.

### Stack B — Toolshed authorization under Google

#### T1 — Authorization inventory and validator

- **Repo:** `toolshed`
- **Depends on:** Deployment gate A
- Add a validator that resolves each app’s actual resource ID, custom `authorizationPolicy`, resource type, supported relations, mounted `allowedRoles`, and registered operations.
- Explicitly cover dedicated policy types such as agent-trace-viewer, Talent Team aliases, Rippling relations, Traffic Cop nested operations, and app-admin paths.
- Make no production authorization behavior change.

#### T2 — Group-only canary

- **Repo:** `toolshed`
- **Depends on:** T1
- Add one closed canary policy/app with no `defaultRole`, a compact `valon-employees` subject-set grant, and representative UI/read operation.
- Keep the shared `app` model unchanged.
- Deploy under Google.
- Put Michael into the group-only canary state.
- Verifier A and Verifier B must pass both the canary and all existing surfaces.
- Restore Michael’s original grants after testing.
- Soak for one business day.

#### T3 — Production authorization migration

- **Repo:** `toolshed`
- **Depends on:** successful T2 soak
- Remove unintended shared `app.defaultRole`.
- Keep:

  ```yaml
  actions:
    "*":
      relations: [viewer, user, editor, admin]
  ```

- Do not use `[nobody]` for the shared catch-all.
- Add compact `valon-employees` memberships and subject-set grants.
- Do not generate direct per-user/app rows.
- Migrate custom policies only when T1 proves the exact resource/relation mapping.

#### Deployment gate B

1. Deploy T3 with Google still active.
2. Put Michael into the group-only canary state and run Verifier A and Verifier B immediately.
3. Have Dave and Kevon run the identity-diversity confirmation.
4. Restore Michael’s original grants.
5. Repeat employee verification after 30 minutes and the next business morning.
6. Confirm an ungranted test identity is denied.
7. Soak for one full business day with authorization-denial monitoring.
8. Roll back T3 on any staff regression; do not patch individual apps during the gate.

### Stack C — Auth0 readiness without production cutover

#### T4 — Auth0 setup and preflight tooling

- **Repo:** `toolshed`
- **Cutover dependency:** Deployment gate B
- Make setup idempotent and validate:
  - exact issuer with trailing slash;
  - RS256 ID tokens and JWKS verification;
  - callback and logout allowlists;
  - Google Workspace connection and Home Realm Discovery;
  - database connection disabled for the employee-only stage;
  - GSM secret values and IAM;
  - verified email and `allowedDomains: [valon.com]`.
- Fail preflight before merge or deploy if any check fails.

#### P1 — OIDC/logout contract tests, if needed

- **Repo:** `gestalt-providers`
- **Depends on:** a concrete provider gap found by T4
- Do not make speculative provider changes.
- Cover issuer normalization, Auth0 logout capability, remote-provider config propagation, and non-Auth0 behavior.
- Publish a snapshot only after provider integration tests pass.

#### T5 — Isolated employee-only Auth0 deployment

- **Repo:** `toolshed`
- **Depends on:** T4, optional P1, and Deployment gate B
- Deploy on a separate hostname with isolated authorization storage.
- Use `allowedDomains: [valon.com]`; keep database/external login disabled.
- Record Michael’s, Dave’s, and Kevon’s `/api/v1/auth/session` UUIDs before and after Auth0 login; each must match.
- Put Michael into the group-only canary state and run the complete Verifier A and B matrices, logout/account switching, token renewal, and a non-Valon-domain rejection.
- Have Dave and Kevon perform identity-diversity confirmation before restoring Michael’s grants.

### Stack D — Production identity and external pilot

#### T6 — Production employee-only Auth0 cutover

- **Repo:** `toolshed`
- **Depends on:** successful T5 verification and two human approvals
- Change only identity configuration and the lockfile/pin required by the tested candidate.
- Keep external database login disabled and `allowedDomains: [valon.com]`.

#### Deployment gate C

1. Retain a named, verified Google revision and exact rollback command.
2. Do not split traffic between revisions that validate different issuers.
3. Shift atomically in an announced window.
4. Put Michael into the group-only canary state and run Verifier A and Verifier B.
5. Dave, Kevon, and one separate app admin sign off within 15 minutes.
6. Restore Michael’s original grants.
7. Roll back immediately for login failure, UUID change, empty catalog, UI 403, or CLI/MCP denial.
8. Soak for one business day.

#### T7 — External probe and grant/revoke runbook

- **Repo:** `toolshed`
- **Depends on:** Deployment gate C
- Enable only the temporary probe domain and verified database probe user.
- Run the external probe negative, grant, and revoke sequence.
- Document lookup, grant, revoke, domain onboarding, Auth0 block, and rollback commands.
- Remove the temporary domain after testing.

#### T8 — First partner domain

- **Repo:** `toolshed`
- **Depends on:** successful T7 soak
- Add one partner domain and a small named pilot only.
- Do not enable open database signup.
- Repeat employee and external matrices before expanding.

## Stack diagram

```text
gestalt/main
  └── G1 shared authorization evaluator
       └── G2 canonical authorization subject
            └── G3 cross-surface conformance tests
                 └── Deployment gate A

toolshed/main
  └── T1 authorization validator
       └── T2 group-only canary
            └── T2 deploy + one-day soak
                 └── T3 production authorization under Google
                      └── Deployment gate B + one-day soak

toolshed/main
  └── T4 Auth0 preflight tooling
       ├── P1 provider fix, only if required
       └── T5 isolated employee-only Auth0
            └── T6 production employee-only Auth0
                 └── Deployment gate C + one-day soak
                      └── T7 external probe
                           └── T8 first partner
```

## Merge and deployment process

1. Every PR needs two human approvals: one Gestalt authorization owner and one Toolshed/on-call owner.
2. All high-severity automated findings must be resolved or explicitly accepted by both reviewers before merge.
3. PR test-plan boxes must contain linked evidence, not planned future checks.
4. Merge one production-changing PR at a time.
5. Do not merge the next stack layer until the prior deployment gate and soak are complete.
6. Announce the verifier window before deploy and identify the rollback owner.
7. Capture revision, binary/provider pins, effective config digest, verifier results, and dashboard links in the deployment record.
8. Capture Michael’s relationship export and restoration confirmation in the deployment record without exposing tokens.
9. On a gate failure, restore Michael’s grants, execute rollback, and diagnose on the rolled-back state; do not stack emergency authorization or identity changes.

## Required monitoring

- Login start/completion failures by provider.
- Raw-subject form versus canonical UUID outcome, without logging tokens or sensitive claims.
- Authorization denials by UI, catalog, HTTP invocation, CLI, and MCP.
- Empty-catalog response count.
- Subject-set evaluation errors and latency.
- Cloud Run candidate startup/readiness and authorization bootstrap duration.

## Rollback triggers

Rollback immediately if any of the following occurs:

- a verifier cannot log in;
- an existing user’s UUID changes;
- an employee sees an empty or materially reduced catalog;
- Meal Swap, AI Spend, Build, or another verifier surface returns 401/403;
- CLI or MCP returns `authorization denied` for an expected read operation;
- an ungranted external can see or invoke an internal app;
- authorization denials or login failures cross the agreed alert threshold;
- the candidate fails readiness or authorization bootstrap approaches its timeout.
