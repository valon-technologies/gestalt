# Implement the external-users migration on valon.tools

Read [implementation_v2.md](./implementation_v2.md) first. It is the spec; this document is
the framing and the guardrails.

## Goal

Make valon.tools reachable by users from an approved external email domain, without
internal users losing access.

- **External users get authentication only.** They sign in, land on the valon.tools home
  page, and get nothing else: no apps visible in `/apps`, no MCP tools listed, nothing
  invocable, zero authorization grants.
- **Internal users move into a `valon-employees` group** and receive **at least reader
  (`viewer`) on every app**, granted to the group — never per user.

The migration is one substitution: `defaultRole` (an implicit blanket allow that grants
every authenticated subject) is removed, and employees are granted explicitly via
`valon-employees` instead. `defaultRole` must go, because the moment an external can
authenticate they inherit it.

## This migration was attempted on 2026-08-06 and broke employee access. Read what happened.

Do not start until you have looked at these. The failure modes are specific and repeatable.

**The two things that actually caused access loss:**

1. **The employee roster was built from existing authorization rows instead of an employee
   authority.** It contained 67 UUIDs against 358 real users — only 64 matched. Employees
   whose access came solely from `defaultRole` were simply absent from the group, so
   clearing `defaultRole` cut them off. The validator checked the same incomplete source,
   so nothing flagged it.

