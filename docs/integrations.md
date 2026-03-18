# Integrations

Toolshed is a framework that exposes third-party APIs as tool operations for AI clients. You declare which integrations to load in `config.yaml`, and toolshed handles auth, spec parsing, and request execution.

Each integration resolves its definition through one of three sources:

1. **OpenAPI spec URL** (`openapi`) -- toolshed fetches the spec, extracts operations, and builds the integration at startup. Supports both Swagger 2.0 and OpenAPI 3.x.
2. **Local YAML provider file** (`provider`) -- point to a `.yaml` file on disk that defines the integration manually.
3. **Provider directories** (`provider_dirs`) -- if neither `openapi` nor `provider` is set, toolshed searches `provider_dirs` for `<name>.yaml`.

## Config format

Integrations live under the `integrations:` key as a map of name to `IntegrationDef`:

```yaml
integrations:
  <name>:
    # --- Source (pick one) ---
    openapi: ""          # URL to an OpenAPI/Swagger spec
    provider: ""         # Path to a local YAML provider file
    # If neither is set, toolshed searches provider_dirs for <name>.yaml

    # --- Credentials ---
    client_id: ""
    client_secret: ""
    redirect_url: ""
    base_url: ""         # Override the base URL from the spec/provider

    # --- Auth overrides (see below) ---
    auth: {}

    # --- Hooks ---
    response_check: ""
    token_parser: ""
    request_mutator: ""
    token_prefix: ""
    auth_style: ""       # bearer (default), raw, none
    headers: {}          # Extra headers added to every request

    # --- Operation filtering ---
    allowed_operations:  # nil = all operations; map = only these operationIds
      operationId: ""    # empty string = use spec description; non-empty = override
```

### Example: OpenAPI-driven integration with operation filtering

```yaml
integrations:
  slack:
    openapi: https://raw.githubusercontent.com/slackapi/slack-api-specs/master/web-api/slack_web_openapi_v2.json
    client_id: ${SLACK_CLIENT_ID}
    client_secret: ${SLACK_CLIENT_SECRET}
    redirect_url: ${SLACK_REDIRECT_URL}
    response_check: slack_ok
    auth:
      authorization_url: https://slack.com/oauth/v2/authorize
      token_url: https://slack.com/api/oauth.v2.access
      response_hook: slack_ok
    allowed_operations:
      conversations_list: "List channels the bot is a member of"
      chat_postMessage: ""   # use description from spec
      users_list: ""
      search_messages: ""
```

### Example: API-key integration (manual auth)

When there is no OAuth flow, set `auth.type: manual`. Users paste their credentials directly.

```yaml
integrations:
  datadog:
    auth:
      type: manual
    token_parser: datadog_keys
```

### Example: local YAML provider file

```yaml
integrations:
  internal_api:
    provider: /etc/toolshed/providers/internal_api.yaml
    client_id: ${INTERNAL_CLIENT_ID}
    client_secret: ${INTERNAL_CLIENT_SECRET}
    redirect_url: ${INTERNAL_REDIRECT_URL}
```

### Example: provider_dirs lookup

Place YAML files in a directory and reference it with `provider_dirs`. Toolshed searches for `<name>.yaml` when no `openapi` or `provider` is set.

```yaml
provider_dirs:
  - /etc/toolshed/providers

integrations:
  slack:
    client_id: ${SLACK_CLIENT_ID}
    client_secret: ${SLACK_CLIENT_SECRET}
    redirect_url: ${SLACK_REDIRECT_URL}
```

## allowed_operations

Controls which operations from the spec or provider file are exposed.

- **`nil` (omitted)** -- all operations from the source are exposed.
- **Map of `operationId: description`** -- only the listed operations are exposed.
  - Empty string value (`""`) uses the description from the spec/provider.
  - Non-empty string value overrides the description (useful for making spec descriptions more useful to AI clients).

```yaml
allowed_operations:
  conversations_list: "List Slack channels the bot belongs to"
  chat_postMessage: ""        # keep spec description
  conversations_history: ""
```

## Auth overrides

When an OpenAPI spec's OAuth URLs are wrong, incomplete, or missing, override them in config. Any field set in `auth:` is applied on top of whatever the spec or provider file declares.

