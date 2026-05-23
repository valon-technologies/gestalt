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

## API sections

The docs.rs reference is organized around the provider-authoring workflow. Most
provider code imports from the crate root after renaming `gestalt-sdk` to
`gestalt`.

| Section | Start with | Use it for |
| --- | --- | --- |
| Provider authoring | [`Provider`], [`Operation`], [`Router`], [`Request`], [`HTTPSubjectRequest`], [`ConnectedToken`], [`Response`], [`ok`] | Executable app providers, typed request handlers, hosted HTTP subject hooks, post-connect metadata hooks, and operation results. |
| Catalog metadata | [`Catalog`], [`CatalogOperation`], [`Router::register`] | Schema-derived operation catalogs from `serde` and `schemars` types. |
| Provider runtimes | [`AuthenticationProvider`], [`CacheProvider`], [`S3Provider`], [`SecretsProvider`], [`WorkflowProvider`], [`AgentProvider`], [`AppRuntimeProvider`] | Host-service backends implemented as Rust providers. |
| Workflow and agent models | [`new_bound_workflow_target`], [`new_workflow_signal`], [`new_bound_workflow_run`], [`new_workflow_execution_reference`], [`new_agent_message`], [`new_agent_tool_ref`] | Native workflow values, agent messages, tool refs, and copy helpers. |
| Host-service clients | [`Cache`], [`S3`], [`WorkflowHost`], [`WorkflowManager`], [`AgentHost`], [`AgentManager`], [`AppInvoker`] | Calling sibling services exposed to a provider process by `gestaltd`. |
| Runtime and telemetry | [`runtime`], [`telemetry`], [`RuntimeMetadata`] | Provider process entrypoints and provider-authored GenAI spans and metrics. |

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
| `CacheProvider` | `export_cache_provider!` | App-bound cache storage. |
| `S3Provider` | `export_s3_provider!` | S3-compatible object storage. |
| `SecretsProvider` | `export_secrets_provider!` | Secret resolution. |
| `WorkflowProvider` | `export_workflow_provider!` | Workflow runs, schedules, and event triggers. |
| `AgentProvider` | `export_agent_provider!` | Agent sessions, turns, events, interactions, and capabilities. |
| `AppRuntimeProvider` | `export_app_runtime_provider!` | Hosted app execution backends. |

The crate also exposes clients for sibling host services, including `Cache`,
`S3`, `WorkflowHost`, `WorkflowManager`, `AgentHost`, `AgentManager`, and
`AppInvoker`.

`AgentProvider` implementations receive and return native structs such as
`CreateAgentProviderTurnRequest`, `AgentSession`, `AgentTurn`, and
`AgentTurnEvent`. Structured payload fields use `serde_json::Value` and
timestamp fields use `SystemTime`; the SDK runtime owns transport serialization at
the transport boundary. `AgentHost` includes plain-input helpers for listing
tools, executing tools, and resolving connections during one turn.
Workflow builders such as `new_bound_workflow_target`,
`new_workflow_signal`, `new_bound_workflow_run`, and
`new_workflow_execution_reference` accept SDK-owned input structs with
`serde::Serialize` payload setters and native `SystemTime` fields. Copy helpers
such as `new_bound_workflow_target_from_target` preserve request shape without
asking provider code to assemble transport objects.

## Public surface

The crate exposes higher-level authoring APIs:

- `Provider`, `Request`, `ConnectedToken`, `Response`, and `ok(...)` model
  integration providers.
- `HTTPSubjectRequest` and `Provider::resolve_http_subject` let executable
  apps map verified hosted HTTP requests to concrete subjects.
- `Router` and `Operation` register typed operations and derive catalog
  metadata from `serde` and `schemars`.
- `AuthenticationProvider`, `CacheProvider`, `S3Provider`, `SecretsProvider`,
  `WorkflowProvider`, `AgentProvider`, and `AppRuntimeProvider` model
  executable provider runtimes.
- `Cache`, `S3`, `WorkflowHost`, `WorkflowManager`, `AgentHost`,
  `AgentManager`, and `AppInvoker` call sibling host services.
- `RuntimeMetadata` lets provider runtimes describe their display metadata and
  version.
- Workflow builder inputs such as `BoundWorkflowTarget`, `WorkflowStep`,
  `WorkflowSignal`, and `BoundWorkflowRun` model provider-owned workflow
  state.

## Package layout

This package intentionally lives outside the existing `gestalt/` Cargo
workspace so the SDK can evolve independently of the CLI crate graph.
