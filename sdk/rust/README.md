# Gestalt Rust SDK

This directory contains the Rust SDK for Gestalt executable providers.

## Quick start

The main integration-provider flow stays small:

```rust,no_run
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

#[derive(Default)]
struct EchoProvider;

#[gestalt::async_trait]
impl gestalt::Provider for EchoProvider {}

#[derive(Deserialize, JsonSchema)]
struct EchoInput {
    message: String,
}

#[derive(Serialize, JsonSchema)]
struct EchoOutput {
    echoed: String,
}

fn router() -> gestalt::Result<gestalt::Router<EchoProvider>> {
    gestalt::Router::new().register(
        gestalt::Operation::<EchoInput, EchoOutput>::new("echo")
            .description("Echo one message back to the caller"),
        |_: Arc<EchoProvider>, input, _request| async move {
            Ok::<_, gestalt::Error>(gestalt::ok(EchoOutput {
                echoed: input.message,
            }))
        },
    )
}

gestalt::export_provider!(constructor = EchoProvider::default, router = router);
```

Sibling provider surfaces stay source-native too:

```rust,no_run
# async fn example() -> Result<(), Box<dyn std::error::Error>> {
let mut cache = gestalt::Cache::connect().await?;
cache.set("token", b"abc123", Default::default()).await?;

let mut object = gestalt::S3::connect().await?.object("docs", "hello.txt");
object.write_string("hello", None).await?;
# Ok(())
# }
```

Current scope:

- standalone Cargo package published as `gestalt-sdk`
- library crate name remains `gestalt` in Rust code
- checked-in protobuf/gRPC bindings generated from `sdk/proto/v1/*.proto`
- maintainer-only stub generation via vendored `protoc`
- generated protocol bindings exposed via `proto::v1`
- typed integration-provider authoring helpers for requests, responses, catalogs, and routing
- authentication-provider, cache-provider, secrets-provider, and workflow-provider traits that map to the shared executable runtime protocol
- `Workflow` and `WorkflowHost` client helpers for plugin-side workflow control and workflow-provider host callbacks
- `S3` client helpers and the `S3Provider` trait for S3-compatible provider components
- runtime servers for the integration, authentication, cache, secrets, workflow, and S3 provider surfaces over the Unix socket exposed by `gestaltd`
- `export_provider!`, `export_authentication_provider!`, `export_cache_provider!`, `export_secrets_provider!`, `export_workflow_provider!`, and `export_s3_provider!` macros for source builds that let `gestaltd` synthesize the executable wrapper

## Codegen strategy

Bindings are checked into
[`src/generated`](https://github.com/valon-technologies/gestalt/tree/main/sdk/rust/src/generated)
so crate consumers do not need a protobuf toolchain when building
`gestalt-sdk`.

Maintainers regenerate them from the shared proto definitions in
[`sdk/proto`](https://github.com/valon-technologies/gestalt/tree/main/sdk/proto)
with a helper binary under
[`tools/rust-sdk-codegen`](https://github.com/valon-technologies/gestalt/tree/main/tools/rust-sdk-codegen),
which uses a vendored `protoc`.

To regenerate the bindings:

```bash
sdk/rust/scripts/generate_stubs.sh
```

## Public surface

The crate is intentionally small:

- `Provider`, `Request`, `Response`, and `ok(...)` model integration providers
- `AuthenticationProvider`, `BeginAuthenticationRequest`, `BeginAuthenticationResponse`, `CompleteAuthenticationRequest`, `AuthenticateRequest`, and `AuthenticatedUser` model authentication providers
- `Cache`, `CacheProvider`, `CacheEntry`, and `CacheSetOptions` model cache clients and providers
- `SecretsProvider` models secrets providers
- `Workflow`, `WorkflowHost`, and `WorkflowProvider` model workflow clients, host callbacks, and workflow base providers
- `S3`, `S3Provider`, and `gestalt::s3::*` model S3-compatible object-store clients and providers
- `Router` and `Operation` register typed operations and derive catalog metadata from `serde` + `schemars`
- `Catalog` types expose explicit static or session-scoped catalogs when needed
- `RuntimeMetadata` lets any provider kind describe its runtime name/display metadata and version
- `runtime` runs the integration, authentication, cache, secrets, workflow, or S3 gRPC servers, or writes the static catalog when `GESTALT_PLUGIN_WRITE_CATALOG` is set
- `export_provider!` exports `__gestalt_serve` and `__gestalt_write_catalog` for integration providers
- `export_authentication_provider!` exports `__gestalt_serve_authentication` for authentication providers
- `export_cache_provider!` exports `__gestalt_serve_cache` for cache providers
- `export_secrets_provider!` exports `__gestalt_serve_secrets` for secrets providers
- `export_workflow_provider!` exports `__gestalt_serve_workflow` for workflow providers
- `export_s3_provider!` exports `__gestalt_serve_s3` for S3 providers

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo workspace so the SDK can evolve independently of the CLI crate graph.
