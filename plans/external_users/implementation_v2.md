# External Users — Implementation v2

## Goal

Let approved external users log in to valon.tools without internal users losing access, by moving employees from an implicit `defaultRole` grant to explicit `valon-employees` group membership.

**External users get authentication only.** They sign in, reach the valon.tools home page, and get nothing else: no apps visible, no operations invocable, zero authorization grants. Per-partner access, if it is ever wanted, is a later decision driven by a real requirement.

Externals share the valon.tools instance with employees (decided; see [Decisions](#decisions)). That is what makes this migration necessary rather than optional: `defaultRole` is a blanket allow, so the moment an external can authenticate they inherit viewer on everything unless it is gone.

## The migration is one substitution

Everything here serves a single change:

> **`defaultRole` on every resource type that declares one (implicit, grants everyone) → `valon-employees` group membership (explicit, grants employees).**

It is **not** just `app`. As of `toolshed/valon-tools/deploy/config.yaml` six resource types carry a `defaultRole`:

| Resource type | `defaultRole` | Note |
| --- | --- | --- |
| `agent-trace-viewer` | `viewer` | YAML anchor `&defaultAuthzResource` |
| `workflow` | `viewer` | alias of that anchor |
| `agent` | `viewer` | alias of that anchor |
| `app` | `viewer` | |
| `pmiPayoffCancellations` | `admin` | an authenticated external would inherit **admin** |
| `itAccountOnboarding` | `noaccess` | non-empty, so still an allow — see below |

Two traps here. The anchor/alias means editing `agent-trace-viewer` silently changes `workflow` and `agent` too, and grepping for `defaultRole` finds only four of the six sites. And `noaccess` is not a deny: `authorizeMountedResourceRoles` allows whenever `defaultRole != "" && len(allowedRoles) == 0`, so any non-empty value authorizes a mount that restricts no roles. Clear the field; do not set it to a sentinel.

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
Catalog and MCP listing authorization is **not** cut. `FilterCatalogForPrincipal` is currently `return cat` — it ignores the principal — and MCP `tools/list` filters only on token scope. An authenticated external with zero grants would therefore see the entire internal app catalog and every tool, even though it could invoke none of them. "No apps visible" makes that a hard prerequisite for Phase 4, not a follow-up. It is still not required for the employee flip, so it lands between Phase 3 and externals.

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

- **Enumerate every resource type that declares a `defaultRole`**, resolving YAML anchors and aliases rather than grepping — the six in the table above are the current answer, but re-derive it from the deployed config rather than trusting this doc. This list is exactly the set of grants the group needs, and exactly the set Phase 3 clears.
- Export active employees from Rippling. Join normalized Valon emails to canonical Gestalt user UUIDs.
- Create `valon-employees` and add memberships from that join.
- Add **one grant per resource type for the group** — matching the role each `defaultRole` currently confers, so employees keep exactly what they have today. Not per user.
- Store the employee↔user join outside source control; commit only its digest and aggregate counts.

**Gate — this is the check that would have caught the original failure:**

1. Every active Rippling employee resolves to exactly one Gestalt UUID and is in the group. Zero unmapped, zero ambiguous, zero duplicates.
2. Every Gestalt user who authenticated in the last 30 days is either in the group or an identified non-employee. **Reconcile the count explicitly** — the original roster was 67 against 358 users and nothing flagged it.
3. Every resource type carrying a `defaultRole` has a corresponding group grant. A resource type cleared in Phase 3 without one silently revokes whatever that `defaultRole` was providing.
4. Recompute the join immediately before Phase 3 and stop on any employee-relevant change.

## Phase 3 — Flip

The one risky step.

- **Clear `defaultRole` from every resource type enumerated in Phase 2** — omit the field entirely; do not set `noaccess` or any other sentinel, which still reads as an allow. Remember the anchor: clearing `agent-trace-viewer` also clears `workflow` and `agent`, and that is intended, but confirm the rendered config rather than the source YAML.
- Change nothing else. Keep the wildcard at `[viewer, user, editor, admin]`.
- Highest-risk item to confirm cleared: `pmiPayoffCancellations`, whose `defaultRole: admin` would otherwise hand an authenticated external admin.

**Verify immediately** (within 15 minutes), with real people, not synthetic subjects:

- Dave: fresh browser and CLI session.
- Kevon: fresh browser and MCP session.
- An app admin: management and user-lookup paths.
- `/apps` lists the expected integrations; Meal Swap, AI Spend and Build load.
- The apps behind the non-`app` resource types still work: agent trace viewer, workflows, agents, `pmiPayoffCancellations`, and `itAccountOnboarding`. These are the ones a roster built only around `app` would silently break.
- A fresh CLI session runs read-only Linear and Slack operations; MCP lists and calls them.

**Rollback:** re-add `defaultRole`, redeploy. Memberships stay. Do this on the first failure rather than diagnosing live.

Repeat the checks at 30 minutes and the next business morning. Soak one business day before Phase 4.

## Phase 4 — Externals

**Prerequisite.** Land catalog and MCP listing authorization first. Without it an authenticated external sees every internal app and tool. This must be deployed and verified against an ungranted identity *before* any external domain is enabled.

**Auth0 employee-only cutover.** Verify issuer matches discovery, RS256 + JWKS, correct audience, ID-token and userinfo subjects match, callbacks/logout allowlisted, Google Workspace connection and HRD enabled, database login and public signup disabled, `allowedDomains: [valon.com]`. Run a real authorization-code flow against the exact candidate before deploying. Shift traffic atomically; never split across issuers. Soak one business day.

**Probe.** One temporary domain, one pre-created verified user, no grants. Confirm:

- login succeeds and `GET /api/v1/auth/session` returns a `user:<uuid>`;
- home renders and **`/apps` is empty**;
- MCP `tools/list` returns nothing and `tools/call` refuses;
- direct operation invocation is denied;
- a non-allowlisted domain is rejected at the Gestalt callback;
- employees are unaffected throughout.

Remove the temporary domain afterwards.

**First partner.** One approved domain, a small named cohort, public signup stays disabled. Repeat the checks above.

Externals hold no grants, so grant/revoke workflows and per-app external access are deliberately not built here. Add them when a partner actually needs a specific app.

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
| 8 | gestalt | 4 | Catalog + MCP listing authorization (**prerequisite for externals**) |
| 9+ | toolshed | 4 | Auth0 cutover, probe, first partner |

PRs 1–4 were implemented and passed CI on branches `g2-canonical-identity`, `g3a-unify-authorization-evaluator`, `g1-authorization-state-safety`, and `g4-authorization-conformance-suite`. PR 8 was implemented on `g3b-batched-listing-decisions`. Those branches are pushed; the PRs were closed pending this replan and can be reopened.

## Decisions

**Externals share the valon.tools instance with employees.** A separate instance (e.g. `partners.valon.tools`) with no internal apps mounted would have delivered the same external experience — sign in, see nothing — with zero changes to valon.tools and therefore zero employee-access risk. It was rejected because externals must be on the valon.tools hostname itself.

The consequence is that this migration is mandatory rather than optional, and the employee-access risk it carries is accepted deliberately: `defaultRole` cannot be scoped to exclude a subject, so the only way to stop an authenticated external inheriting viewer on everything is to remove it and grant employees explicitly. Phases 1–3 exist to make that removal safe.

**Externals receive no grants.** Authentication only. This removes any need for external grant/revoke tooling in this rollout, and makes the external acceptance criterion simple and testable: an authenticated external sees an empty `/apps` and can invoke nothing.
