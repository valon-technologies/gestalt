# External Users — Checklist

Phase 0 before any login widening. Details: [plan.md](./plan.md).

## Phase 0 — Authz (toolshed)

- [ ] Test user with zero relationships: record which UIs/MCP invokes work today.
- [ ] Patch `app` in `deploy/config.yaml`: `defaultRole: noaccess`, `"*"` → `nobody`,
      explicit relations on all prod `app` actions.
- [ ] Spot-check apps with `authorizationPolicy` + `allowedRoles`.
- [ ] Regression as granted Valon user (CI/CD, 2–3 apps).
- [ ] Deploy to prod before Auth0.

## Phase 1 — Auth0 (dev)

- [ ] Tenant + Regular Web App (localhost + staging callbacks).
- [ ] Google + Username-Password connections; email verification on database.
- [ ] Optional Action: domain allowlist; block `@valon.com` on database.
- [ ] Dev secrets for staging overlay.

## Phase 2 — Staging

- [ ] `deploy/config.yaml`: Auth0 `issuerUrl`, `allowedDomains`.
- [ ] Staging overlay secrets; regen lockfile if needed.
- [ ] Deploy staging / local gestaltd.

## Phase 3 — Prod

- [ ] Prod tenant; GSM secrets; `terraform/main.tf`.
- [ ] `deploy/prod/config.yaml`: issuer, secrets, `allowedDomains`, `redirectUrl`.
- [ ] Confirm phase 0 live; deploy; monitor auth audit events.

Rollback: Google `issuerUrl` + `google-oauth-*`.

## Phase 4 — Ops

- [ ] Runbook: add domain → `allowedDomains` PR + deploy.
- [ ] Runbook: grant user → session API for UUID → relationship row → deploy.
- [ ] Runbook: revoke → remove relationship (+ optional Auth0 block).
- [ ] Replace `example.com` with first partner domain.

## Verify

| Case | Expected |
| --- | --- |
| No relationships, post-authz patch | 403 UI/MCP |
| `@valon.com` + Google | Login OK; grants work |
| `@example.com` + database | Login OK; no access |
| `user@blocked.com` | Rejected at callback |
| Unverified email | Rejected at callback |
| CLI login (`cli=1`) | Still works |
