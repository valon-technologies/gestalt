# External Users on valon.tools

Support authenticated users whose email domain is not `@valon.com`, while keeping
default access closed. Valon employees retain Google sign-in; allowlisted external
domains (e.g. `@example.com`) can authenticate via Auth0 database login.

## Contents

- [Goals and non-goals](#goals-and-non-goals)
- [Current state](#current-state)
- [Target architecture](#target-architecture)
- [Domain allowlist](#domain-allowlist)
- [Authorization model](#authorization-model)
- [Implementation phases](#implementation-phases)
- [Configuration reference](#configuration-reference)
- [Auth0 setup](#auth0-setup)
- [Toolshed / valon-tools deploy](#toolshed--valon-tools-deploy)
- [Testing and rollout](#testing-and-rollout)
- [Open questions](#open-questions)

## Goals and non-goals

### Goals

- Allowlisted email domains can complete platform login on `https://valon.tools/`.
- `@valon.com` remains allowed and continues to sign in with Google (via Auth0
  Google social connection, not a separate Gestalt identity provider).
- External users receive **no app, MCP, or admin access** until explicitly granted
  in `authorization.relationships`.
- Domain enforcement happens at login (`allowedDomains` on the OIDC identity
  provider), not only at the IdP.
- Single OIDC issuer in Gestalt config (Auth0 tenant); no multi-button login UX
  work in gestaltd for v1.

### Non-goals (v1)

- Per-partner SAML/OIDC enterprise connections (can add Auth0 connections later
  without Gestalt code changes).
- Self-service signup policy beyond what Auth0 configures (invite-only vs open
  registration is an Auth0 dashboard decision).
- Native username/password forms hosted by gestaltd (Auth0 Universal Login only).
- Automatic provisioning of external users into specific apps (manual or scripted
  grant assignment only).
- Changing how Valon employees are named in `authorization.relationships` (stable
  `user:<uuid>` subjects keyed by email remain the source of truth).

## Current state

**Identity.** Production valon.tools uses the generic OIDC identity provider from
`gestalt-providers/auth/oidc`, configured with `issuerUrl: https://accounts.google.com`
and secrets `google-oauth-client-id` / `google-oauth-client-secret`. The config key
is named `google` but the implementation is OIDC, not a bespoke Google plugin.

**Domain filter.** `allowedDomains` is not set today, so Gestalt does not restrict
email domains after a successful OAuth exchange. Any restriction is implicit in who
can use the Google OAuth client / Workspace.

**Authorization gap.** The shared `app` resource type in
`toolshed/valon-tools/deploy/config.yaml` sets `defaultRole: viewer` with a
wildcard action that includes `viewer`. Apps without a dedicated authorization
resource type name map to type `app` for `CheckAccess`. Users with **no**
`authorization.relationships` rows can still receive **viewer** on those resources
via `defaultRole`. Opening login to external domains without tightening this policy
would grant unintended baseline access.

**User records.** On first login, gestaltd calls `FindOrCreateUser(email)` and
issues stable `user:<uuid>` subject IDs. Existing relationship rows in prod
reference those UUIDs (comments note the email). Same email across IdP changes
preserves the internal user id.

## Target architecture

```text
Browser → valon.tools (gestaltd)
              │
              │  GET /api/v1/auth/login
              ▼
         Auth0 Universal Login
              │
              ├── Google connection  → @valon.com (and other Google accounts on allowlist)
              └── Database connection → allowlisted external domains (e.g. @example.com)
              │
              │  redirect + code
              ▼
         GET /api/v1/auth/login/callback
              │
              ▼
    gestalt-providers/auth/oidc
      · exchange code, verify email_verified
      · CheckAllowedDomains(allowedDomains, email)
      · issue session cookie
              │
              ▼
    FindOrCreateUser(email) → user:<uuid>
              │
              ▼
    authorization CheckAccess (relationships + defaultRole)
```

Gestalt continues to expose one login entry point (`/api/v1/auth/login`). Auth0
chooses Google vs database on its hosted page. Gestalt config points at one
`issuerUrl` (`https://<tenant>.auth0.com`).

## Domain allowlist

Enforce domains in **Gestalt OIDC config** using `allowedDomains`:

```yaml
allowedDomains:
  - valon.com
  - example.com
```

Behavior (implemented in `gestalt-providers/auth/internal/userinfo`):

- Empty list → all domains allowed (current prod behavior).
- Non-empty list → login rejected if the verified email domain is not listed
  (case-insensitive match on the part after `@`).

**Allowlist is not authorization.** It only decides who may authenticate. Pair
with closed default authorization for externals.

Optional Auth0-side rules (recommended):

- Auth0 Action: users with `@valon.com` must use the Google connection (block
  database signup/login for internal domain).
- Rate limits and MFA on the database connection.

Add new partner domains by updating `allowedDomains` and redeploying config (no
gestaltd code change).

## Authorization model

### Principle

| Stage | Valon employee (`@valon.com`) | External (`@example.com`) |
| --- | --- | --- |
| Login | Allowed (Google via Auth0) | Allowed (database via Auth0) |
| Default app access | Existing `authorization.relationships` | **Denied** |
| MCP / API invoke | As granted today | Denied until granted |
| Admin UI | As granted today | Denied until granted |

### Required policy change (gestalt / toolshed config)

Tighten the default posture for the `app` resource type before enabling external
login. Recommended approach (mirror `itAccountOnboarding`):

```yaml
app:
  defaultRole: noaccess
  relations:
    noaccess:
      subjectTypes: [subject]
    nobody:
      subjectTypes: [subject]
    viewer: ...
    # ... existing relations preserved
  actions:
    "*":
      relations: [nobody]
    get_me:
      relations: [viewer, operator, admin]   # if needed for session/bootstrap
    # ... preserve explicit actions Valon apps rely on
```

Audit every action on `app` that Valon workflows and apps use today (`get_me`,
`stores.health`, deal-hub actions, CI/CD sync actions, etc.) and ensure each has
explicit `relations` — wildcard `*` should not grant `viewer` by default.

Apps with `authorizationPolicy: <name>` and UI `allowedRoles: [admin]` already deny
ungranted users at the mounted UI layer. The `app` `defaultRole` fix closes the
MCP/API path for provider kind `app`.

### Granting access to an external user

1. User completes Auth0 signup/login; gestaltd creates `user:<uuid>`.
2. Operator adds `authorization.relationships` in deploy config (or future admin UI),
   e.g. viewer on a specific app resource.
3. Redeploy or apply relationship update through the authorization provider.

Document the subject id via admin tooling or `GET /api/v1/auth/session` after a
test login.

## Implementation phases

See [implementation.md](./implementation.md) for a checklist with owners and
verification steps per phase.

| Phase | Summary |
| --- | --- |
| **0 — Authz audit** | Inventory effective access for a user with zero relationships; patch `app.defaultRole`. |
| **1 — Auth0 dev tenant** | Tenant, OIDC app, Google + Database connections, Universal Login. |
| **2 — Gestalt staging** | Point staging/local overlay at Auth0; set `allowedDomains`. |
| **3 — Toolshed prod config** | Secrets, issuer swap, allowlist, deploy. |
| **4 — Operational runbook** | Grant workflow, domain onboarding, incident rollback. |

## Configuration reference

### Identity provider (prod overlay)

Replace Google issuer with Auth0; keep the same OIDC provider package:

```yaml
server:
  providers:
    identity: auth0   # or keep key name `google` — cosmetic only

providers:
  identity:
    auth0:
      source:
        git:
          repo: https://github.com/valon-technologies/gestalt-providers.git
          ref: <pinned ref>
          path: auth/oidc/manifest.yaml
          artifactRepository: valon
          materialization: snapshot
      config:
        issuerUrl: https://<tenant>.auth0.com
        displayName: Sign in
        redirectUrl: https://valon.tools/api/v1/auth/login/callback
        clientId:
          secret:
            provider: secrets
            name: auth0-client-id
        clientSecret:
          secret:
            provider: secrets
            name: auth0-client-secret
        indexeddb: main-db
        scopes:
          - openid
          - email
          - profile
        allowedDomains:
          - valon.com
          - example.com
```

Callback path remains `config.AuthCallbackPath` =
`/api/v1/auth/login/callback`.

### Secrets and Terraform

Add to GSM and `toolshed/valon-tools/terraform/main.tf` secret list:

- `auth0-client-id`
- `auth0-client-secret`

Retain `google-oauth-*` until Google connection credentials live in Auth0 only;
other valon.tools connections (Calendar, Drive, Front Porch IAP, etc.) may still
reference the Google OAuth client independently.

## Auth0 setup

1. **Create tenant** (dev + prod; custom domain optional for prod).
2. **Application** — OIDC, type Regular Web Application:
   - Allowed Callback URLs: `https://valon.tools/api/v1/auth/login/callback`
   - Allowed Logout URLs: `https://valon.tools/`
   - Token Endpoint Authentication: Post (confidential client).
3. **Connections:**
   - **Google** — for workforce; restrict to `@valon.com` in connection or Action.
   - **Username-Password-Authentication** — for external allowlisted domains;
     enable/disable signup per product policy.
4. **Universal Login** — enable both connections on the application.
5. **Email verification** — required (Gestalt OIDC rejects unverified emails).
6. **Optional Action** (login / pre-user-registration):
   - Reject email domains not in `{valon.com, example.com}` (defense in depth
     beside Gestalt `allowedDomains`).
   - Force `@valon.com` through Google connection only.
7. **Issuer URL** for Gestalt: `https://<tenant>.auth0.com` (verify via
   `/.well-known/openid-configuration`).

## Toolshed / valon-tools deploy

Files to change:

| File | Change |
| --- | --- |
| `valon-tools/deploy/config.yaml` | `allowedDomains`, Auth0 issuer in base identity block (dev); **authz `app` hardening** |
| `valon-tools/deploy/prod/config.yaml` | Prod secrets refs, `redirectUrl`, `allowedDomains` overlay |
| `valon-tools/terraform/main.tf` | New secret names |
| `gestalt.lock.json` | Regenerate if provider ref bumps |

Deploy path unchanged: merge to toolshed → CI validates lockfile → deploy
workflow pins gestaltd image.

**Rollback:** revert config to Google `issuerUrl` and previous secrets; Auth0
sessions expire via cookie TTL (`sessionTtl`, default 24h).

## Testing and rollout

### Pre-prod checks

- [ ] User with zero relationships cannot invoke MCP operations on a sample `app`-kind
      provider after authz patch.
- [ ] `@valon.com` via Google: login succeeds; existing grants still work.
- [ ] `@example.com` via database: login succeeds; all mounted apps return 403 or
      empty capability surface without grants.
- [ ] `user@notallowed.com`: rejected at Gestalt callback (`allowedDomains`).
- [ ] `email_verified: false` in Auth0: rejected by OIDC provider.
- [ ] CLI login (`gestalt auth login` / callback with `cli=1`) still works through
      Auth0.

### Staged rollout

1. Authz patch deploy **before** widening login (safe for current Google-only users
   if `get_me` / explicit actions are preserved).
2. Auth0 dev tenant + staging gestaltd config.
3. Prod Auth0 + config swap in a low-traffic window.
4. Add first real external domain to `allowedDomains` when a partner is ready
   (replace `example.com` placeholder).

### Monitoring

- gestaltd auth metrics (`auth.login.start`, `auth.login.complete` audit events).
- Auth0 dashboard: failed logins, signup rate, blocked domains.
- Alert on spike in `403`/`401` from new external subject IDs (expected until
  grants are assigned).

## Open questions

1. **Signup policy** — Open registration on database connection vs invite-only
   (Auth0 creates user, operator sends password reset link).
2. **MFA** — Required for database users at Auth0; optional for Google workforce.
3. **Custom Auth0 domain** — `login.valon.com` vs default `*.auth0.com` issuer.
4. **Per-app external access** — Will some apps use a dedicated authorization
   resource type with `defaultRole: noaccess` instead of relying solely on the
   global `app` tightening?
5. **Subject discovery** — Admin UI or runbook step to map email → `user:<uuid>`
   for relationship edits.

## Related documentation

- [Identity providers](https://gestaltd.ai/providers/identity) — OIDC config shape.
- `gestalt-providers/auth/oidc/README.md` — `allowedDomains`, `displayName`, PKCE.
- `gestalt/docs/content/reference/config-file.mdx` — `providers.identity.oidc`.
- Prior toolshed exploration: Auth0 database login vs SSO (conversation context);
  old standalone toolshed `docs/auth.md` documented OIDC issuers including Auth0.
