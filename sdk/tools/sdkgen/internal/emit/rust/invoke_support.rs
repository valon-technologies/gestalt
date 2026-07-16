use serde_json::Value;

use crate::rpc_support::GestaltError;

/// Decoded app invocation payload failure: an HTTP-error status, an error
/// envelope, or an undecodable result body.
#[derive(Clone, Debug, thiserror::Error)]
#[error("{message}")]
pub struct InvokeResultError {
    /// The invoked app.
    pub app: String,
    /// The invoked operation.
    pub operation: String,
    /// The HTTP status when the result carried one.
    pub status: Option<u16>,
    /// The error envelope's code when present.
    pub code: Option<String>,
    /// The failure message.
    pub message: String,
    /// The decoded result body when it parsed as JSON.
    pub body: Option<Box<Value>>,
    /// The raw result body bytes.
    pub raw_body: Vec<u8>,
}

/// Failure of a `json_result` method: the transport failed, or the decoded
/// result carried a payload failure.
#[derive(Debug, thiserror::Error)]
pub enum InvokeError {
    /// The transport failed before a result decoded.
    #[error(transparent)]
    Transport(#[from] GestaltError),
    /// The decoded result carried a payload failure.
    #[error(transparent)]
    Result(#[from] Box<InvokeResultError>),
}

/// Decodes one app operation result with the standard JSON envelope
/// semantics: success envelopes return their data, error envelopes and
/// HTTP-error statuses return [`InvokeResultError`], and any other JSON body
/// passes through unchanged.
pub fn decode_app_result(
    app: &str,
    operation: &str,
    status: i32,
    body: &[u8],
) -> Result<Value, Box<InvokeResultError>> {
    let parsed = parse_json_result_body(body);
    if !is_success(status) {
        return Err(http_status_error(app, operation, status, body, parsed));
    }
    let parsed = match parsed {
        Ok(parsed) => parsed,
        Err(_) => {
            return Err(Box::new(InvokeResultError {
                app: app.to_string(),
                operation: operation.to_string(),
                status: None,
                code: None,
                message: "app invoke response is not valid JSON".to_string(),
                body: None,
                raw_body: body.to_vec(),
            }));
        }
    };
    if let Some(status_value) = parsed.get("status").and_then(Value::as_str) {
        if status_value == "error" {
            let mut error = InvokeResultError {
                app: app.to_string(),
                operation: operation.to_string(),
                status: None,
                code: None,
                message: "app invoke failed".to_string(),
                body: None,
                raw_body: body.to_vec(),
            };
            apply_invoke_error_fields(&mut error, &parsed);
            error.body = Some(Box::new(parsed));
            return Err(Box::new(error));
        }
        if status_value == "success" {
            if let Some(data) = parsed.get("data") {
                return Ok(data.clone());
            }
        }
    }
    Ok(parsed)
}

/// Decodes one GraphQL invocation result like [`decode_app_result`] and
/// additionally fails when the response carries a non-empty GraphQL `errors`
/// array.
pub fn decode_graphql_result(
    app: &str,
    status: i32,
    body: &[u8],
) -> Result<Value, Box<InvokeResultError>> {
    let decoded = decode_app_result(app, "graphql", status, body)?;
    if let Ok(raw) = parse_json_result_body(body) {
        graphql_errors(app, body, &raw)?;
    }
    graphql_errors(app, body, &decoded)?;
    Ok(decoded)
}

/// Reports whether an HTTP status code is a success (200-299), mirroring
/// reqwest's `StatusCode::is_success`.
pub fn is_success(status: i32) -> bool {
    (200..=299).contains(&status)
}

/// Returns the payload failure a non-success HTTP status decodes to,
/// mirroring reqwest's `Response::error_for_status`; success statuses return
/// Ok. The error carries exactly what [`decode_app_result`] would attach for
/// the same status and body.
pub fn error_for_status(
    app: &str,
    operation: &str,
    status: i32,
    body: &[u8],
) -> Result<(), Box<InvokeResultError>> {
    if is_success(status) {
        return Ok(());
    }
    Err(http_status_error(
        app,
        operation,
        status,
        body,
        parse_json_result_body(body),
    ))
}

/// Builds the payload failure for an HTTP-error status: the raw body always
/// rides along, and a JSON body additionally carries its parsed form and
/// error envelope fields.
fn http_status_error(
    app: &str,
    operation: &str,
    status: i32,
    body: &[u8],
    parsed: Result<Value, serde_json::Error>,
) -> Box<InvokeResultError> {
    let mut error = InvokeResultError {
        app: app.to_string(),
        operation: operation.to_string(),
        status: u16::try_from(status).ok(),
        code: None,
        message: format!("app invoke failed with status {status}"),
        body: None,
        raw_body: body.to_vec(),
    };
    if let Ok(parsed) = parsed {
        apply_invoke_error_fields(&mut error, &parsed);
        error.body = Some(Box::new(parsed));
    }
    Box::new(error)
}

fn parse_json_result_body(body: &[u8]) -> Result<Value, serde_json::Error> {
    if body.iter().all(u8::is_ascii_whitespace) {
        return Ok(serde_json::json!({}));
    }
    serde_json::from_slice(body)
}

fn graphql_errors(app: &str, raw_body: &[u8], value: &Value) -> Result<(), Box<InvokeResultError>> {
    let Some(errors) = value.get("errors").and_then(Value::as_array) else {
        return Ok(());
    };
    if errors.is_empty() {
        return Ok(());
    }
    let message = errors
        .first()
        .and_then(|first| first.get("message"))
        .and_then(Value::as_str)
        .filter(|text| !text.trim().is_empty())
        .unwrap_or("GraphQL returned errors")
        .to_string();
    Err(Box::new(InvokeResultError {
        app: app.to_string(),
        operation: "graphql".to_string(),
        status: None,
        code: Some("graphql_errors".to_string()),
        message,
        body: Some(Box::new(value.clone())),
        raw_body: raw_body.to_vec(),
    }))
}

fn apply_invoke_error_fields(error: &mut InvokeResultError, parsed: &Value) {
    let nested = parsed.get("error");
    let nested_message = nested
        .and_then(|value| value.get("message"))
        .and_then(Value::as_str)
        .filter(|text| !text.trim().is_empty());
    let nested_code = nested
        .and_then(|value| value.get("code"))
        .and_then(Value::as_str)
        .filter(|text| !text.trim().is_empty());
    let top_message = parsed
        .get("message")
        .and_then(Value::as_str)
        .filter(|text| !text.trim().is_empty());
    let top_code = parsed
        .get("code")
        .and_then(Value::as_str)
        .filter(|text| !text.trim().is_empty());
    if let Some(message) = nested_message.or(top_message) {
        error.message = message.to_string();
    }
    if let Some(code) = nested_code.or(top_code) {
        error.code = Some(code.to_string());
    }
}
