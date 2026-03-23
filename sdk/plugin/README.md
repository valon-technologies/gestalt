# Gestalt Plugin SDKs

These SDK shims implement the subprocess plugin protocol used by Gestalt for
custom code-based providers.

The transport is JSON-RPC 2.0 over stdio with LSP-style `Content-Length`
framing. Gestalt starts the plugin process, sends an `initialize` request, and
then dispatches `provider.execute` and optional auth methods.

Protocol shape:

- `initialize` request: `protocolVersion`, `hostInfo`, `integration{name, config}`
- `initialize` result: `protocolVersion`, `pluginInfo{name, version}`, `provider{displayName, description, connectionMode, operations, catalog?, auth?}`, `capabilities{catalog?, oauth?, manualAuth?, cancellation?}`
- `provider.execute` request: `operation`, `params`, `token`, `meta?`
- `provider.execute` result: `status`, `body`
- `auth.start` result: `authUrl`, `verifier?`
- `auth.exchange_code` and `auth.refresh_token` results: `accessToken`, `refreshToken?`, `expiresIn?`, `tokenType?`

Each language directory contains a thin runtime helper and a README with a
minimal example.

