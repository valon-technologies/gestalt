//! Gestalt public transport errors.

use crate::rpc_support::{GestaltError, gestalt_error_code};

/// Header name that marks OperationResult passthrough responses.
pub const RESPONSE_KIND_HEADER: &str = "X-Gestalt-Response-Kind";
/// Header value indicating the body is an OperationResult envelope.
pub const RESPONSE_KIND_OPERATION_RESULT: &str = "operation-result";

/// grpc-gateway style REST error response.
#[derive(Debug)]
pub struct GatewayError {
    /// Gestalt error code when known.
    pub code: i32,
    /// Human-readable error message.
    pub message: String,
    /// HTTP status code from the gateway.
    pub status: u16,
    /// Raw response body bytes.
    pub body: Vec<u8>,
}

impl std::fmt::Display for GatewayError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.message)
    }
}

impl std::error::Error for GatewayError {}

impl From<GatewayError> for GestaltError {
    fn from(value: GatewayError) -> Self {
        GestaltError::new(value.code, value.message)
    }
}

/// Reports whether headers mark an OperationResult passthrough.
pub fn is_operation_result(headers: &http::HeaderMap) -> bool {
    headers
        .get(RESPONSE_KIND_HEADER)
        .and_then(|value| value.to_str().ok())
        == Some(RESPONSE_KIND_OPERATION_RESULT)
}

/// Parses a non-operation-result REST error body when possible.
pub fn parse_gateway_error(status: u16, body: &[u8]) -> GatewayError {
    let mut code = http_status_to_gestalt_code(status);
    let mut message = format!("request failed with status {status}");
    if !body.is_empty() {
        if let Ok(payload) = serde_json::from_slice::<serde_json::Value>(body) {
            if let Some(text) = payload.get("message").and_then(|value| value.as_str()) {
                if !text.trim().is_empty() {
                    message = text.to_string();
                }
            }
            if let Some(value) = payload.get("code").and_then(|value| value.as_i64()) {
                code = value as i32;
            }
        }
    }
    GatewayError {
        code,
        message,
        status,
        body: body.to_vec(),
    }
}

fn http_status_to_gestalt_code(status: u16) -> i32 {
    match status {
        400 => gestalt_error_code::INVALID_ARGUMENT,
        401 => gestalt_error_code::UNAUTHENTICATED,
        403 => gestalt_error_code::PERMISSION_DENIED,
        404 => gestalt_error_code::NOT_FOUND,
        409 => gestalt_error_code::ALREADY_EXISTS,
        412 => gestalt_error_code::FAILED_PRECONDITION,
        429 => gestalt_error_code::RESOURCE_EXHAUSTED,
        499 => gestalt_error_code::CANCELLED,
        500 => gestalt_error_code::INTERNAL,
        501 => gestalt_error_code::UNIMPLEMENTED,
        503 => gestalt_error_code::UNAVAILABLE,
        504 => gestalt_error_code::DEADLINE_EXCEEDED,
        _ => gestalt_error_code::UNKNOWN,
    }
}
