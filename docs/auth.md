# Authentication

Gestalt supports pluggable user authentication. Configure your provider under `auth:` in config.yaml.

## Google

Uses Google's OAuth2 endpoints directly. Best for Google Workspace organizations.

```yaml
auth:
  provider: google
  config:
    client_id: ${GOOGLE_CLIENT_ID}
    client_secret: ${GOOGLE_CLIENT_SECRET}
    allowed_domains:              # optional; empty = allow all
      - example.com
```

### Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Create an OAuth 2.0 Client ID (Web application)
3. Add `<your-base-url>/api/v1/auth/login/callback` as an authorized redirect URI
4. Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`

## OIDC

Generic OpenID Connect provider. Works with any IdP that supports OIDC discovery, including Okta, Azure AD, Auth0, Keycloak, Firebase Auth, Rippling, OneLogin, and others.

```yaml
auth:
  provider: oidc
  config:
    issuer_url: https://login.example.com
    client_id: ${OIDC_CLIENT_ID}
    client_secret: ${OIDC_CLIENT_SECRET}
    session_secret: ${GESTALT_SESSION_SECRET}
    display_name: "Okta"          # shown on the login button
    scopes:                       # optional; defaults to openid, email, profile
      - openid
      - email
      - profile
    allowed_domains:              # optional; empty = allow all
      - example.com
    pkce: false                   # set true for public clients
```

### Common issuer URLs

| Provider     | Issuer URL                                             |
|-------------|--------------------------------------------------------|
| Okta         | `https://<your-org>.okta.com`                          |
| Azure AD     | `https://login.microsoftonline.com/<tenant-id>/v2.0`   |
| Auth0        | `https://<your-tenant>.auth0.com`                      |
| Keycloak     | `https://<host>/realms/<realm>`                        |
| Google       | `https://accounts.google.com`                          |
| Firebase     | `https://securetoken.google.com/<project-id>`          |
| OneLogin     | `https://<your-org>.onelogin.com/oidc/2`               |

### Setup (Okta example)

1. Create a new OIDC Web Application in the Okta admin console
2. Set the redirect URI to `<your-base-url>/api/v1/auth/login/callback`
3. Copy the Client ID and Client Secret
4. Set `issuer_url` to `https://<your-org>.okta.com`
5. Set `display_name` to `"Okta"` so the login button reads "Sign in with Okta"

## Config reference

| Field            | Required | Default                     | Description                                     |
|------------------|----------|-----------------------------|-------------------------------------------------|
| `provider`       | yes      | —                           | `google` or `oidc`                              |
| `client_id`      | yes      | —                           | OAuth client ID                                 |
| `client_secret`  | yes*     | —                           | OAuth client secret (*not needed with PKCE)     |
| `issuer_url`     | oidc     | —                           | OIDC discovery base URL                         |
| `redirect_url`   | no       | derived from `base_url`     | OAuth callback URL                              |
| `display_name`   | no       | `SSO`                       | Label on the login button (OIDC only)           |
| `allowed_domains`| no       | allow all                   | Restrict login to these email domains           |
| `scopes`         | no       | `openid`, `email`, `profile`| OIDC scopes to request                          |
| `pkce`           | no       | `false`                     | Enable PKCE (OIDC only)                         |
| `session_secret` | no       | derived from `encryption_key`| Key for signing session JWTs                   |
| `session_ttl`    | no       | `24h`                       | Session token lifetime                          |

## Domain restriction

Both providers support `allowed_domains` to restrict which email domains can log in. When set, only users with an email address matching one of the listed domains will be allowed to authenticate. An empty list (or omitting the field) allows all domains.
