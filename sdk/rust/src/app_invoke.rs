//! Handwritten GraphQL invoke convenience over the generated pieces.
//!
//! GraphQL invocation deliberately stays out of the `json_result` annotation
//! vocabulary: the generated `App::invoke_graphql` returns the raw operation
//! result, and the generated invoke support exposes the envelope-plus-errors
//! decoding. This helper keeps the one ergonomic step the deleted facade used
//! to provide.

use serde_json::Value;

use crate::app::{App, AppInvokeGraphQLOptions};
use crate::invoke_support::{InvokeError, InvokeResultError, decode_graphql_result};

/// Invokes the GraphQL surface of another app through the generated
/// [`App`] client's `invoke_graphql` method and decodes the JSON result like
/// [`decode_graphql_result`], failing with [`InvokeError`] when the response
/// carries a GraphQL `errors` array. GraphQL invocation stays a helper by
/// design: the generated method returns the raw result.
pub async fn invoke_graphql(
    client: &mut App,
    app: &str,
    document: &str,
    mut options: AppInvokeGraphQLOptions,
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
    options.idempotency_key = options.idempotency_key.trim().to_string();
    let result = client
        .invoke_graphql(app.to_string(), document.to_string(), options)
        .await?;
    Ok(decode_graphql_result(app, result.status, &result.body)?)
}
