// Package access is the shared gestaltd policy gate.
//
// It combines credential-scope checks from principals with authorization-model
// checks from core.AuthorizationProvider so callers can enforce a single
// principal, resource, action decision for app, workflow, agent, UI, and
// authorization-administration surfaces.
//
// AppOperation and Provider requests include credential-scope requirements.
// ProviderScope is scope-only for token and catalog plumbing before an
// operation is known. AppOperationPolicyOnly deliberately skips credential
// scope for caller-app delegation while still checking model policy.
//
// Allowed returns false without an error for scope or policy denial; use it
// for list/filter paths where denied and absent are equivalent. Require
// returns typed access errors for the same denials. A nil Enforcer or nil
// AuthorizationProvider is intentionally policy-allow, but request scopes are
// still enforced.
package access
