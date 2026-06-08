# Breaking Changes

## Typed app invoke

`invoke` and `invoke_graphql` now decode app invocation responses and return the decoded JSON payload directly. Success envelopes shaped like `{ "status": "success", "data": ... }` return `data`; error envelopes and transport failures raise `InvokeError`.

Raw transport callers should use the new raw methods:

- Go: `InvokeRaw`, `InvokeGraphQLRaw`
- TypeScript: `invokeRaw`, `invokeGraphQLRaw`
- Python: `invoke_raw`, `invoke_graphql_raw`
- Rust: `invoke_raw`, `invoke_graphql_raw`

Raw operation results still expose JSON helpers for callers that need custom classification.