```yaml
auth:
  type: oauth2                          # oauth2 or manual
  authorization_url: https://...        # full URL or path (appended to base_url)
  token_url: https://...                # full URL or path
  client_auth: header                   # body (default) or header
  token_exchange: json                  # form (default) or json
  scope_separator: ","                  # separator for scope strings (default is space)
  pkce: true                            # enable PKCE
  authorization_params:                 # extra query params on the authorize redirect
    team: T12345
  token_params:                         # extra params on the token exchange
    audience: https://api.example.com
  refresh_params:                       # extra params on token refresh
    scope: read write
  accept_header: application/json       # Accept header for token endpoint
  token_metadata:                       # fields to extract from token response
    - team_id
    - team_name
  response_hook: slack_ok               # named hook to validate token responses
```

When `authorization_url` or `token_url` is a relative path (no `http://` or `https://` prefix), it is resolved against `base_url`.

If `auth.type` is `manual` (or if both `type` and `authorization_url` are empty), no OAuth flow is configured and users provide credentials manually.

## Named hooks

Hooks customize request/response behavior for APIs with non-standard conventions. They are referenced by name in config and resolved from a compiled-in registry.

### response_check

Validates API responses beyond HTTP status codes.

| Name       | Behavior |
|------------|----------|
| `slack_ok` | Parses the JSON response body and checks the `ok` field. Returns the `error` field as an error if `ok` is false. |

### token_parser

Transforms the stored token into the actual auth credentials for API requests.

| Name           | Behavior |
|----------------|----------|
| `datadog_keys` | Parses a JSON token containing `api_key` and `app_key`, sets them as `DD-API-KEY` and `DD-APPLICATION-KEY` headers. |

Alternatively, use `token_prefix` for simple cases where the token just needs a prefix:

```yaml
token_prefix: "Token "
```

### request_mutator

Modifies outgoing requests before they are sent. Used for APIs where the OpenAPI spec does not fully describe the request format.

| Name              | Behavior |
|-------------------|----------|
| `gitlab`          | Adds `scope=blobs` parameter to `search_code` operations. |
| `gong`            | Rewrites `get_call_transcript` into the correct POST body format; converts snake_case date params to camelCase. |
| `google_calendar` | Defaults `calendar_id` to `"primary"` if not provided. |

### auth response_hook

Validates token endpoint responses (set inside `auth:`).

| Name       | Behavior |
|------------|----------|
| `slack_ok` | Checks the `ok` field in Slack's non-standard token response. |

## provider_dirs

Load provider YAML files from directories on disk.

```yaml
provider_dirs:
  - /etc/toolshed/providers
  - ./custom-providers
```

When an integration has no `openapi` or `provider` set, toolshed searches each directory in `provider_dirs` for `<name>.yaml`. If not found in any directory, startup fails with an error telling you to set `openapi` or `provider`.

## Writing a YAML provider

A provider file defines an integration's base URL, auth configuration, and operations. Place it in a `provider_dirs` directory or reference it directly with `provider:`.

### Format

```yaml
provider: my_service             # required, unique identifier
display_name: My Service
description: Short description for AI clients
base_url: https://api.myservice.com

auth:
  type: oauth2                   # oauth2 or manual
  authorization_url: /oauth/authorize
  token_url: /oauth/token

operations:
  list_items:
    description: List all items
    method: GET
    path: /api/v1/items
    parameters:
      - name: limit
        type: integer
        description: Maximum number of results
        default: 100
      - name: cursor
        type: string
        description: Pagination cursor

  create_item:
    description: Create a new item
    method: POST
    path: /api/v1/items
    parameters:
      - name: name
        type: string
        description: Item name
        required: true
      - name: tags
        type: string
        description: Comma-separated tags
```

### Parameter fields

| Field         | Type    | Description |
|---------------|---------|-------------|
| `name`        | string  | Parameter name (used in query string or request body) |
| `type`        | string  | `string`, `integer`, `boolean`, etc. |
| `description` | string  | Shown to AI clients |
| `required`    | bool    | Whether the parameter is required |
| `default`     | any     | Default value if not provided |
