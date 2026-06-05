// Package access is the shared gestaltd policy gate.
//
// It combines credential-scope checks from principals with authorization-model
// checks from core.AuthorizationProvider so callers can enforce a single
// principal, resource, action decision for app, workflow, agent, UI, and
// authorization-administration surfaces.
//
// AppOperation and Provider requests include credential-scope requirements.
// ProviderScope is scope-only for token and catalog plumbing before an
// operation is known. A nil Enforcer or nil AuthorizationProvider is
// intentionally policy-allow, but request scopes are still enforced.
package access
