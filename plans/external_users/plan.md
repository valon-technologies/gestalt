# External Users on valon.tools

Allowlisted domains (e.g. `valon.com`, `example.com`) can log in. `@valon.com` via
Auth0 Google connection; externals via Auth0 database. No app/MCP/admin access without
`authorization.relationships`. Checklist: [implementation.md](./implementation.md).

## Current state

- Identity: `gestalt-providers/auth/oidc` with `issuerUrl: https://accounts.google.com`
  (`toolshed/valon-tools/deploy`; config key `google`).
- No `allowedDomains` — Gestalt accepts any verified email after OAuth.
- **Blocker:** `app` resource type has `defaultRole: viewer` and `"*"` includes
  `viewer`. Ungranted users get viewer on `app`-kind checks. Fix before widening login.
- Users: `FindOrCreateUser(email)` → stable `user:<uuid>`; grants use UUIDs in prod.

## Flow

```text
Browser → GET /api/v1/auth/login → Auth0 Universal Login
  (Google | Database) → /api/v1/auth/login/callback
  → oidc provider (email_verified, allowedDomains) → session cookie
  → FindOrCreateUser → CheckAccess (relationships + defaultRole)
```

One Gestalt OIDC issuer (`https://<tenant>.auth0.com`). Login UI stays on Auth0.

## Domain allowlist

```yaml
allowedDomains:
  - valon.com
  - example.com
```

Empty list = allow all. Non-empty = reject at callback if domain not listed
(`gestalt-providers/auth/internal/userinfo`). Not a substitute for authz.

Optional Auth0 Action: force `@valon.com` to Google; duplicate domain check.

## Authorization

| | `@valon.com` | External |
| --- | --- | --- |
| Login | Yes | Yes if allowlisted |
| Default access | Existing grants | Denied |

Before external login, patch `app` in `toolshed/valon-tools/deploy/config.yaml`
(mirror `itAccountOnboarding`):

- `defaultRole: noaccess`
- `"*": relations: [nobody]`
- Explicit `relations` on every `app` action Valon uses (`get_me`, `stores.health`,
  deal-hub, ci-cd sync, …)

Grant externals: login → `GET /api/v1/auth/session` for `user:<uuid>` → add
relationship → deploy.

## Gestalt / toolshed config

```yaml
providers:
  identity:
    auth0:
      source: { git: ... path: auth/oidc/manifest.yaml }
      config:
        issuerUrl: https://<tenant>.auth0.com
        redirectUrl: https://valon.tools/api/v1/auth/login/callback
        clientId: { secret: auth0-client-id }
        clientSecret: { secret: auth0-client-secret }
        indexeddb: main-db
        scopes: [openid, email, profile]
        allowedDomains: [valon.com, example.com]
```

GSM + `toolshed/valon-tools/terraform/main.tf`: `auth0-client-id`,
`auth0-client-secret`. Keep `google-oauth-*` for Calendar/Drive/IAP connections.

Files: `deploy/config.yaml` (authz + dev identity), `deploy/prod/config.yaml`,
`gestalt.lock.json` if ref changes.

## Auth0

1. Regular Web App; callback `https://valon.tools/api/v1/auth/login/callback`.
2. Connections: Google (`@valon.com`), Username-Password (externals).
3. Universal Login: both enabled; email verification required on database.
4. Issuer: `https://<tenant>.auth0.com`

Rollback: revert issuer to Google + `google-oauth-*` secrets.

## Rollout

1. Ship authz patch (phase 0).
2. Auth0 dev + staging config.
3. Prod cutover.
4. Swap `example.com` for real partner domains in `allowedDomains`.

## Open questions

- Database signup: open vs invite-only?
- MFA on database connection?
- Custom Auth0 domain?
- Per-app `defaultRole: noaccess` resource types vs global `app` patch only?
