//! reqwest-based REST transport for the public Gestalt API (/api/v2).

use std::sync::Arc;
use std::time::Duration;

use prost::Message;
use reqwest::header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue};
use reqwest::{Client, Url};

use crate::public::auth::Auth;
use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::transport_kernel::{
    RawRestResponse, decode_rest_response, prepare_rest_request,
};
use crate::public::generated::unary_transport::UnaryTransport;
use crate::rpc_support::gestalt_error_code;

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
            base_url: base_url.into(),
            auth,
            client: Client::builder()
                .use_rustls_tls()
                .build()
                .expect("reqwest client"),
        }
    }

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
        let base_url = self.base_url.clone();
        let auth = Arc::clone(&self.auth);
        let client = self.client.clone();
        let request_bytes = request.encode_to_vec();

        async move {
            let prepared = prepare_rest_request(method, &request_bytes)?;

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

            let http_method: reqwest::Method = prepared.verb.parse().map_err(|_| {
                GestaltError::new(
                    gestalt_error_code::INVALID_ARGUMENT,
                    format!("unsupported HTTP verb {}", prepared.verb),
                )
            })?;
            let url = Url::parse(&format!("{base_url}{}", prepared.path)).map_err(|err| {
                GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
            })?;
            let mut builder = client
                .request(http_method, url)
                .headers(headers)
                .query(&prepared.query);
            if let Some(body) = prepared.body {
                builder = builder.json(&body);
            }

            let http_response = builder.send().await.map_err(|err| {
                if err.is_timeout() {
                    GestaltError::new(gestalt_error_code::DEADLINE_EXCEEDED, err.to_string())
                } else if err.is_request() {
                    GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
                } else {
                    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
                }
            })?;
            let status = http_response.status().as_u16();
            let response_headers = collect_header_pairs(http_response.headers());
            let body = http_response.bytes().await.map_err(|err| {
                GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
            })?;

            let response_bytes = decode_rest_response(
                method,
                RawRestResponse {
                    status,
                    headers: response_headers,
                    body: body.to_vec(),
                },
            )?;
            *response = Resp::decode(response_bytes.as_slice())
                .map_err(|err| GestaltError::new(gestalt_error_code::INTERNAL, err.to_string()))?;
            Ok(())
        }
    }
}

fn collect_header_pairs(headers: &HeaderMap) -> Vec<(String, String)> {
    headers
        .iter()
        .filter_map(|(key, value)| {
            value
                .to_str()
                .ok()
                .map(|text| (key.as_str().to_string(), text.to_string()))
        })
        .collect()
}