2. **Group membership was not honored on every authorization path.** `CheckAccess`
   understood subject sets, but mounted UI, catalog, and app-admin each ran their own
   direct-grant-only relationship scan. An employee *in* the group was still denied.
   - [gestalt#3061](https://github.com/valon-technologies/gestalt/pull/3061) — "Filter
     /apps catalog by viewer/editor/admin grants". Introduced direct-only catalog checks;
     broke group-only catalog access.
   - [gestalt#3062](https://github.com/valon-technologies/gestalt/pull/3062) — "Resolve
     mounted UI roles through subject sets". Fixed the mounted-role helper and catalog, but
     **app-admin checks stayed direct-only**, so the incident continued after this shipped.
   - Root cause found later: `resolvePrincipalUserID` treated any subject without an `@` as
     already canonical, so a provider-opaque subject like `auth0|abc` reached authorization
     unresolved and matched no relationship.

**What made recovery worse:**

3. **The shared app wildcard was set to `[nobody]`**, denying 713 operations that relied on
   wildcard viewer access.
   [toolshed#4062](https://github.com/valon-technologies/toolshed/pull/4062) — "Restore
   staff access for registry app operations" restored `[viewer, user, editor, admin]`.

4. **The hotfix was oversized.**
   [toolshed#4071](https://github.com/valon-technologies/toolshed/pull/4071) — "Hotfix
   direct Valon employee app grants" generated 5,762 direct per-user grants and failed
   startup probes. Reverted by
   [toolshed#4072](https://github.com/valon-technologies/toolshed/pull/4072) and
   [toolshed#4073](https://github.com/valon-technologies/toolshed/pull/4073).

5. **Auth0 was configured untested** — issuer mismatch, missing connection/HRD setup, then
   HS256 tokens, discovered in sequence during the cutover.

6. **No-traffic Cloud Run candidates could overwrite shared authorization state at
   startup**, and traffic rollback did not restore it.

Fallout continued for days: [toolshed#4124](https://github.com/valon-technologies/toolshed/pull/4124),
[#4134](https://github.com/valon-technologies/toolshed/pull/4134),
[#4139](https://github.com/valon-technologies/toolshed/pull/4139),
[#4140](https://github.com/valon-technologies/toolshed/pull/4140), and
[#4141](https://github.com/valon-technologies/toolshed/pull/4141) are all follow-on grant
and resource-type repairs.

## Non-negotiables

- **Never** build the roster from authorization rows. Use Rippling (the People authority),
  joined to canonical Gestalt user UUIDs by normalized email.
- **Never** set the shared app wildcard to `[nobody]`. Keep
  `actions: "*": relations: [viewer, user, editor, admin]`.
- **Never** generate per-user/per-app grants. One grant per resource type, to the group.
- Keep Google login until employee authorization has passed and soaked one business day.
- Roll back on the first failed check. Do not diagnose live and do not stack hotfixes.

## Traps that are easy to miss

- **`defaultRole` is on six resource types, not just `app`.** In
  `toolshed/valon-tools/deploy/config.yaml`: `agent-trace-viewer` (`viewer`), `workflow`
  and `agent` (aliases of it), `app` (`viewer`), `pmiPayoffCancellations` (**`admin`**),
  `itAccountOnboarding` (`noaccess`). Clearing only `app` leaves an external with viewer on
  three types and **admin on one**. Re-derive this list from the deployed config; do not
  trust this document.
- **`agent-trace-viewer` is a YAML anchor** (`&defaultAuthzResource`). Grepping
  `defaultRole` finds four sites; there are six. Editing it silently changes `workflow` and
  `agent` too.
- **`noaccess` is not a deny.** `authorizeMountedResourceRoles` allows whenever
  `defaultRole != "" && len(allowedRoles) == 0`. Omit the field entirely; never set a
  sentinel.
- **Grants must match or exceed what each `defaultRole` confers today.** "At least reader"
  is the floor, not the target — `pmiPayoffCancellations` currently confers `admin`, so
  granting the group `viewer` there is a downgrade that breaks that app for employees.
  Every resource type you clear needs a corresponding group grant, or you have revoked
  access rather than migrated it.
- **`CheckAccess` is action-shaped and there is no effective-roles RPC.** A policy-shaped
  resource type that declares relations but no actions makes the evaluator deny a question
  it cannot represent. A bounded transition shim handles this; if its warning never fires
  in production, the shim can be deleted.

## Work already done

Five gestalt PRs were implemented and passed CI, then closed pending a replan. The branches
are pushed; reopen or re-file them rather than rewriting:

| Branch | Content | Closed PR |
| --- | --- | --- |
| `g2-canonical-identity` | Canonical identity at every authorization boundary | [#3070](https://github.com/valon-technologies/gestalt/pull/3070) |
| `g3a-unify-authorization-evaluator` | Mounted UI + app admin decide through the evaluator | [#3071](https://github.com/valon-technologies/gestalt/pull/3071) |
| `g1-authorization-state-safety` | Startup authorization-state write gated behind explicit apply | [#3069](https://github.com/valon-technologies/gestalt/pull/3069) |
| `g4-authorization-conformance-suite` | Conformance suite across every server surface | [#3073](https://github.com/valon-technologies/gestalt/pull/3073) |
| `g3b-batched-listing-decisions` | Catalog + MCP listing authorization | [#3072](https://github.com/valon-technologies/gestalt/pull/3072) |

Caveat on the conformance suite: it runs against an in-repo reference evaluator, not the
real provider (that provider is a separate module in another repository and needs a live
indexeddb host service). It proves every server surface agrees with every other; it does
not prove the shipped provider implements those semantics.

## Sequence — 13 PRs, four phases

Phases 1 and 2 are additive with `defaultRole` still active, so **nobody's access changes**
and mistakes are free. Only Phase 3 carries risk.

**Phase 1 — make group membership work** (PRs 1–5). Land the five gestalt branches above,
wire the state-apply flag to exactly one owning revision in Toolshed, deploy under Google.
*Check:* current-access regression for real employees. Nothing should change for anyone.

**Phase 2 — build the roster, additively** (PR 6). Enumerate every resource type declaring a
`defaultRole`. Build `valon-employees` from Rippling. Add memberships and one grant per
resource type to the group.
*Gate:* every active employee resolves to exactly one Gestalt UUID and is in the group;
every user who authenticated in the last 30 days is either in the group or an identified
non-employee, **with the count reconciled explicitly** — this is the check that would have
caught the original 67-vs-358 failure; every cleared resource type has a matching grant.

**Phase 3 — the flip** (PRs 7–8). Clear `defaultRole` on the two narrow custom-policy types
first as a live rehearsal including the rollback, then on `app` and the anchor trio.
*Verify within 15 minutes with real people*, not synthetic subjects: fresh browser, CLI, and
MCP sessions; `/apps` lists expected integrations; the apps behind the non-`app` resource
types still work.
*Rollback:* re-add `defaultRole` and redeploy. Group memberships stay, so reverting costs
nothing. Do this on the first failure.
Soak one business day.

**Phase 4 — externals** (PRs 9–13). Catalog/MCP listing authorization must be deployed and
verified against an ungranted identity **before** any external domain is enabled — without
it an authenticated external sees the entire internal app catalog. Then Auth0 secrets,
employee-only Auth0 cutover (verify issuer matches discovery, RS256 + JWKS, audience,
subjects match, callbacks allowlisted, HRD enabled, public signup disabled,
`allowedDomains: [valon.com]`, via a real authorization-code flow before deploying), then a
temporary probe domain, then the first partner domain.
*External acceptance test:* an authenticated external sees an empty `/apps`, `tools/list`
returns nothing, and invocation is denied.

## How to work

- Open PRs; do not merge without review. Two approvals per gate: Gestalt authorization owner
  and Toolshed/on-call owner.
- Push with `git push origin <branch>:<branch>` and open PRs with `gh pr create`. Do **not**
  use `gs branch submit` — it reads `branch.<name>.merge` as the remote name and can push
  straight to `main`, and the credentials in use bypass branch protection.
- Run `go build ./...`, `go test -count=1 ./...`, and `golangci-lint run <touched packages>`
  (v2.12.1, config at `gestaltd/.golangci.yml`; `paralleltest` requires `t.Parallel()`
  except where `t.Setenv` is used) before pushing.
- `TestAgentRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields` in
  `internal/bootstrap` flakes under full-suite load on `main`. It is pre-existing.
