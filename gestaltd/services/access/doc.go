// Package access is the shared gestaltd policy gate.
//
// It combines credential-scope checks from principals with authorization-model
// checks from core.AuthorizationProvider so callers can enforce a single
// principal, resource, action decision for app, workflow, agent, UI, and
// authorization-administration surfaces.
//
// Request names a resource/action and, when needed, the credential scope that
// must also be present on the principal. ScopeOnly requests are for token and
// catalog plumbing before a policy action is known.
//
// Allowed returns false without an error for scope or policy denial; use it
// for list/filter paths where denied and absent are equivalent. Require
// returns typed access errors for the same denials. A nil Enforcer or nil
// AuthorizationProvider is intentionally policy-allow, but request scopes are
// still enforced.
package access
