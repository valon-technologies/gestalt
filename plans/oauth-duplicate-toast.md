# OAuth duplicate connection feedback

## Problem

When an OAuth connection is completed for an account that is already linked,
the UI only receives a generic connected signal and shows `${app} is connected.`
The connection setup code already knows whether the account was already linked,
but the callback success paths discarded that result.

## Decision

Preserve the existing server-side `AlreadyConnected` result through every OAuth
completion path. The popup success page sends it with the existing completion
message, visible fallback pages use the duplicate-specific copy, and same-tab/
fallback links include it as a query parameter. The client still confirms the
connection by refreshing the catalog before showing success; the flag only
selects the duplicate-specific copy.

## Acceptance criteria

- Duplicate OAuth completion communicates `${app} is already connected.`.
- New OAuth completion continues to communicate `${app} is connected.`.
- Popup and same-tab/fallback completion paths preserve the result.
- Existing manual connection and popup-close recovery behavior is unchanged.

## Validation

- `go test ./gestaltd/internal/server`
- Handler-level regression coverage must verify that a duplicate OAuth callback
  preserves `AlreadyConnected` in both the popup completion payload and the
  non-HTML redirect.
- Pending-connection selection must preserve the same result in both the
  authenticated redirect path and the unauthenticated fallback page.
- Client validation runs in the companion toolshed worktree.
- `git diff --check`

## Rollout and rollback

Deploy the backend commit before relying on the duplicate-specific popup copy.
The query and message fields are additive, so older clients continue to use the
generic success copy. Roll back by reverting this commit if needed.
