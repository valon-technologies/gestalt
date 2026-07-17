# Changelog

## 0.0.1-alpha.2

### Breaking

- `InvokeError.code` (app envelope string slug) renamed to `InvokeError.reason`. Wire JSON is unchanged (`error.code` on the envelope).
- `InvokeError` now extends `GestaltError`. Use `error.code` for gRPC classification (`GestaltErrorCode.Unauthenticated`, etc.), not HTTP status or envelope slugs.

### Added

- `httpStatusToGestaltCode(status)` — map HTTP status to `GestaltErrorCode`.

### Migration

```ts
// Before
if (invokeError.code === "missing_credential") { ... }
if (invokeError.status === 401) { ... }

// After
if (invokeError.reason === "missing_credential") { ... }
if (invokeError.code === GestaltErrorCode.Unauthenticated) { ... }
```

```ts
// instanceof: check InvokeError first when narrowing invoke context
if (error instanceof InvokeError) {
  log({ reason: error.reason, app: error.app });
} else if (error instanceof GestaltError) {
  log({ code: error.code });
}
```
