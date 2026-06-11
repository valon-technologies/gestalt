//! Handwritten GraphQL invoke convenience over the generated pieces.
//!
//! GraphQL invocation deliberately stays out of the `json_result` annotation
//! vocabulary: the generated `App::invoke_graphql` returns the raw operation
//! result, and the generated invoke support exposes the envelope-plus-errors
//! decoding. This helper keeps the one ergonomic step the deleted facade used
//! to provide.

use serde_json::{Map, Value};

use crate::app::App;
use crate::invoke_support::{InvokeError, InvokeResultError, decode_graphql_result};

/// Options for the [`invoke_graphql`] helper: the optional invocation
/// targeting fields of the generated `App::invoke_graphql` surface.
#[derive(Clone, Debug, Default)]
pub struct InvokeGraphQLOptions {
    /// Connected account id or name to invoke against.
    pub connection: String,
    /// Provider instance id or name to invoke against.
    pub instance: String,
    /// Idempotency key forwarded to the target operation.
    pub idempotency_key: String,
    /// GraphQL variables for the document.
    pub variables: Option<Map<String, Value>>,
}

/// Invokes the GraphQL surface of another app through the generated
/// [`App`] client's `invoke_graphql` method and decodes the JSON result like
/// [`decode_graphql_result`], failing with [`InvokeError`] when the response
/// carries a GraphQL `errors` array. GraphQL invocation stays a helper by
/// design: the generated method returns the raw result.
pub async fn invoke_graphql(
    client: &mut App,
    app: &str,
    document: &str,
    options: InvokeGraphQLOptions,
) -> Result<Value, InvokeError> {
    let document = document.trim();
    if document.is_empty() {
        return Err(Box::new(InvokeResultError {
            app: app.to_string(),
            operation: "graphql".to_string(),
            status: None,
            code: None,
            message: "graphql document is required".to_string(),
            body: None,
            raw_body: Vec::new(),
        })
        .into());
    }
    let result = client
        .invoke_graphql(
            app.to_string(),
            document.to_string(),
            options.connection,
            options.instance,
            options.idempotency_key.trim().to_string(),
            options.variables,
        )
        .await?;
    Ok(decode_graphql_result(app, result.status, &result.body)?)
}
