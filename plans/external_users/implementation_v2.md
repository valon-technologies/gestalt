# External Users — Implementation v2

## Goal

Let approved external users log in to valon.tools without internal users losing access, by moving employees from an implicit `defaultRole` grant to explicit `valon-employees` group membership.

## The migration is one substitution

Everything here serves a single change:

> **`app.defaultRole: viewer` (implicit, grants everyone) → `valon-employees` group membership (explicit, grants employees).**

`defaultRole` is a blanket allow. Once an external can authenticate, `defaultRole` hands them viewer on everything, so it must be gone before the first external logs in. That one flip is the only risky step; the rest of this plan exists to make it safe and to make it reversible.

**Rollback is re-adding `defaultRole` and redeploying.** Group memberships are additive, so they stay in place through a revert and cost nothing. This is why this plan needs no snapshot/rollback subsystem: the risky delta is a single config field.

## What failed last time

1. **Incomplete employee migration.** The roster was derived from subjects already present in production authorization relationships instead of reconciling an employee authority against the Gestalt `users` table. Its validator checked that same incomplete source. The deployed roster contained 67 UUIDs and six email-form subjects; production had 358 users, of whom only 64 matched.
2. **Inconsistent authorization.** Operation `CheckAccess` supported subject sets, but mounted UI, catalog, and app-admin paths did not share that evaluator. [#3061](https://github.com/valon-technologies/gestalt/pull/3061) introduced direct-only catalog checks. [#3062](https://github.com/valon-technologies/gestalt/pull/3062) added subject-set traversal to the mounted-role helper, but app-admin checks remained direct-only. An employee *in* the group was still denied.
3. **Bad policy migration.** Setting the shared app wildcard to `[nobody]` denied 713 operations that previously relied on wildcard viewer access. [Toolshed #4062](https://github.com/valon-technologies/toolshed/pull/4062) restored `[viewer, user, editor, admin]`.
4. **Untested Auth0 configuration.** The cutover exposed an issuer mismatch and missing connection/HRD setup, followed by HS256 tokens.
5. **Unsafe deployment state.** No-traffic Cloud Run candidates could overwrite shared authorization state during startup. Traffic rollback did not restore that state.
6. **Oversized hotfix.** [#4071](https://github.com/valon-technologies/toolshed/pull/4071) added 5,762 direct grants and failed startup probes. [#4073](https://github.com/valon-technologies/toolshed/pull/4073) restored the prior tree.

Findings 1 and 2 are the causes of employee access loss. Everything else made recovery harder.

Root cause of 2, found while implementing this plan: `resolvePrincipalUserID` treated any subject without an `@` as already canonical, so a provider-opaque subject such as `auth0|abc` reached authorization unresolved and matched no relationship.

## Non-negotiables

- Keep Google login until employee authorization passes and soaks.
- Build `valon-employees` from an approved People/Identity authority (Rippling), joined to canonical Gestalt users by normalized email. **Never** infer employment from authorization rows or email domain.
- **Never** use `[nobody]` as the shared app wildcard. Keep `actions: "*": relations: [viewer, user, editor, admin]`.
- **Never** generate per-user/per-app grants. One grant per app for the group.
- A no-traffic candidate must not mutate active authorization state.
- Roll back on the first failed check; do not stack hotfixes.
- Keep federated logout out of this rollout.

## Deliberately out of scope

Cut from the earlier draft of this plan because the flip is reversible by construction:

| Cut | Why |
| --- | --- |
| Explicit authorization `plan`/`apply`/`rollback`, diff/count budgets, atomic activation | Rollback is one config field. A snapshot subsystem is insurance against an irreversible change this is not. |
| Isolated-hostname candidate, shadow evaluation, per-stack rollout manifests | Same. Verification happens with `defaultRole` still active, where mistakes are free. |
| Catalog/MCP listing authorization | Leaks app *names* to externals, not access. Required before externals log in (Phase 4), not before the employee flip. |

## Phase 1 — Make group membership work

Correctness only. `defaultRole` stays on, so **no one's access changes**.

- Route every authorization boundary through the canonical `user:<uuid>` subject; fail closed on unresolved or provider-opaque subjects.
- Replace the direct-only relationship scans in mounted UI and app admin with the shared evaluator, so group and subject-set grants are honored.
- Stop server startup from writing active authorization state unless explicitly told to, and wire exactly one revision to own that write.
- Land the conformance suite covering direct, one-level, nested, cyclic, malformed and paginated grants across mounted UI, catalog, app admin, invocation and MCP.

**Check:** current-access regression for Dave, Kevon, and an app admin. Nothing should change for anyone.

**Note:** `CheckAccess` is action-shaped and there is no effective-roles RPC, so a policy-shaped resource type that declares relations but no actions makes the evaluator deny a question it cannot represent. A bounded transition shim keeps the legacy direct-grant path working in exactly that case and logs a warning. **If that warning never fires in production, the shim is safe to delete** — and its absence is the evidence Phase 2 needs.

## Phase 2 — Build the roster, additively

Still additive. `defaultRole` stays on, so **still no access change**.

- Export active employees from Rippling. Join normalized Valon emails to canonical Gestalt user UUIDs.
- Create `valon-employees` and add memberships from that join.
- Add **one grant per app for the group**. Not per user.
- Store the employee↔user join outside source control; commit only its digest and aggregate counts.

**Gate — this is the check that would have caught the original failure:**

1. Every active Rippling employee resolves to exactly one Gestalt UUID and is in the group. Zero unmapped, zero ambiguous, zero duplicates.
2. Every Gestalt user who authenticated in the last 30 days is either in the group or an identified non-employee. **Reconcile the count explicitly** — the original roster was 67 against 358 users and nothing flagged it.
3. Recompute the join immediately before Phase 3 and stop on any employee-relevant change.

## Phase 3 — Flip

The one risky step.

- Remove `defaultRole` from the `app` resource type. Change nothing else.
- Keep the wildcard at `[viewer, user, editor, admin]`.

**Verify immediately** (within 15 minutes), with real people, not synthetic subjects:

- Dave: fresh browser and CLI session.
- Kevon: fresh browser and MCP session.
- An app admin: management and user-lookup paths.
- `/apps` lists the expected integrations; Meal Swap, AI Spend and Build load.
- A fresh CLI session runs read-only Linear and Slack operations; MCP lists and calls them.

**Rollback:** re-add `defaultRole`, redeploy. Memberships stay. Do this on the first failure rather than diagnosing live.

Repeat the checks at 30 minutes and the next business morning. Soak one business day before Phase 4.

## Phase 4 — Externals

- Land catalog and MCP listing authorization so an ungranted identity cannot enumerate internal apps or tools.
- Auth0 employee-only cutover: verify issuer matches discovery, RS256 + JWKS, correct audience, ID-token and userinfo subjects match, callbacks/logout allowlisted, Google Workspace connection and HRD enabled, database login and public signup disabled, `allowedDomains: [valon.com]`. Run a real authorization-code flow against the exact candidate before deploying. Shift traffic atomically; never split across issuers.
- Probe: one temporary domain, one pre-created verified user. Confirm no-grant sees home only, a grant exposes only the selected app, revoke removes access promptly, and employees are unaffected. Remove the temporary domain.
- First partner: one approved domain, a small named cohort, public signup stays disabled.

## PRs

| # | Repo | Phase | Content |
| --- | --- | --- | --- |
| 1 | gestalt | 1 | Canonical identity at every authorization boundary |
| 2 | gestalt | 1 | Mounted UI + app admin decide through the evaluator |
| 3 | gestalt | 1 | Startup authorization-state write gated behind explicit apply |
| 4 | gestalt | 1 | Conformance suite |
| 5 | toolshed | 1 | Wire the state-apply flag to the one owning revision; deploy Phase 1 |
| 6 | toolshed | 2 | `valon-employees` roster, memberships, per-app group grants, reconciliation gate |
| 7 | toolshed | 3 | Remove `defaultRole` |
| 8 | gestalt | 4 | Catalog + MCP listing authorization |
| 9+ | toolshed | 4 | Auth0 cutover, probe, first partner |

PRs 1–4 were implemented and passed CI on branches `g2-canonical-identity`, `g3a-unify-authorization-evaluator`, `g1-authorization-state-safety`, and `g4-authorization-conformance-suite`. Those branches are pushed; the PRs were closed pending this replan and can be reopened.

## Open question

**What do external users actually need access to?** If it is one or two specific apps rather than broad access, running them against a separate Gestalt instance avoids the employee migration entirely — no `defaultRole` removal, no roster, no flip. The whole risk here comes from externals sharing an authorization surface with employees.
