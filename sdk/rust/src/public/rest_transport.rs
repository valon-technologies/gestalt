//! reqwest-based REST transport for the public Gestalt API (/api/v2).

use std::collections::BTreeMap;
use std::sync::Arc;

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use reqwest::Url;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue};
use serde_json::{Map, Value};

use crate::public::auth::Auth;
use crate::public::errors::{RESPONSE_KIND_HEADER, is_operation_result, parse_gateway_error};
use crate::public::generated::metadata::Method;
use crate::public::generated::rest_caller::RestCaller;
use crate::public::rest_mapping::{
    build_body_map, build_query_pairs, encode_query_string, path_param_names, substitute_path,
};
use crate::rpc_support::GestaltError;

/// Protobuf-JSON REST transport for generated public clients.
pub struct RestTransport {
    base_url: String,
    auth: Arc<dyn Auth>,
    client: reqwest::blocking::Client,
}

impl RestTransport {
    /// Creates a REST transport rooted at `base_url`.
    pub fn new(base_url: impl Into<String>, auth: Arc<dyn Auth>) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            auth,
            client: reqwest::blocking::Client::new(),
        }
    }

    fn join_url(&self, path: &str) -> Result<Url, GestaltError> {
        let path = if path.starts_with('/') {
            path.to_string()
        } else {
            format!("/{path}")
        };
        Url::parse(&format!("{}{path}", self.base_url)).map_err(|err| {
            GestaltError::new(
                crate::rpc_support::gestalt_error_code::INVALID_ARGUMENT,
                err.to_string(),
            )
        })
    }
}

impl RestCaller for RestTransport {
    fn call_unary(
        &self,
        method: &Method,
        request_json: Value,
        response_json: &mut Value,
    ) -> Result<(), GestaltError> {
        if method.http_verb.is_empty() || method.http_path.is_empty() {
            return Err(GestaltError::new(
                crate::rpc_support::gestalt_error_code::INVALID_ARGUMENT,
                format!("method {} has no HTTP binding", method.full_method),
            ));
        }

        let path_params = path_param_names(method.http_path);
        let path = substitute_path(method.http_path, &request_json)?;
        let mut url = self.join_url(&path)?;
        let mut headers = HeaderMap::new();
        headers.insert(ACCEPT, HeaderValue::from_static("application/json"));
        headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
        if let Some(authorization) = self.auth.authorization_header() {
            headers.insert(
                AUTHORIZATION,
                HeaderValue::from_str(&authorization).map_err(|err| {
                    GestaltError::new(
                        crate::rpc_support::gestalt_error_code::INVALID_ARGUMENT,
                        err.to_string(),
                    )
                })?,
            );
        }

        let http_method = method.http_verb.parse().unwrap_or(reqwest::Method::POST);
        let mut builder = self
            .client
            .request(http_method.clone(), url.clone())
            .headers(headers.clone());
        if method.http_verb == "GET" || method.http_verb == "DELETE" {
            if let Some(object) = request_json.as_object() {
                let query = build_query_pairs(object, &path_params);
                if !query.is_empty() {
                    url = self.join_url(&path)?;
                    url.set_query(Some(&encode_query_string(&query)));
                    builder = self.client.request(http_method, url).headers(headers);
                }
            }
        } else if let Some(object) = request_json.as_object() {
            let body = build_body_map(object, &path_params);
            builder = builder.json(&Value::Object(body));
        }

        let http_response = builder.send().map_err(|err| {
            GestaltError::new(
                crate::rpc_support::gestalt_error_code::UNAVAILABLE,
                err.to_string(),
            )
        })?;
        let status = http_response.status();
        let response_headers = http_response.headers().clone();
        let body = http_response.bytes().map_err(|err| {
            GestaltError::new(
                crate::rpc_support::gestalt_error_code::UNAVAILABLE,
                err.to_string(),
            )
        })?;

        if is_operation_result(&response_headers) {
            *response_json =
                fill_operation_result_json(status.as_u16() as i32, &body, &response_headers);
            return Ok(());
        }

        if status.as_u16() >= 400 {
            return Err(parse_gateway_error(status.as_u16(), &body).into());
        }
        if body.is_empty() {
            *response_json = Value::Object(Map::new());
            return Ok(());
        }
        *response_json = serde_json::from_slice(&body).map_err(|err| {
            GestaltError::new(
                crate::rpc_support::gestalt_error_code::INTERNAL,
                err.to_string(),
            )
        })?;
        Ok(())
    }
}

fn fill_operation_result_json(status: i32, body: &[u8], headers: &http::HeaderMap) -> Value {
    Value::Object(Map::from_iter([
        ("status".to_string(), Value::Number(status.into())),
        ("body".to_string(), Value::String(BASE64.encode(body))),
        (
            "headers".to_string(),
            Value::Object(headers_to_json(headers)),
        ),
    ]))
}

fn headers_to_json(headers: &http::HeaderMap) -> Map<String, Value> {
    let mut grouped: BTreeMap<String, Vec<String>> = BTreeMap::new();
    for (key, value) in headers.iter() {
        if key.as_str().eq_ignore_ascii_case(RESPONSE_KIND_HEADER) {
            continue;
        }
        if let Ok(text) = value.to_str() {
            grouped
                .entry(key.as_str().to_string())
                .or_default()
                .push(text.to_string());
        }
    }
    let mut out = Map::new();
    for (key, values) in grouped {
        out.insert(
            key,
            Value::Object(Map::from_iter([(
                "values".to_string(),
                Value::Array(values.into_iter().map(Value::String).collect::<Vec<_>>()),
            )])),
        );
    }
    out
}
