# ADR-004: Invocation Principal

## Status

Accepted

## Context

Invocations can originate from API requests, bindings, or runtimes. The broker needs to know both who the caller is (user identity) and where the call came from (source). Additionally, some providers use org-level credentials rather than per-user tokens, requiring a way to store and resolve identity-scoped credentials.

## Decisions

### Principal shape

Extend the existing `principal.Principal` struct with two fields:

- `CallSource` — "api", "binding", or "runtime"
- `CallSourceName` — the name of the binding or runtime, empty for api

This avoids a new type and keeps the principal as a single context value.

### Identity credential source

Use the sentinel user ID `__identity__` (defined as `principal.IdentityPrincipal`) to store and look up identity-scoped tokens in the existing datastore. Providers with `ConnectionModeIdentity` resolve tokens via this sentinel. Providers with `ConnectionModeEither` try the caller's user ID first, then fall back to the identity sentinel.
