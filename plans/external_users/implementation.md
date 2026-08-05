# External Users — Implementation Checklist

Phased work to ship allowlisted external login on valon.tools. Complete phases in
order; phase 0 is a prerequisite for any production login widening.

## Phase 0 — Authorization hardening (toolshed)

**Goal:** A authenticated subject with zero `authorization.relationships` cannot
read or invoke Valon apps by inheriting `app.defaultRole: viewer`.

### Tasks

- [ ] **0.1 Baseline audit** — Deploy config to a dev gestaltd with `identity:
      local` or staging; create a test user with no relationships. Record which
      mounted UIs load and which MCP `invoke` calls succeed today.
- [ ] **0.2 Patch `app` resource type** in `toolshed/valon-tools/deploy/config.yaml`:
      - Set `defaultRole: noaccess` (or remove `defaultRole` and rely on deny-by-default
        if all actions are explicit — prefer explicit `noaccess` for clarity).
      - Change `"*"` action to `relations: [nobody]` (or equivalent deny relation).
      - List every `app` action used in prod (grep deploy config and app manifests
        for `get_me`, `stores.health`, deal-hub, ci-cd sync, etc.) and assign explicit
        `relations` so Valon behavior is unchanged **for granted users**.
- [ ] **0.3 Per-app spot check** — For each app with `authorizationPolicy`, confirm
      ungranted users are denied at UI (`allowedRoles`) and API layers after 0.2.
- [ ] **0.4 Regression** — Run CI/CD dev workflow, open 2–3 high-traffic apps as a
      granted Valon user; confirm no permission regressions.
- [ ] **0.5 Ship authz-only deploy** — Merge and deploy to prod **before** Auth0
      cutover.

### Verification

```text
Subject: user:<uuid> with no relationships
Expect: 403 on mounted app UIs and MCP invokes (except explicitly public ops if any)
```

## Phase 1 — Auth0 tenant (dev)

**Goal:** IdP ready for gestaltd OIDC integration testing.

### Tasks

- [ ] **1.1** Create dev Auth0 tenant.
- [ ] **1.2** Create Regular Web Application; callback
      `http://localhost:8080/api/v1/auth/login/callback` and staging URL.
- [ ] **1.3** Enable **Google** connection; configure for `@valon.com` workforce.
- [ ] **1.4** Enable **Username-Password-Authentication**; decide signup policy
      (invite-only recommended for first partner).
- [ ] **1.5** Require email verification on database connection.
- [ ] **1.6** (Optional) Auth0 Action: domain allowlist `{valon.com, example.com}`;
      block `@valon.com` on database connection.
- [ ] **1.7** Store client id/secret in dev secret manager or local env for
      `gestalt-dev.yaml` / staging overlay.

### Verification

- Manual login at Auth0 Universal Login with Google test account and database test
  user `@example.com`.
- `/.well-known/openid-configuration` returns `authorization_endpoint`,
  `token_endpoint`, `userinfo_endpoint`.

## Phase 2 — Gestalt / toolshed staging config

**Goal:** End-to-end login on non-prod with `allowedDomains`.

### Tasks

- [ ] **2.1** Update `valon-tools/deploy/config.yaml` identity block:
      - `issuerUrl: https://<dev-tenant>.auth0.com`
      - `allowedDomains: [valon.com, example.com]`
      - `displayName: Sign in`
      - Rename provider key to `auth0` (optional) and set
        `server.providers.identity: auth0`.
- [ ] **2.2** Point staging `prod/config.yaml` overlay (or staging-specific overlay)
      at Auth0 secrets.
- [ ] **2.3** Regenerate `gestalt.lock.json` if provider git ref changes.
- [ ] **2.4** Deploy staging valon.tools (or run local gestaltd with prod config
      merge + dev secrets).

### Verification

| Case | Expected |
| --- | --- |
| `@valon.com` + Google | Login OK; grants work |
| `@example.com` + database | Login OK; no app access |
| `user@blocked.com` | Error at callback (domain) |
| Unverified email | Error at callback |

## Phase 3 — Production cutover

**Goal:** valon.tools uses Auth0 with domain allowlist.

### Tasks

- [ ] **3.1** Create prod Auth0 tenant (or promote dev tenant settings).
- [ ] **3.2** Add GSM secrets `auth0-client-id`, `auth0-client-secret`; update
      `toolshed/valon-tools/terraform/main.tf`.
- [ ] **3.3** Update `valon-tools/deploy/prod/config.yaml`:
      - Identity issuer, secrets, `allowedDomains`.
      - `redirectUrl: https://valon.tools/api/v1/auth/login/callback`.
- [ ] **3.4** Confirm phase 0 authz deploy is live in prod.
- [ ] **3.5** Deploy via toolshed CI; monitor auth audit logs during cutover.
- [ ] **3.6** Communicate to Valon: login page now Auth0 Universal Login (Google
      button should still work).

### Rollback

- Revert deploy config to Google `issuerUrl` and `google-oauth-*` secrets.
- No gestaltd binary rollback required if only config changed.

## Phase 4 — Operations

**Goal:** Repeatable process for partners and support.

### Tasks

- [ ] **4.1 Runbook** — How to add a domain to `allowedDomains` (PR to toolshed,
      deploy, no Auth0 change if only Gestalt enforces list; optional Auth0 Action
      update for defense in depth).
- [ ] **4.2 Runbook** — How to grant an external user access (find `user:<uuid>` via
      session endpoint or admin tooling; add `authorization.relationships` row;
      deploy).
- [ ] **4.3 Runbook** — How to revoke external access (remove relationship; optional
      Auth0 user block).
- [ ] **4.4** Replace placeholder `example.com` with first real partner domain when
      contracted.

## Gestalt platform follow-ups (optional, post-v1)

Not required for initial valon.tools ship, but tracked for product completeness:

- [ ] **Multi-IdP login UI** — `GET /api/v1/auth/info` listing multiple providers
      (only needed if Gestalt hosts more than one OIDC issuer simultaneously).
- [ ] **Admin UI: external user grants** — Reduce reliance on config-file
      relationship edits.
- [ ] **Auth0 Actions as config** — Document standard Action templates in
      gestalt-providers or toolshed docs.

## File touch list

| Repository | Paths |
| --- | --- |
| toolshed | `valon-tools/deploy/config.yaml` |
| toolshed | `valon-tools/deploy/prod/config.yaml` |
| toolshed | `valon-tools/terraform/main.tf` |
| toolshed | `valon-tools/deploy/gestalt.lock.json` (regenerated) |
| gestalt | `plans/external_users/*` (this plan) |
| gestalt-providers | No code changes expected for v1 |
