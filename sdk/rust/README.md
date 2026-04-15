# Gestalt Rust SDK

This directory contains the Rust SDK for Gestalt executable providers.

Current scope:

- standalone Cargo package published as `gestalt-sdk`
- library crate name remains `gestalt` in Rust code
- checked-in protobuf/gRPC bindings generated from `sdk/proto/v1/*.proto`
- maintainer-only stub generation via vendored `protoc`
- generated protocol bindings exposed via `proto::v1`
- typed integration-provider authoring helpers for requests, responses, catalogs, and routing
- auth-provider, cache-provider, and secrets-provider traits that map to the shared executable runtime protocol
- `S3` client helpers and the `S3Provider` trait for S3-compatible provider components
- runtime servers for the integration, auth, cache, secrets, and S3 provider surfaces over the Unix socket exposed by `gestaltd`
- `export_provider!`, `export_auth_provider!`, `export_cache_provider!`, `export_secrets_provider!`, and `export_s3_provider!` macros for source builds that let `gestaltd` synthesize the executable wrapper

## Codegen strategy

Bindings are checked into [`src/generated`](src/generated) so crate consumers do
not need a protobuf toolchain when building `gestalt-sdk`.

Maintainers regenerate them from the shared proto definitions in
[`sdk/proto`](../proto) with a helper binary under `tools/rust-sdk-codegen/`,
which uses a vendored `protoc`.

To regenerate the bindings:

```bash
sdk/rust/scripts/generate_stubs.sh
```

## Public surface

The crate is intentionally small:

- `Provider`, `Request`, `Response`, and `ok(...)` model integration providers
- `AuthProvider`, `BeginLoginRequest`, `BeginLoginResponse`, `CompleteLoginRequest`, and `AuthenticatedUser` model auth providers
- `Cache`, `CacheProvider`, `CacheEntry`, and `CacheSetOptions` model cache clients and providers
- `SecretsProvider` models secrets providers
- `S3`, `S3Provider`, and `gestalt::s3::*` model S3-compatible object-store clients and providers
- `Router` and `Operation` register typed operations and derive catalog metadata from `serde` + `schemars`
- `Catalog` types expose explicit static or session-scoped catalogs when needed
- `RuntimeMetadata` lets any provider kind describe its runtime name/display metadata and version
- `runtime` runs the integration, auth, cache, secrets, or S3 gRPC servers, or writes the static catalog when `GESTALT_PLUGIN_WRITE_CATALOG` is set
- `export_provider!` exports `__gestalt_serve` and `__gestalt_write_catalog` for integration providers
- `export_auth_provider!` exports `__gestalt_serve_auth` for auth providers
- `export_cache_provider!` exports `__gestalt_serve_cache` for cache providers
- `export_secrets_provider!` exports `__gestalt_serve_secrets` for secrets providers
- `export_s3_provider!` exports `__gestalt_serve_s3` for S3 providers

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo workspace so the SDK can evolve independently of the CLI crate graph.
