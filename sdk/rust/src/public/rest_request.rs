//! Shared request building and response decoding for REST transports.
//!
//! Both the async `RestTransport` and the sync `SyncRestTransport` use these
//! pure helpers so the request assembly and response decoding logic lives in
//! exactly one place. Only the HTTP I/O half differs between the transports.

use std::sync::Arc;

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use http::HeaderMap;
use reqwest::Method as HttpMethod;
use reqwest::Url;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderName, HeaderValue};
use serde_json::{Map, Value};

use crate::public::auth::Auth;
use crate::public::errors::{is_operation_result, parse_gateway_error};
use crate::public::generated::metadata::{DecodeResponseJson, Method};
use crate::public::generated::rpc_support::GestaltError;
use crate::public::rest_mapping::{
    build_body_map, build_query_pairs, encode_query_string, substitute_path,
};
use crate::rpc_support::gestalt_error_code;

const HEADER_GESTALT_CLIENT: &str = "x-gestalt-client";
const HEADER_GESTALT_CLIENT_VERSION: &str = "x-gestalt-client-version";

/// Assembled REST request, ready for an HTTP send.
pub(crate) struct PreparedRequest {
    /// Target URL with path substitution and query string applied.
    pub url: Url,
    /// HTTP method (GET/POST/PUT/PATCH/DELETE).
    pub method: HttpMethod,
    /// Request headers (Accept, Content-Type, Authorization).
    pub headers: HeaderMap,
    /// JSON body for non-GET/DELETE requests; `None` for query-string requests.
    pub body: Option<Value>,
}

/// Builds the REST request: encode wire bytes to protobuf JSON, substitute
/// path parameters, assemble headers, and build the query string or JSON body.
/// Pure sync; performs no I/O.
pub(crate) fn build_rest_request(
    method: &Method,
    request_bytes: &[u8],
    base_url: &str,
    auth: &Arc<dyn Auth>,
    gestalt_client_kind: Option<&str>,
    gestalt_client_version: Option<&str>,
) -> Result<PreparedRequest, GestaltError> {
    let encode = method.encode_request_json.ok_or_else(|| {
        GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            format!("method {} has no REST encoder", method.full_method),
        )
    })?;
    if method.http_verb.is_empty() || method.http_path.is_empty() {
        return Err(GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            format!("method {} has no HTTP binding", method.full_method),
        ));
    }

    let request_json = encode(request_bytes)?;
    let path = substitute_path(method.http_path, &request_json, method.http_path_fields)?;
    let mut url = Url::parse(&format!("{base_url}{path}"))
        .map_err(|err| GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string()))?;

    let mut headers = HeaderMap::new();
    headers.insert(ACCEPT, HeaderValue::from_static("application/json"));
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    if let Some(authorization) = auth.authorization_header() {
        headers.insert(
            AUTHORIZATION,
            HeaderValue::from_str(&authorization).map_err(|err| {
                GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
            })?,
        );
    }
    if let Some(kind) = gestalt_client_kind {
        headers.insert(
            HeaderName::from_static(HEADER_GESTALT_CLIENT),
            HeaderValue::from_str(kind).map_err(|err| {
                GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
            })?,
        );
    }
    if let Some(version) = gestalt_client_version {
        headers.insert(
            HeaderName::from_static(HEADER_GESTALT_CLIENT_VERSION),
            HeaderValue::from_str(version).map_err(|err| {
                GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
            })?,
        );
    }

    let http_method: HttpMethod = method.http_verb.parse().map_err(|_| {
        GestaltError::new(
            gestalt_error_code::INVALID_ARGUMENT,
            format!("unsupported HTTP verb {}", method.http_verb),
        )
    })?;

    let body = if method.http_verb == "GET" || method.http_verb == "DELETE" {
        if let Some(object) = request_json.as_object() {
            let query = build_query_pairs(object, method.http_path_fields);
            if !query.is_empty() {
                url.set_query(Some(&encode_query_string(&query)));
            }
        }
        None
    } else {
        request_json
            .as_object()
            .map(|object| Value::Object(build_body_map(object, method.http_path_fields)))
    };

    Ok(PreparedRequest {
        url,
        method: http_method,
        headers,
        body,
    })
}

/// Decodes a REST response body into wire bytes suitable for prost decode.
/// Handles the OperationResult envelope, error-status mapping, empty bodies,
/// and protobuf-JSON decode. Pure sync; performs no I/O.
pub(crate) fn decode_rest_response(
    status: u16,
    response_headers: &HeaderMap,
    body: &[u8],
    decode: DecodeResponseJson,
) -> Result<Vec<u8>, GestaltError> {
    let response_json = if is_operation_result(response_headers) {
        fill_operation_result_json(status as i32, body, response_headers)
    } else if status >= 400 {
        return Err(parse_gateway_error(status, body));
    } else if body.is_empty() {
        Value::Object(Map::new())
    } else {
        serde_json::from_slice(body)
            .map_err(|err| GestaltError::new(gestalt_error_code::INTERNAL, err.to_string()))?
    };
    decode(&response_json)
}

fn fill_operation_result_json(status: i32, body: &[u8], headers: &HeaderMap) -> Value {
    Value::Object(Map::from_iter([
        ("status".to_string(), Value::Number(status.into())),
        ("body".to_string(), Value::String(BASE64.encode(body))),
        (
            "headers".to_string(),
            Value::Object(headers_to_json(headers)),
        ),
    ]))
}

fn headers_to_json(headers: &HeaderMap) -> Map<String, Value> {
    use std::collections::BTreeMap;
    let mut grouped: BTreeMap<String, Vec<String>> = BTreeMap::new();
    for (key, value) in headers.iter() {
        if key
            .as_str()
            .eq_ignore_ascii_case(crate::public::errors::RESPONSE_KIND_HEADER)
        {
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
                Value::Array(values.into_iter().map(Value::String).collect()),
            )])),
        );
    }
    out
}
