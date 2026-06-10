use serde_json::Value;

#[derive(Clone, Debug, thiserror::Error)]
#[error("{message}")]
/// Decoded app invocation failure.
pub struct InvokeError {
    pub app: String,
    pub operation: String,
    pub status: Option<u16>,
    pub code: Option<String>,
    pub message: String,
    pub body: Option<Box<Value>>,
    pub raw_body: Vec<u8>,
}

pub(crate) fn parse_operation_result_json(body: &[u8]) -> Result<Value, serde_json::Error> {
    if body.iter().all(u8::is_ascii_whitespace) {
        return Ok(serde_json::json!({}));
    }
    serde_json::from_slice(body)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_empty_bodies_as_empty_objects() {
        assert_eq!(
            parse_operation_result_json(b"  \n\t ").unwrap(),
            serde_json::json!({})
        );
        assert_eq!(
            parse_operation_result_json(br#"{ "ok": true }"#).unwrap(),
            serde_json::json!({ "ok": true })
        );
        parse_operation_result_json(b"not json").expect_err("invalid json");
    }
}
