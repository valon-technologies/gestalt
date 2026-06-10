# Breaking Changes

## Provider-minted agent session ids

`CreateAgentProviderSessionRequest` no longer carries a host-supplied session id. The provider mints a non-empty, stable session id and returns it on the created `AgentSession`; the host treats it as opaque. Session creation must be idempotent on `idempotency_key` scoped per subject (`created_by_subject_id`): replaying a key the provider has already honored for the same subject returns the existing session, including its persisted metadata, instead of creating a new one. Keys from different subjects never collide, and an empty key always creates a new session.

Affected surfaces:

- Go: `CreateAgentProviderSessionRequest.SessionID` removed
- TypeScript: `CreateAgentProviderSessionRequest.sessionId` removed
- Python: `CreateAgentProviderSessionRequest.session_id` removed
- Rust: `CreateAgentProviderSessionRequest.session_id` removed

Get, update, and turn requests are unchanged and continue to reference sessions by the provider-minted id.

## Typed app invoke

`invoke` and `invoke_graphql` now decode app invocation responses and return the decoded JSON payload directly. Success envelopes shaped like `{ "status": "success", "data": ... }` return `data`; error envelopes and transport failures raise `InvokeError`.

Raw transport callers should use the new raw methods:

- Go: `InvokeRaw`, `InvokeGraphQLRaw`
- TypeScript: `invokeRaw`, `invokeGraphQLRaw`
- Python: `invoke_raw`, `invoke_graphql_raw`
- Rust: `invoke_raw`, `invoke_graphql_raw`

Raw operation results still expose JSON helpers for callers that need custom classification.
