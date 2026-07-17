# Changelog

## 0.0.1-alpha.2

### Breaking changes

- `InvokeError.code` is removed. Operation-envelope failures now expose
  `InvokeError.reason` (the wire `error.code` value) alongside the inherited
  `GestaltError.code` transport classification.

### Added

- `bindApp(client, app)` returns an app-scoped client that accepts only
  `{ operation, params?, ... }` invoke requests.
- Web public request types use sparse `Init<T>` construction so transport
  metadata fields are optional on invoke and GraphQL requests.

### Fixed

- Sparse JSON sanitization before protobuf Struct encoding.
- Hardened v2 session cookie lifting when `Authorization` is malformed.
- Documented Vite proxy-token behavior for local development.
