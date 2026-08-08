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
- Build `valon-employees` from the Gestalt `users` table — every row whose email is `@valon.com`, by canonical UUID. **Never** from authorization rows: that is what produced the 67-of-358 roster. Every row the filter excludes must be named and dispositioned, never silently dropped. See [Roster source](#roster-source).
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
Catalog and MCP listing authorization is **not** cut. `FilterCatalogForPrincipal` used to be `return cat` — it ignored the principal — and MCP `tools/list` filtered only on token scope. An authenticated external with zero grants would therefore have seen the entire internal app catalog and every tool, even though it could invoke none of them. "No apps visible" makes that a hard prerequisite for Phase 4, not a follow-up. It shipped early, with Phase 1 (PR 9, [#3072](https://github.com/valon-technologies/gestalt/pull/3072)), because the conformance suite does not compile without it; it is inert while `defaultRole` is active and still needs verifying against an ungranted identity before Phase 4.

## Phase 1 — Make group membership work

**Merged and deployed 2026-08-08.** Correctness only. `defaultRole` stays on, so **no one's access changes**.

- Route every authorization boundary through the canonical `user:<uuid>` subject; fail closed on unresolved or provider-opaque subjects.
- Replace the direct-only relationship scans in mounted UI and app admin with the shared evaluator, so group and subject-set grants are honored.
- Stop server startup from writing active authorization state unless explicitly told to, and wire exactly one revision to own that write.
- Land the conformance suite covering direct, one-level, nested, cyclic, malformed and paginated grants across mounted UI, catalog, app admin, invocation and MCP.

**Check — outstanding:** current-access regression for Dave, Kevon, and an app admin. Nothing should change for anyone.

**Note:** `CheckAccess` is action-shaped and there is no effective-roles RPC, so a policy-shaped resource type that declares relations but no actions makes the evaluator deny a question it cannot represent. A bounded transition shim keeps the legacy direct-grant path working in exactly that case and logs a warning. **If that warning never fires in production, the shim is safe to delete** — and its absence is the evidence Phase 2 needs.

## Phase 2 — Build the roster, additively

Still additive. `defaultRole` stays on, so **still no access change**.

- **Enumerate every resource type that declares a `defaultRole`**, resolving YAML anchors and aliases rather than grepping — the six in the table above are the current answer, but re-derive it from the deployed config rather than trusting this doc. This list is exactly the set of grants the group needs, and exactly the set Phase 3 clears.
- Read every row of the Gestalt `users` table and take its canonical UUID. The database is the Cloud SQL instance the valon.tools service mounts, `gitlab-peach-street:us-east4:terra-east4`; credentials are in that project's Secret Manager. Do not hand-transcribe the UUIDs — export them.
- Partition that export on email domain. `@valon.com` (case-insensitive, after normalization) is the roster. Everything else goes on an exclusion list. Every employee is on the Valon domain, so a non-Valon *domain* on that list is a non-employee and needs no adjudication.
- **Read the exclusion list for rows with no usable email** — blank, null, or a provider-opaque subject like `auth0|abc` that never had an email backfilled. Those are not covered by "every employee is on the Valon domain", because they carry no domain to judge. Resolve each to a person before the flip and add them to the roster by name if they are one. This is the same subject shape as the original root cause, so expect it to exist rather than assuming it does not.
- Create `valon-employees` and add a membership per UUID.
- Add **one grant per resource type for the group** — matching the role each `defaultRole` currently confers, so employees keep exactly what they have today. Not per user.
- Store the exported roster outside source control; commit only its digest and aggregate counts.

**Gate:**

1. Membership count plus exclusion-list count equals the `users` table row count. Zero unmapped, zero ambiguous, zero duplicates. **Record all three numbers** — the original roster was 67 against 358 users and nothing flagged it. Three numbers that add up is the check; a single roster count proves nothing.
2. **Zero rows on the exclusion list lack a usable email.** Rows excluded for having a non-Valon domain need no further work. Rows excluded because they have no domain to judge are the gap the filter cannot reason about, and each one is resolved to a person or confirmed not to be one.
3. Every Gestalt user who authenticated in the last 30 days is in the group. A recent authenticator on the exclusion list is the loudest possible signal that the predicate dropped a working user; treat it as a stop, not a note.
4. Every resource type carrying a `defaultRole` has a corresponding group grant. A resource type cleared in Phase 3 without one silently revokes whatever that `defaultRole` was providing.
5. Re-export immediately before Phase 3 and stop on any change in either count.

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

**Prerequisite.** Phases 2 and 3 must be complete. The first external login puts an external into the `users` table the roster is built from. The `@valon.com` filter is what keeps a later rebuild from granting them employee access, so it is load-bearing from this point on: any rebuild after this must re-read its exclusion list rather than trusting the predicate alone. See [Roster source](#roster-source).

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

| # | Repo | Phase | Content | Status |
| --- | --- | --- | --- | --- |
| 1 | gestalt | 1 | Canonical identity at every authorization boundary | merged [#3075](https://github.com/valon-technologies/gestalt/pull/3075) |
| 2 | gestalt | 1 | Mounted UI + app admin decide through the evaluator | merged [#3071](https://github.com/valon-technologies/gestalt/pull/3071) |
| 3 | gestalt | 1 | Startup authorization-state write gated behind explicit apply | merged [#3069](https://github.com/valon-technologies/gestalt/pull/3069) |
| 4 | gestalt | 1 | Conformance suite | merged [#3073](https://github.com/valon-technologies/gestalt/pull/3073) |
| 5 | toolshed | 1 | Wire the state-apply flag to the one owning revision; deploy Phase 1 | merged + deployed [#4144](https://github.com/valon-technologies/toolshed/pull/4144) |
| 6 | toolshed | 2 | `valon-employees` roster, memberships, per-resource-type group grants, reconciliation gate | not started |
| 7 | toolshed | 3 | Clear `defaultRole` on the custom-policy types (`pmiPayoffCancellations`, `itAccountOnboarding`) | not started |
| 8 | toolshed | 3 | Clear `defaultRole` on `app` and the anchor trio (`agent-trace-viewer`/`workflow`/`agent`) | not started |
| 9 | gestalt | 4 | Catalog + MCP listing authorization (**prerequisite for externals**) | merged early with Phase 1, [#3072](https://github.com/valon-technologies/gestalt/pull/3072) |
| 10 | toolshed | 4 | Auth0 secrets — GSM + `terraform/main.tf` | not started |
| 11 | toolshed | 4 | Auth0 employee-only identity cutover | not started |
| 12 | toolshed | 4 | Enable temporary probe domain | not started |
| 13 | toolshed | 4 | Remove probe domain; enable first partner domain | not started |

Phase 3 is split so the two narrow custom-policy types go first as a live rehearsal of the procedure *including the rollback*, before touching `app` and the anchor trio.

PRs 1–5 are merged and deployed as of 2026-08-08, and PR 9 landed with them — see "Work already done" in [implementation_prompt.md](./implementation_prompt.md) for merge commits and the two deviations. Phase 1's current-access regression is still outstanding. PRs 6–8 and 10–13 are not started.

Implementation handoff: [implementation_prompt.md](./implementation_prompt.md).

## Decisions

**Externals share the valon.tools instance with employees.** A separate instance (e.g. `partners.valon.tools`) with no internal apps mounted would have delivered the same external experience — sign in, see nothing — with zero changes to valon.tools and therefore zero employee-access risk. It was rejected because externals must be on the valon.tools hostname itself.

The consequence is that this migration is mandatory rather than optional, and the employee-access risk it carries is accepted deliberately: `defaultRole` cannot be scoped to exclude a subject, so the only way to stop an authenticated external inheriting viewer on everything is to remove it and grant employees explicitly. Phases 1–3 exist to make that removal safe.

**Externals receive no grants.** Authentication only. This removes any need for external grant/revoke tooling in this rollout, and makes the external acceptance criterion simple and testable: an authenticated external sees an empty `/apps` and can invoke nothing.

### Roster source

**`valon-employees` is the `@valon.com` rows of the Gestalt `users` table, not a Rippling join.**

The failure this migration exists to avoid is an *incomplete* roster: employees whose access came solely from `defaultRole` were absent from the group, so clearing `defaultRole` cut them off. The `users` table is the source least able to have that failure. Every subject that can authenticate today has a row in it, so the table is a superset of everyone the flip could hurt. A Rippling join reintroduces the failure mode directly: anyone Rippling omits, or whose email does not join cleanly, silently loses access at the flip. It is also simpler to check — counts that add up, rather than a three-way reconciliation across an HR export, an email normalization, and a UUID join.

**Why the domain filter, given that.** Without it the roster is "every row", which is correct today and wrong the moment Phase 4 lands: externals enter the same table, and any later rebuild would hand them full employee access. The `@valon.com` predicate makes the rule survive its own success — it stays correct after externals exist, so the roster is not a thing that quietly expires.

**The filter is also the one part of this that can subtract.** Every other rule here can only over-grant, which is safe during a migration whose whole risk is under-granting. A predicate that drops a real employee is the original failure wearing different clothes.

Two things bound that risk, and they are not the same thing. Every Valon employee is on the Valon domain — established, and it means a non-Valon *domain* on the exclusion list is a non-employee needing no adjudication. What it does not cover is a row with no domain to judge at all: a blank email, or a provider-opaque subject like `auth0|abc` that never had one backfilled. That shape is exactly the original root cause, so it is the one case the filter genuinely cannot reason about.

So the filter partitions rather than discards, and the reading is narrow: `@valon.com` rows are the roster, the rest are excluded, and the only rows anyone has to look at are the ones carrying no usable email. The gate is three numbers that add up — roster plus exclusions equals total — plus zero unresolved email-less rows. This keeps the property that made the users table the right source (nothing is omitted without someone noticing) while gaining the property that makes it durable (it does not silently absorb externals later).

**What it still does not do:**

- **It is not an employment authority.** Departed employees keep rows, so they keep membership. Today that is a no-op — they cannot authenticate, because Google login is domain-restricted and their account is gone. It becomes real only if a departed identity can authenticate again, which is a deprovisioning concern rather than a migration concern. Track it separately.
- **It does not make Phase 4 ordering optional.** The filter means a rebuild after externals exist is no longer catastrophic, but "no longer catastrophic" is not "verified". Build the roster and complete the flip before enabling any external domain, and treat any post-Phase-4 rebuild as a change that needs its own review of the exclusion list.

A durable employment authority — Rippling or otherwise — remains the right follow-up before this group is next rebuilt from scratch. It is the wrong tool for *this* step, where the cost of omission is an employee locked out and the cost of inclusion is an employee keeping access they already have.
