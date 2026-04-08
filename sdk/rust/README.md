# Gestalt Rust SDK

This directory contains the Rust SDK for Gestalt executable provider plugins.

Current scope:

- standalone Cargo package named `gestalt_plugin_sdk`
- build-time protobuf/gRPC generation from `sdk/proto/v1/*.proto`
- vendored `protoc` via `protoc-bin-vendored`
- generated protocol bindings exposed via `proto::v1`
- typed provider authoring helpers for requests, responses, catalogs, and routing
- a runtime server that speaks the executable provider protocol over the Unix socket exposed by `gestaltd`
- an `export_provider!` macro for source-plugin builds that lets `gestaltd` synthesize the executable wrapper

Out of scope for this branch:

- auth/datastore SDK ergonomics beyond generated protocol bindings

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

- `Provider`, `Request`, `Response`, and `ok(...)` model provider handlers
- `Router` and `Operation` register typed operations and derive catalog metadata from `serde` + `schemars`
- `Catalog` types expose explicit static or session-scoped catalogs when needed
- `runtime` runs the gRPC plugin server or writes the static catalog when `GESTALT_PLUGIN_WRITE_CATALOG` is set
- `export_provider!` exports the `__gestalt_serve` and `__gestalt_write_catalog` functions that the host-side Rust wrapper calls

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo workspace so the SDK can evolve independently of the CLI crate graph.
