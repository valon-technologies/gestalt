//! Gestalt public transport errors (compatibility re-exports).

pub use crate::public::generated::transport_kernel::{
    RESPONSE_KIND_HEADER, RESPONSE_KIND_OPERATION_RESULT, parse_gateway_error,
};

use http::HeaderMap;

/// Reports whether headers mark an OperationResult passthrough.
pub fn is_operation_result(headers: &HeaderMap) -> bool {
    headers
        .get(RESPONSE_KIND_HEADER)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| value.eq_ignore_ascii_case(RESPONSE_KIND_OPERATION_RESULT))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rpc_support::gestalt_error_code;

    #[test]
    fn parses_gateway_error_field_and_string_code() {
        let body = br#"{"code":"Unauthenticated","error":"Not authenticated"}"#;
        let err = parse_gateway_error(401, body);
        assert_eq!(err.code, gestalt_error_code::UNAUTHENTICATED);
        assert_eq!(err.message, "Not authenticated");
    }
}
