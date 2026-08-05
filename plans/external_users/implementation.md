# External Users — Implementation

Allowlisted external login on valon.tools. Design: [plan.md](./plan.md).

## Implementation

### gestalt

**PR 1 — Design doc**

Land [plan.md](./plan.md) and this file. No runtime changes.

### toolshed

**PR 2 — `app` authorization hardening**

- Patch `app` in `valon-tools/deploy/config.yaml`: `defaultRole: noaccess`, `"*"` →
  `nobody`, explicit `relations` on every prod `app` action (`get_me`,
  `stores.health`, deal-hub, ci-cd sync, …).
- Spot-check apps with `authorizationPolicy` and `allowedRoles`.
- Regression as a granted Valon user (CI/CD, 2–3 apps).
- Deploy to prod. **No identity provider change.**

**PR 3 — Auth0 staging identity config**

Stack on merged PR 2 (or land in parallel after PR 2 is approved; do not deploy
widened login until PR 2 is live).

- Update `valon-tools/deploy/config.yaml` identity block: Auth0 `issuerUrl`,
  `allowedDomains: [valon.com, example.com]`, `displayName`.
- Point staging overlay at `auth0-client-id` / `auth0-client-secret` (dev GSM or
  staging secrets).
- Regenerate `gestalt.lock.json` if provider git ref changes.
- Deploy staging / run local gestaltd against staging Auth0 tenant.

Requires Auth0 dev tenant (see Process step 1).

**PR 4 — Auth0 production cutover**

Stack on merged PR 2. Land after PR 3 staging verification.

- Add `auth0-client-id` and `auth0-client-secret` to GSM and
  `valon-tools/terraform/main.tf`.
- Update `valon-tools/deploy/prod/config.yaml`: issuer, secrets, `allowedDomains`,
  `redirectUrl: https://valon.tools/api/v1/auth/login/callback`.
- Deploy via toolshed CI; monitor `auth.login.*` audit events.

Rollback: revert issuer to Google and `google-oauth-*` secrets.

**PR 5 — External user ops runbooks**

Land after PR 4 or in parallel once authz shape is stable.

- Runbook: add partner domain → `allowedDomains` PR + deploy (+ optional Auth0
  Action).
- Runbook: grant external user → `GET /api/v1/auth/session` for `user:<uuid>` →
  `authorization.relationships` row → deploy.
- Runbook: revoke → remove relationship; optional Auth0 user block.
- Replace placeholder `example.com` with first partner domain when contracted.

### gestalt-providers

No code changes expected for v1. OIDC provider already supports `allowedDomains`
and generic Auth0 issuer.

### Stacking

```text
main
 ├── gestalt PR 1 — design doc
 ├── toolshed PR 2 — app authorization hardening
 │    └── toolshed PR 3 — Auth0 staging identity config
 │         └── toolshed PR 4 — Auth0 production cutover
 └── toolshed PR 5 — external user ops runbooks
```

| PR | Repo | Base branch | Depends on |
| --- | --- | --- | --- |
| 1 | gestalt | `main` | — |
| 2 | toolshed | `main` | — |
| 3 | toolshed | `main` | PR 2 approved; Auth0 dev tenant |
| 4 | toolshed | `main` | PR 2 merged; PR 3 verified on staging |
| 5 | toolshed | `main` | PR 2 merged (PR 4 for prod-specific steps) |

Auth0 tenant setup (dashboard) is not a git PR; complete before PR 3 deploy.

### Process

1. **Auth0 dev tenant** — Regular Web Application; callbacks for localhost and
   staging; Google + `Username-Password-Authentication` connections; Universal Login
   with both enabled; email verification on database. Optional Action: domain
   allowlist; block `@valon.com` on database. Docs:
   [Database connections](https://auth0.com/docs/authenticate/database-connections),
   [Universal Login](https://auth0.com/docs/authenticate/login/auth0-universal-login).
2. **gestalt design (PR 1)** — Merge plan docs; get explicit approval on authz +
   Auth0 approach.
3. **Authz hardening (toolshed PR 2)** — Implement, babysit CI/Bugbot, deploy to
   prod before any login widening. Verify ungranted test subject gets 403 on UI/MCP.
4. **Staging Auth0 (toolshed PR 3)** — Point staging config at dev tenant; run
   verification matrix below on staging.
5. **Auth0 prod tenant** — Mirror dev settings; prod callback
   `https://valon.tools/api/v1/auth/login/callback`; GSM secrets.
6. **Production cutover (toolshed PR 4)** — Merge after staging sign-off; monitor
   auth audit logs; confirm Valon Google login still works.
7. **Ops runbooks (toolshed PR 5)** — Document domain onboarding and grant/revoke
   workflow for support.
8. **First partner** — Add real domain to `allowedDomains`; configure Auth0 signup
   policy (open vs invite-only); grant relationships for pilot users.

## Tests

### Authorization (PR 2)

- Subject with zero `authorization.relationships`: 403 on mounted app UIs and MCP
  invokes after patch (except explicitly public ops).
- Granted Valon user: no regression on CI/CD and 2–3 high-traffic apps.

### Login and domain allowlist (PR 3–4)

| Case | Expected |
| --- | --- |
| `@valon.com` + Google | Login OK; existing grants work |
| `@example.com` + database | Login OK; no app/MCP/admin access without grants |
| `user@notallowed.com` | Rejected at Gestalt callback (`allowedDomains`) |
| Unverified email | Rejected by OIDC provider |
| CLI login (`cli=1`) | Still works through Auth0 |

### Rollback

- Revert to Google `issuerUrl` + `google-oauth-*`: Valon users can log in; external
  database users cannot.

## Related Docs

<pre>
├── <a href="./plan.md">plan.md</a> — design, config, Auth0 summary
├── <a href="https://auth0.com/docs/authenticate/database-connections">Auth0 — Database connections</a>
├── <a href="https://auth0.com/docs/authenticate/login/auth0-universal-login">Auth0 — Universal Login</a>
├── <a href="https://auth0.com/docs/get-started/authentication-and-authorization-flow/authorization-code-flow">Auth0 — Authorization Code Flow</a>
├── <a href="https://github.com/valon-technologies/gestalt-providers/tree/main/auth/oidc">gestalt-providers/auth/oidc</a>
└── <a href="../../docs/content/providers/identity.mdx">gestaltd — Identity providers</a>
</pre>
