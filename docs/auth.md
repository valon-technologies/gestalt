# Authentication

Gestalt has pluggable platform authentication. Configure it under `auth:` in `config.yaml`.

## Available Providers

### `none`

Use `none` when you want Gestalt to skip platform authentication entirely.

```yaml
auth:
  provider: none
```

Every request is treated as coming from a single anonymous user.

### `local`

The built-in local provider is the simplest authenticated way to run Gestalt without any external identity system.

```yaml
auth:
  provider: local
  config:
    email: local@example.com
```

`email` is optional. If omitted, Gestalt uses `local@gestalt.local`.

### `google`

Google uses Google's OAuth endpoints directly.

```yaml
auth:
  provider: google
  config:
    client_id: ${GOOGLE_CLIENT_ID}
    client_secret: ${GOOGLE_CLIENT_SECRET}
    allowed_domains:
      - example.com
```

If `redirect_url` is omitted and `server.base_url` is set, Gestalt derives the callback automatically as:

```text
<base-url>/api/v1/auth/login/callback
```

### `oidc`

OIDC works with providers such as Okta, Auth0, Azure AD, and Keycloak.

```yaml
auth:
  provider: oidc
  config:
    issuer_url: https://login.example.com
    client_id: ${OIDC_CLIENT_ID}
    client_secret: ${OIDC_CLIENT_SECRET}
    session_secret: ${OIDC_SESSION_SECRET}
    display_name: Company SSO
    scopes:
      - openid
      - email
      - profile
    allowed_domains:
      - example.com
    pkce: true
```

## Field Summary

| Field | Provider | Notes |
| --- | --- | --- |
| none | `none` | No provider-specific config. |
| `email` | `local` | Optional local-login email. |
| `client_id` | `google`, `oidc` | OAuth client ID. |
| `client_secret` | `google`, `oidc` | OAuth client secret. |
| `redirect_url` | `google`, `oidc` | Optional explicit callback URL. |
| `allowed_domains` | `google`, `oidc` | Optional email-domain allowlist. |
| `issuer_url` | `oidc` | OIDC discovery URL. |
| `session_secret` | `oidc` | Secret used to sign OIDC-backed sessions. |
| `session_ttl` | `oidc` | Optional session lifetime, defaults to `24h`. |
| `scopes` | `oidc` | Optional scope list, defaults to `openid`, `email`, `profile`. |
| `pkce` | `oidc` | Optional PKCE flag. |
| `display_name` | `oidc` | Optional login button label, defaults to `SSO`. |

## Callback Path

Platform login callbacks always land on:

```text
/api/v1/auth/login/callback
```

That is distinct from the integration OAuth callback path used for upstream provider connections.
