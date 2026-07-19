//! reqwest-based REST transports for the public Gestalt API (/api/v2).
//!
//! [`RestTransport`] is async (backed by `reqwest`); [`SyncRestTransport`] is
//! sync (backed by `reqwest::blocking`). Both share request building and
//! response decoding via shared helpers in the rest_request module.

use std::sync::Arc;
use std::time::Duration;

use prost::Message;
use reqwest::header::HeaderMap;

use crate::public::auth::Auth;
use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;
use crate::public::generated::unary_transport::{SyncUnaryTransport, UnaryTransport};
use crate::public::rest_request::{build_rest_request, decode_rest_response};
use crate::rpc_support::gestalt_error_code;

/// Async reqwest-based REST transport implementing [`UnaryTransport`].
#[derive(Clone)]
pub struct RestTransport {
    base_url: String,
    auth: Arc<dyn Auth>,
    client: reqwest::Client,
}

impl RestTransport {
    /// Creates a REST transport rooted at `base_url`.
    pub fn new(base_url: impl Into<String>, auth: Arc<dyn Auth>) -> Self {
        Self {
            base_url: base_url.into(),
            auth,
            client: reqwest::Client::builder()
                .use_rustls_tls()
                .build()
                .expect("reqwest client"),
        }
    }

    /// Applies a per-request timeout to the underlying HTTP client.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.client = reqwest::Client::builder()
            .use_rustls_tls()
            .timeout(timeout)
            .build()
            .expect("reqwest client");
        self
    }
}

fn map_send_error(err: reqwest::Error) -> GestaltError {
    if err.is_timeout() {
        GestaltError::new(gestalt_error_code::DEADLINE_EXCEEDED, err.to_string())
    } else if err.is_request() {
        GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())
    } else {
        GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
    }
}

fn map_body_error(err: reqwest::Error) -> GestaltError {
    GestaltError::new(gestalt_error_code::UNAVAILABLE, err.to_string())
}

fn finalize_response<Resp>(
    status: u16,
    response_headers: &HeaderMap,
    body: &[u8],
    decode: crate::public::generated::metadata::DecodeResponseJson,
    response: &mut Resp,
) -> Result<(), GestaltError>
where
    Resp: Message + Default + Send,
{
    let response_bytes = decode_rest_response(status, response_headers, body, decode)?;
    *response = Resp::decode(response_bytes.as_slice())
        .map_err(|err| GestaltError::new(gestalt_error_code::INTERNAL, err.to_string()))?;
    Ok(())
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
        let method = method.clone();

        async move {
            let decode = method.decode_response_json.ok_or_else(|| {
                GestaltError::new(
                    gestalt_error_code::INVALID_ARGUMENT,
                    format!("method {} has no REST decoder", method.full_method),
                )
            })?;
            let prepared = build_rest_request(&method, &request_bytes, &base_url, &auth)?;
            let mut builder = client
                .request(prepared.method, prepared.url)
                .headers(prepared.headers);
            if let Some(body) = prepared.body {
                builder = builder.json(&body);
            }
            let http_response = builder.send().await.map_err(map_send_error)?;
            let status = http_response.status();
            let response_headers = http_response.headers().clone();
            let body = http_response.bytes().await.map_err(map_body_error)?;
            finalize_response(status.as_u16(), &response_headers, &body, decode, response)
        }
    }
}

/// Sync reqwest::blocking-based REST transport implementing [`SyncUnaryTransport`].
#[derive(Clone)]
pub struct SyncRestTransport {
    base_url: String,
    auth: Arc<dyn Auth>,
    client: reqwest::blocking::Client,
}

impl SyncRestTransport {
    /// Creates a sync REST transport rooted at `base_url`.
    pub fn new(base_url: impl Into<String>, auth: Arc<dyn Auth>) -> Self {
        Self {
            base_url: base_url.into(),
            auth,
            client: reqwest::blocking::Client::builder()
                .use_rustls_tls()
                .build()
                .expect("reqwest blocking client"),
        }
    }

    /// Applies a per-request timeout to the underlying HTTP client.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.client = reqwest::blocking::Client::builder()
            .use_rustls_tls()
            .timeout(timeout)
            .build()
            .expect("reqwest blocking client");
        self
    }
}

impl SyncUnaryTransport for SyncRestTransport {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> Result<(), GestaltError>
    where
        Req: Message + Clone + Send + Sync + 'static,
        Resp: Message + Default + Send + 'static,
    {
        let decode = method.decode_response_json.ok_or_else(|| {
            GestaltError::new(
                gestalt_error_code::INVALID_ARGUMENT,
                format!("method {} has no REST decoder", method.full_method),
            )
        })?;
        let prepared =
            build_rest_request(method, &request.encode_to_vec(), &self.base_url, &self.auth)?;
        let mut builder = self
            .client
            .request(prepared.method, prepared.url)
            .headers(prepared.headers);
        if let Some(body) = prepared.body {
            builder = builder.json(&body);
        }
        let http_response = builder.send().map_err(map_send_error)?;
        let status = http_response.status();
        let response_headers = http_response.headers().clone();
        let body = http_response.bytes().map_err(map_body_error)?;
        finalize_response(status.as_u16(), &response_headers, &body, decode, response)
    }
}
