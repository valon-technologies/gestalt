//! reqwest-based REST transport for the public Gestalt API (/api/v2).

use std::sync::Arc;
use std::time::Duration;

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue};
use reqwest::{Client, Url};
use serde_json::{Map, Value};

use crate::public::auth::Auth;
use crate::public::errors::{RESPONSE_KIND_HEADER, is_operation_result, parse_gateway_error};
use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::unary_transport::UnaryTransport;
use crate::public::rest_mapping::{
    build_body_map, build_query_pairs, encode_query_string, path_param_names, substitute_path,
};
use crate::rpc_support::gestalt_error_code;
use prost::Message;

/// Protobuf-JSON REST transport implementing [`UnaryTransport`].
#[derive(Clone)]
pub struct RestTransport {
    base_url: String,
    auth: Arc<dyn Auth>,
    client: Client,
}

impl RestTransport {
    /// Creates a REST transport rooted at `base_url`.
    pub fn new(base_url: impl Into<String>, auth: Arc<dyn Auth>) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            auth,
            client: Client::builder()
                .use_rustls_tls()
                .build()
                .expect("reqwest client"),
        }
    }
}

impl UnaryTransport for RestTransport {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> impl std::future::Future<Output = Result<(), GestaltError>> + Send
    where
        Req: Message + Send + Sync,
        Resp: Message + Default + Send,
    {
        let encode = method.encode_request_json.ok_or_else(|| {
            GestaltError::new(
                gestalt_error_code::INVALID_ARGUMENT,
                format!("method {} has no REST encoder", method.full_method),
            )
        });
        let decode = method.decode_response_json.ok_or_else(|| {
            GestaltError::new(
                gestalt_error_code::INVALID_ARGUMENT,
                format!("method {} has no REST decoder", method.full_method),
            )
        });

        let base_url = self.base_url.clone();
        let auth = Arc::clone(&self.auth);
        let client = self.client.clone();
        let http_verb = method.http_verb.to_string();
        let http_path = method.http_path.to_string();
        let full_method = method.full_method.to_string();
        let request_bytes = request.encode_to_vec();

        async move {
            let encode = encode?;
            let decode = decode?;
            if http_verb.is_empty() || http_path.is_empty() {
                return Err(GestaltError::new(
                    gestalt_error_code::INVALID_ARGUMENT,
                    format!("method {full_method} has no HTTP binding"),
                ));
            }

            let request_json = encode(&request_bytes)?;
            let path_params = path_param_names(&http_path);
            let path = substitute_path(&http_path, &request_json)?;
            let mut url = Url::parse(&format!("{base_url}{path}")).map_err(|err| {
                GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
            })?;

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

            let http_method = http_verb.parse().unwrap_or(reqwest::Method::POST);
            let mut builder = client
                .request(http_method.clone(), url.clone())
                .headers(headers.clone());
            if http_verb == "GET" || http_verb == "DELETE" {
                if let Some(object) = request_json.as_object() {
                    let query = build_query_pairs(object, &path_params);
                    if !query.is_empty() {
                        url.set_query(Some(&encode_query_string(&query)));
                        builder = client.request(http_method, url).headers(headers);
                    }
                }
            } else if let Some(object) = request_json.as_object() {
                let body = build_body_map(object, &path_params);
                builder = builder.json(&Value::Object(body));
            }

            let http_response = builder.send().await.map_err(|err| {
                if err.is_timeout() {
                    GestaltError::new(gestalt_error_code::DEADLINE_EXCEEDED, err.to_string())
                } else if err.is_request() {
                    GestaltError::new(gestalt_error_code::CANCELLED, err.to_string())
                } else {
                    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
                }
            })?;
            let status = http_response.status();
            let response_headers = http_response.headers().clone();
            let body = http_response.bytes().await.map_err(|err| {
                GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
            })?;

            let response_json = if is_operation_result(&response_headers) {
                fill_operation_result_json(status.as_u16() as i32, &body, &response_headers)
            } else if status.as_u16() >= 400 {
                return Err(parse_gateway_error(status.as_u16(), &body).into());
            } else if body.is_empty() {
                Value::Object(Map::new())
            } else {
                serde_json::from_slice(&body).map_err(|err| {
                    GestaltError::new(gestalt_error_code::INTERNAL, err.to_string())
                })?
            };

            let response_bytes = decode(&response_json)?;
            *response = Resp::decode(response_bytes.as_slice())
                .map_err(|err| GestaltError::new(gestalt_error_code::INTERNAL, err.to_string()))?;
            Ok(())
        }
    }
}

impl RestTransport {
    /// Applies a per-request timeout to the underlying HTTP client.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.client = Client::builder()
            .use_rustls_tls()
            .timeout(timeout)
            .build()
            .expect("reqwest client");
        self
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
    use std::collections::BTreeMap;
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
                Value::Array(values.into_iter().map(Value::String).collect()),
            )])),
        );
    }
    out
}
