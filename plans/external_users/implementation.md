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

**PR 3 — Auth0 cutover (`valon.tools`)**

Stack on merged PR 2 (or land in parallel after PR 2 is approved; do not deploy
widened login until PR 2 is live).

- Add `auth0-client-id` and `auth0-client-secret` to GSM and
  `valon-tools/terraform/main.tf`.
- Update identity in `valon-tools/deploy/config.yaml` and
  `valon-tools/deploy/prod/config.yaml`: Auth0 `issuerUrl`,
  `allowedDomains: [valon.com, example.com]`, `displayName`,
  `redirectUrl: https://valon.tools/api/v1/auth/login/callback`, secrets refs.
- Regenerate `gestalt.lock.json` if provider git ref changes.
- Deploy via toolshed CI; monitor `auth.login.*` audit events on valon.tools.

Requires Auth0 application `valon-tools-toolshed` (see Process steps 2–3). Keep
existing `google-oauth-*` GSM secrets for Calendar/Drive/IAP — they are not the
Gestalt login issuer.

Rollback: revert issuer to Google and `google-oauth-*` secrets.

**PR 4 — External user ops runbooks**

Land after PR 3 or in parallel once authz shape is stable.

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
 │    └── toolshed PR 3 — Auth0 cutover (valon.tools)
 └── toolshed PR 4 — external user ops runbooks
```

| PR | Repo | Base branch | Depends on |
| --- | --- | --- | --- |
| 1 | gestalt | `main` | — |
| 2 | toolshed | `main` | — |
| 3 | toolshed | `main` | PR 2 approved; `valon-tools-toolshed` + GSM secrets |
| 4 | toolshed | `main` | PR 2 merged (PR 3 for prod-specific steps) |

Auth0 application setup (dashboard) is not a git PR; complete before PR 3 deploy.

### Process

1. **gestalt design (PR 1)** — Merge plan docs; get explicit approval on authz +
   Auth0 approach.
2. **Auth0 application `valon-tools-toolshed`** — In your existing Auth0 tenant,
   create a **new Regular Web Application** for valon.tools Gestalt login (do not
   repurpose unrelated apps):
   - **Name:** `valon-tools-toolshed`
   - **Callbacks:** `https://valon.tools/api/v1/auth/login/callback`
   - **Connections:** Google (for `@valon.com`) + `Username-Password-Authentication`
     (externals / probe users)
   - **Universal Login:** both connections enabled; email verification on database
   - Optional Action: block `@valon.com` on database; duplicate domain allowlist
   - Copy **Client ID** and **Client Secret** for step 3
   Docs:
   [Database connections](https://auth0.com/docs/authenticate/database-connections),
   [Universal Login](https://auth0.com/docs/authenticate/login/auth0-universal-login).
3. **GSM secrets** — Store app credentials in `gitlab-peach-street` as
   `auth0-client-id` / `auth0-client-secret`. Do not overwrite
   `google-oauth-client-id` / `google-oauth-client-secret` (Google API / IAP).
4. **Authz hardening (toolshed PR 2)** — Implement, babysit CI/Bugbot, deploy to
   valon.tools before any login widening. Verify ungranted test subject gets 403 on
   UI/MCP. Can run in parallel with steps 2–3.
5. **Auth0 cutover (toolshed PR 3)** — Wire valon.tools identity config to
   `valon-tools-toolshed`; deploy; create verified external probe user (see
   **External domain probe** below); run verification matrix on valon.tools.
6. **Ops runbooks (toolshed PR 4)** — Document domain onboarding and grant/revoke
   workflow for support.
7. **First partner** — Add real domain to `allowedDomains`; configure Auth0 signup
   policy (open vs invite-only); grant relationships for pilot users.

## Tests

### Authorization (PR 2)

- Subject with zero `authorization.relationships`: 403 on mounted app UIs and MCP
  invokes after patch (except explicitly public ops).
- Granted Valon user: no regression on CI/CD and 2–3 high-traffic apps.

### Login and domain allowlist (PR 3)

#### External domain probe (`valon.tools`)

Test allowlisted external login **without** owning an inbox for the probe address
(e.g. `probe@example.com`). Use Auth0 admin create with **email verified** set.

**Prerequisites:** PR 2 deployed on valon.tools; PR 3 live with Auth0 identity and
`allowedDomains: [valon.com, example.com]` (or your probe domain).

1. **Auth0 `valon-tools-toolshed`** (Process step 2) — User Management → Create user:
   - Connection: `Username-Password-Authentication`
   - Email: `probe@example.com` (any `@example.com` address)
   - Password: test-only password
   - Enable **email verified** (or equivalent) — no confirmation email required
2. **Gestalt** — Do **not** add `authorization.relationships` for this user.
3. **Login** — https://valon.tools → Universal Login → database → `probe@example.com`.
4. **Record UUID** — `GET /api/v1/auth/session` → `user:<uuid>` for later grant tests.
5. **Negative controls** — Auth0 user on a non-allowlisted domain (e.g.
   `other@gmail.com`) should fail at Gestalt callback (`allowedDomains`).

| Step | Expected |
| --- | --- |
| `probe@example.com` login | OK |
| `GET /api/v1/auth/session` | Returns `user:<uuid>` |
| Mounted app UI / MCP / admin | 403 (no grants) |
| `user@notallowed.com` | Rejected at Gestalt callback |

Remove `example.com` from `allowedDomains` when probe testing is complete unless
contracted for a partner.

#### Verification matrix

| Case | Expected |
| --- | --- |
| `@valon.com` + Google | Login OK; existing grants work |
| `@example.com` + database (verified probe user) | Login OK; no app/MCP/admin access without grants |
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
