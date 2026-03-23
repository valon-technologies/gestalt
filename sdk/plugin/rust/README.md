# Gestalt Plugin SDK for Rust

This is a thin helper for implementing a Gestalt subprocess plugin in Rust.

```rust
use gestalt_plugin::{Capabilities, ExecuteRequest, ExecuteResult, Plugin, PluginInfo, ProviderManifest, OperationDef};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let plugin = Plugin::new(
        PluginInfo { name: "loan-enrichment".into(), version: "0.1.0".into() },
        ProviderManifest {
            display_name: "Loan Enrichment".into(),
            description: "Fetches enrichment data for a loan".into(),
            connection_mode: "user".into(),
            operations: vec![OperationDef {
                name: "enrich_loan".into(),
                description: "Fetch enrichment data".into(),
                method: "POST".into(),
                parameters: vec![],
            }],
            catalog: None,
            auth: None,
        },
        |request: ExecuteRequest| -> Result<ExecuteResult, gestalt_plugin::PluginError> {
            Ok(ExecuteResult { status: 200, body: format!("{{\"operation\":\"{}\"}}", request.operation) })
        },
    );

    plugin.serve()
}
```

The SDK reads framed JSON-RPC messages from stdin and writes responses to
stdout. Use stderr for logs.

