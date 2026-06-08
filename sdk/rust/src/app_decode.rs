use serde_json::Value;

use crate::OperationResult;

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

pub(crate) fn decode_app_result(
    app: &str,
    operation: &str,
    result: &OperationResult,
) -> Result<Value, Box<InvokeError>> {
    decode_app_body(app, operation, result.status, &result.body)
}

pub(crate) fn decode_graphql_result(
    app: &str,
    result: &OperationResult,
) -> Result<Value, Box<InvokeError>> {
    let decoded = decode_app_result(app, "graphql", result)?;
    if let Ok(parsed) = parse_operation_result_json(&result.body) {
        raise_graphql_errors(app, &result.body, &parsed)?;
    }
    raise_graphql_errors(app, &result.body, &decoded)?;
    Ok(decoded)
}

fn raise_graphql_errors(app: &str, raw_body: &[u8], value: &Value) -> Result<(), Box<InvokeError>> {
    if let Value::Object(object) = value {
        if let Some(Value::Array(errors)) = object.get("errors") {
            if !errors.is_empty() {
                return Err(Box::new(InvokeError {
                    app: app.to_string(),
                    operation: "graphql".to_string(),
                    status: None,
                    code: Some("graphql_errors".to_string()),
                    message: graphql_error_message(errors),
                    body: Some(Box::new(value.clone())),
                    raw_body: raw_body.to_vec(),
                }));
            }
        }
    }
    Ok(())
}

pub(crate) fn parse_operation_result_json(body: &[u8]) -> Result<Value, serde_json::Error> {
    if body.iter().all(u8::is_ascii_whitespace) {
        return Ok(serde_json::json!({}));
    }
    serde_json::from_slice(body)
}

fn decode_app_body(
    app: &str,
    operation: &str,
    status: u16,
    body: &[u8],
) -> Result<Value, Box<InvokeError>> {
    let raw_body = body.to_vec();
    let parsed = match parse_operation_result_json(body) {
        Ok(value) => value,
        Err(_) if status >= 400 => {
            return Err(Box::new(InvokeError {
                app: app.to_string(),
                operation: operation.to_string(),
                status: Some(status),
                code: None,
                message: format!("app invoke failed with status {status}"),
                body: None,
                raw_body,
            }));
        }
        Err(_) => {
            return Err(Box::new(InvokeError {
                app: app.to_string(),
                operation: operation.to_string(),
                status: None,
                code: None,
                message: "app invoke response is not valid JSON".to_string(),
                body: None,
                raw_body,
            }));
        }
    };

    if status >= 400 {
        let (message, code) = message_code_from_body(&parsed);
        return Err(Box::new(InvokeError {
            app: app.to_string(),
            operation: operation.to_string(),
            status: Some(status),
            code,
            message: message.unwrap_or_else(|| format!("app invoke failed with status {status}")),
            body: Some(Box::new(parsed)),
            raw_body,
        }));
    }

    if let Value::Object(object) = &parsed {
        if let Some(Value::String(status)) = object.get("status") {
            if status == "error" {
                let (message, code) = message_code_from_body(&parsed);
                return Err(Box::new(InvokeError {
                    app: app.to_string(),
                    operation: operation.to_string(),
                    status: None,
                    code,
                    message: message.unwrap_or_else(|| "app invoke failed".to_string()),
                    body: Some(Box::new(parsed)),
                    raw_body: raw_body.clone(),
                }));
            }
            if status == "success" && object.contains_key("data") {
                return Ok(object.get("data").cloned().unwrap_or(Value::Null));
            }
        }
    }
    Ok(parsed)
}

