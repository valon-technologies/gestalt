# Gestalt Rust SDK

This directory contains the Rust SDK for Gestalt executable providers.

Current scope:

- standalone Cargo package named `gestalt_plugin_sdk`
- build-time protobuf/gRPC generation from `sdk/proto/v1/*.proto`
- vendored `protoc` via `protoc-bin-vendored`
- generated protocol bindings exposed via `proto::v1`
- typed integration-provider authoring helpers for requests, responses, catalogs, and routing
- auth-provider and datastore-provider traits that map to the shared executable runtime protocol
- runtime servers for the integration, auth, and datastore provider surfaces over the Unix socket exposed by `gestaltd`
- `export_provider!`, `export_auth_provider!`, and `export_datastore_provider!` macros for source builds that let `gestaltd` synthesize the executable wrapper

## Codegen strategy

Bindings are generated during `cargo build` and `cargo test` by [`build.rs`](build.rs).
The build script compiles the shared proto definitions from [`sdk/proto`](../proto) using a vendored `protoc`, so maintainers do not need a system protobuf toolchain to work on the crate.

Generated Rust sources are not checked into git. They are emitted under Cargo's build output in `sdk/rust/target/` and should not be committed.

To regenerate the bindings from a clean slate and run the smoke tests:

```bash
./scripts/generate_stubs.sh
```

## Public surface

The crate is intentionally small:

- `Provider`, `Request`, `Response`, and `ok(...)` model integration-provider handlers
- `AuthProvider`, `BeginLoginRequest`, `BeginLoginResponse`, `CompleteLoginRequest`, and `AuthenticatedUser` model auth providers
- `DatastoreProvider`, `StoredUser`, `StoredIntegrationToken`, `StoredApiToken`, and `OAuthRegistration` model datastore providers
- `Router` and `Operation` register typed operations and derive catalog metadata from `serde` + `schemars`
- `Catalog` types expose explicit static or session-scoped catalogs when needed
- `RuntimeMetadata` lets any provider kind describe its runtime name/display metadata and version
- `runtime` runs the integration, auth, or datastore gRPC servers, or writes the static catalog when `GESTALT_PLUGIN_WRITE_CATALOG` is set
- `export_provider!` exports `__gestalt_serve` and `__gestalt_write_catalog` for integration providers
- `export_auth_provider!` exports `__gestalt_serve_auth` for auth providers
- `export_datastore_provider!` exports `__gestalt_serve_datastore` for datastore providers

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo workspace so the SDK can evolve independently of the CLI crate graph.
