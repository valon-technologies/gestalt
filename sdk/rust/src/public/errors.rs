//! Gestalt public transport errors.

use crate::rpc_support::{GestaltError, gestalt_error_code, http_status_to_gestalt_code};

/// Header name that marks OperationResult passthrough responses.
pub const RESPONSE_KIND_HEADER: &str = "X-Gestalt-Response-Kind";
/// Header value indicating the body is an OperationResult envelope.
pub const RESPONSE_KIND_OPERATION_RESULT: &str = "operation-result";

/// Reports whether headers mark an OperationResult passthrough.
pub fn is_operation_result(headers: &http::HeaderMap) -> bool {
    headers
        .get(RESPONSE_KIND_HEADER)
        .and_then(|value| value.to_str().ok())
        == Some(RESPONSE_KIND_OPERATION_RESULT)
}

/// Parses a non-operation-result REST error body when possible.
pub fn parse_gateway_error(status: u16, body: &[u8]) -> GestaltError {
    let parsed = if body.is_empty() {
        None
    } else {
        serde_json::from_slice::<serde_json::Value>(body).ok()
    };
    let code = resolve_gateway_error_code(status, parsed.as_ref());
    let message = parsed
        .as_ref()
        .and_then(extract_gateway_error_message)
        .unwrap_or_else(|| format!("request failed with status {status}"));
    GestaltError::new(code, message)
}

fn extract_gateway_error_message(parsed: &serde_json::Value) -> Option<String> {
    for key in ["message", "error"] {
        if let Some(text) = parsed.get(key).and_then(|value| value.as_str()) {
            let text = text.trim();
            if !text.is_empty() {
                return Some(text.to_string());
            }
        }
    }
    None
}

fn resolve_gateway_error_code(status: u16, parsed: Option<&serde_json::Value>) -> i32 {
    let status = status as i32;
    let Some(parsed) = parsed else {
        return http_status_to_gestalt_code(status);
    };
    match parsed.get("code") {
        Some(serde_json::Value::Number(number)) => number
            .as_i64()
            .and_then(|value| i32::try_from(value).ok())
            .unwrap_or_else(|| http_status_to_gestalt_code(status)),
        Some(serde_json::Value::String(name)) => grpc_code_name_to_gestalt_code(name)
            .unwrap_or_else(|| http_status_to_gestalt_code(status)),
        _ => http_status_to_gestalt_code(status),
    }
}

fn grpc_code_name_to_gestalt_code(name: &str) -> Option<i32> {
    Some(match name {
        "Canceled" => gestalt_error_code::CANCELLED,
        "Unknown" => gestalt_error_code::UNKNOWN,
        "InvalidArgument" => gestalt_error_code::INVALID_ARGUMENT,
        "DeadlineExceeded" => gestalt_error_code::DEADLINE_EXCEEDED,
        "NotFound" => gestalt_error_code::NOT_FOUND,
        "AlreadyExists" => gestalt_error_code::ALREADY_EXISTS,
        "PermissionDenied" => gestalt_error_code::PERMISSION_DENIED,
        "ResourceExhausted" => gestalt_error_code::RESOURCE_EXHAUSTED,
        "FailedPrecondition" => gestalt_error_code::FAILED_PRECONDITION,
        "Aborted" => gestalt_error_code::ABORTED,
        "OutOfRange" => gestalt_error_code::OUT_OF_RANGE,
        "Unimplemented" => gestalt_error_code::UNIMPLEMENTED,
        "Internal" => gestalt_error_code::INTERNAL,
        "Unavailable" => gestalt_error_code::UNAVAILABLE,
        "DataLoss" => gestalt_error_code::DATA_LOSS,
        "Unauthenticated" => gestalt_error_code::UNAUTHENTICATED,
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_gateway_error_field_and_string_code() {
        let body = br#"{"code":"Unauthenticated","error":"Not authenticated"}"#;
        let err = parse_gateway_error(401, body);
        assert_eq!(err.code, gestalt_error_code::UNAUTHENTICATED);
        assert_eq!(err.message, "Not authenticated");
    }
}