fn message_code_from_body(value: &Value) -> (Option<String>, Option<String>) {
    let Some(object) = value.as_object() else {
        return (None, None);
    };
    let nested = object.get("error").and_then(Value::as_object);
    let message = nested
        .and_then(|error| error.get("message"))
        .and_then(Value::as_str)
        .or_else(|| object.get("message").and_then(Value::as_str))
        .filter(|value| !value.trim().is_empty())
        .map(str::to_string);
    let code = nested
        .and_then(|error| error.get("code"))
        .and_then(Value::as_str)
        .or_else(|| object.get("code").and_then(Value::as_str))
        .filter(|value| !value.trim().is_empty())
        .map(str::to_string);
    (message, code)
}

fn graphql_error_message(errors: &[Value]) -> String {
    errors
        .first()
        .and_then(Value::as_object)
        .and_then(|error| error.get("message"))
        .and_then(Value::as_str)
        .filter(|message| !message.trim().is_empty())
        .unwrap_or("GraphQL returned errors")
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn result(body: &str, status: u16) -> OperationResult {
        OperationResult {
            status,
            headers: Default::default(),
            body: body.as_bytes().to_vec(),
        }
    }

    #[test]
    fn decodes_shared_app_fixtures() {
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/success_envelope.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "id": 1 })
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(include_str!("../../testdata/app_invoke/plain_ok.json"), 200)
            )
            .unwrap(),
            serde_json::json!({ "pull_request": { "id": 123, "title": "Fix transport" } })
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/empty_body.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({})
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/success_missing_data.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "status": "success", "ok": true })
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/success_null_data.json"),
                    200
                )
            )
            .unwrap(),
            Value::Null
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/unknown_status.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "status": "pending", "data": { "id": 2 } })
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/non_string_status.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "status": true, "data": { "id": 3 } })
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(include_str!("../../testdata/app_invoke/array_ok.json"), 200)
            )
            .unwrap(),
            serde_json::json!([1, 2, 3])
        );
        assert_eq!(
            decode_app_result(
                "github",
                "get_issue",
                &result(
                    include_str!("../../testdata/app_invoke/primitive_ok.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!("ok")
        );
    }

    #[test]
    fn decodes_shared_error_fixtures() {
        let envelope = decode_app_result(
            "github",
            "get_issue",
            &result(
                include_str!("../../testdata/app_invoke/error_envelope.json"),
                200,
            ),
        )
        .expect_err("error envelope");
        assert_eq!(envelope.code.as_deref(), Some("missing_credential"));
        assert_eq!(envelope.message, "missing credential");

        let http = decode_app_result(
            "github",
            "get_issue",
            &result(include_str!("../../testdata/app_invoke/http_401.json"), 401),
        )
        .expect_err("http error");
        assert_eq!(http.status, Some(401));
        assert!(!http.raw_body.is_empty());

        decode_app_result(
            "github",
            "get_issue",
            &result(
                include_str!("../../testdata/app_invoke/invalid_json.txt"),
                200,
            ),
        )
        .expect_err("invalid json");
    }

    #[test]
    fn decodes_graphql_errors() {
        assert_eq!(
            decode_graphql_result(
                "linear",
                &result(
                    include_str!("../../testdata/app_invoke/graphql_ok.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "data": { "viewer": { "id": "user-1" } }, "errors": [] })
        );
        assert_eq!(
            decode_graphql_result(
                "linear",
                &result(
                    include_str!("../../testdata/app_invoke/graphql_malformed_errors.json"),
                    200
                )
            )
            .unwrap(),
            serde_json::json!({ "data": { "viewer": null }, "errors": { "message": "not an array" } })
        );
        let err = decode_graphql_result(
            "linear",
            &result(
                include_str!("../../testdata/app_invoke/graphql_errors.json"),
                200,
            ),
        )
        .expect_err("graphql errors");
        assert_eq!(err.code.as_deref(), Some("graphql_errors"));
        let envelope_err = decode_graphql_result(
            "linear",
            &result(
                include_str!("../../testdata/app_invoke/graphql_success_envelope_errors.json"),
                200,
            ),
        )
        .expect_err("enveloped graphql errors");
        assert_eq!(envelope_err.code.as_deref(), Some("graphql_errors"));
    }
}
