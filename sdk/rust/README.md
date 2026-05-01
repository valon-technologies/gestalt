# Gestalt Rust SDK

Use the Rust SDK to build compiled Gestalt providers with typed routers,
`serde` input and output types, and schema-derived catalog metadata.

The package is published to crates.io as `gestalt-sdk`, while Rust code imports
the crate as `gestalt`.

```sh
cargo add gestalt-sdk --rename gestalt
cargo add serde --features derive
cargo add schemars --features derive
```

## Quick start

Register typed operations on a router. The router dispatches requests and emits
catalog metadata for `gestaltd`.

```rust,no_run
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

#[derive(Default)]
struct SearchProvider;

#[gestalt::async_trait]
impl gestalt::Provider for SearchProvider {}

#[derive(Deserialize, JsonSchema)]
struct SearchInput {
    #[schemars(description = "Search query")]
    query: String,
}

#[derive(Serialize, JsonSchema)]
struct SearchOutput {
    results: Vec<String>,
}

fn router() -> gestalt::Result<gestalt::Router<SearchProvider>> {
    gestalt::Router::new().register(
        gestalt::Operation::<SearchInput, SearchOutput>::new("search")
            .method("GET")
            .title("Search"),
        |_: Arc<SearchProvider>, input, _request| async move {
            Ok::<_, gestalt::Error>(gestalt::ok(SearchOutput {
                results: vec![input.query],
            }))
        },
    )
}

gestalt::export_provider!(constructor = SearchProvider::default, router = router);
```

Fields are required unless they are modeled as `Option<T>` or have a serde
default. Use `schemars` attributes for catalog descriptions.

## Provider surfaces

Use `Provider`, `Operation`, `Router`, and `export_provider!` for integration
providers. Use the other provider traits and export macros when you are serving
a host-service backend.

| Trait | Export macro | Use it when you want to serve |
| --- | --- | --- |
| `AuthenticationProvider` | `export_authentication_provider!` | Login flows. |
| `CacheProvider` | `export_cache_provider!` | Plugin-bound cache storage. |
| `S3Provider` | `export_s3_provider!` | S3-compatible object storage. |
| `SecretsProvider` | `export_secrets_provider!` | Secret resolution. |
| `WorkflowProvider` | `export_workflow_provider!` | Workflow runs, schedules, and event triggers. |
| `AgentProvider` | `export_agent_provider!` | Agent sessions, turns, events, interactions, and capabilities. |
| `PluginRuntimeProvider` | `export_plugin_runtime_provider!` | Hosted plugin execution backends. |

The crate also exposes clients for sibling host services, including `Cache`,
`S3`, `WorkflowHost`, `WorkflowManager`, `AgentHost`, `AgentManager`, and
`PluginInvoker`.

## Codegen strategy

Bindings are checked into
[`src/generated`](https://github.com/valon-technologies/gestalt/tree/main/sdk/rust/src/generated)
so crate consumers do not need a protobuf toolchain when building
`gestalt-sdk`.

Maintainers regenerate them from the shared proto definitions in
[`sdk/proto`](https://github.com/valon-technologies/gestalt/tree/main/sdk/proto)
with the Buf template in
[`sdk/proto/buf.rust.gen.yaml`](https://github.com/valon-technologies/gestalt/tree/main/sdk/proto/buf.rust.gen.yaml).
Use the same Buf CLI version as CI for deterministic remote-plugin output.

To regenerate the bindings:

```bash
sdk/rust/scripts/generate_stubs.sh
```

## Public surface

The crate keeps generated bindings behind a higher-level authoring API:

- `Provider`, `Request`, `Response`, and `ok(...)` model integration
  providers.
- `Router` and `Operation` register typed operations and derive catalog
  metadata from `serde` and `schemars`.
- `AuthenticationProvider`, `CacheProvider`, `S3Provider`, `SecretsProvider`,
  `WorkflowProvider`, `AgentProvider`, and `PluginRuntimeProvider` model
  executable provider runtimes.
- `Cache`, `S3`, `WorkflowHost`, `WorkflowManager`, `AgentHost`,
  `AgentManager`, and `PluginInvoker` call sibling host services.
- `RuntimeMetadata` lets provider runtimes describe their display metadata and
  version.
- `runtime` contains entrypoints for serving provider surfaces over the Unix
  socket exposed by `gestaltd`.
- `proto::v1` exposes generated protocol bindings for low-level integration
  work.

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo
workspace so the SDK can evolve independently of the CLI crate graph.
