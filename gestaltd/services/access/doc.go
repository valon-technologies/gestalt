// Package access is the shared gestaltd policy gate.
//
// Request names one resource/action and optional credential scope. An empty
// action enforces scope only. Require returns typed access errors; Allowed
// returns false without an error for denials in list/filter paths. A nil
// Enforcer means no policy provider is configured and policy checks allow, but
// credential scopes are still enforced.
package access
