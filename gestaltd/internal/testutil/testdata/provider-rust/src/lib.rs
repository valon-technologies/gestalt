use std::sync::{Arc, Mutex};

use gestalt_plugin_sdk as gestalt;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Map as JsonMap, Value as JsonValue};

pub struct Provider {
    greeting: Mutex<String>,
}

impl Default for Provider {
    fn default() -> Self {
        Self {
            greeting: Mutex::new("Hello".to_string()),
        }
    }
}

#[gestalt::async_trait]
impl gestalt::Provider for Provider {
    async fn configure(&self, _name: &str, config: JsonMap<String, JsonValue>) -> gestalt::Result<()> {
        let greeting = config
            .get("greeting")
            .and_then(JsonValue::as_str)
            .unwrap_or("Hello")
            .to_string();
        *self.greeting.lock().expect("lock greeting") = greeting;
        Ok(())
    }
}

#[derive(Deserialize, JsonSchema)]
struct GreetInput {
    /// Name to greet.
    name: Option<String>,
}

#[derive(Serialize, JsonSchema)]
struct GreetOutput {
    message: String,
}

async fn greet(
    provider: Arc<Provider>,
    input: GreetInput,
    _request: gestalt::Request,
) -> gestalt::Result<gestalt::Response<GreetOutput>> {
    let greeting = provider.greeting.lock().expect("lock greeting").clone();
    let name = input.name.unwrap_or_else(|| "World".to_string());
    Ok(gestalt::ok(GreetOutput {
        message: format!("{greeting}, {name}!"),
    }))
}

fn new() -> Provider {
    Provider::default()
}

fn router() -> gestalt::Result<gestalt::Router<Provider>> {
    gestalt::Router::new()
        .register(
            gestalt::Operation::<GreetInput, GreetOutput>::new("greet")
                .method("GET")
                .description("Return a greeting message")
                .read_only(true),
            greet,
        )
}

gestalt::export_provider!(constructor = new, router = router);
