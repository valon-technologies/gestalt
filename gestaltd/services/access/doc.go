// Package access is the shared gestaltd policy gate.
//
// It combines credential-scope checks from principals with authorization-model
// checks from core.AuthorizationProvider so callers can enforce a single
// principal, resource, action decision for app, workflow, agent, UI, and
// authorization-administration surfaces.
//
// A nil Enforcer or nil AuthorizationProvider is intentionally scope-only:
// policy checks allow, while RequireProviderScope and RequireOperationScope
// still enforce credential scope.
package access
