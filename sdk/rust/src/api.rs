use std::collections::BTreeMap;
use std::convert::Infallible;

use tonic::codegen::async_trait;

use crate::catalog::Catalog;
use crate::error::{Error, Result};

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Request {
    pub token: String,
    pub connection_params: BTreeMap<String, String>,
}

impl Request {
    pub fn connection_param(&self, name: &str) -> &str {
        self.connection_params
            .get(name)
            .map(String::as_str)
            .unwrap_or("")
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Response<T> {
    pub status: Option<u16>,
    pub body: T,
}

impl<T> Response<T> {
    pub fn new(status: u16, body: T) -> Self {
        Self {
            status: Some(status),
            body,
        }
    }
}

pub fn ok<T>(body: T) -> Response<T> {
    Response::new(200, body)
}

pub trait IntoResponse<T> {
    fn into_response(self) -> Response<T>;
}

impl<T> IntoResponse<T> for Response<T> {
    fn into_response(self) -> Response<T> {
        self
    }
}

impl<T> IntoResponse<T> for T {
    fn into_response(self) -> Response<T> {
        ok(self)
    }
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct RuntimeMetadata {
    pub name: String,
    pub display_name: String,
    pub description: String,
    pub version: String,
}

#[async_trait]
pub trait Provider: Send + Sync + 'static {
    async fn configure(
        &self,
        _name: &str,
        _config: serde_json::Map<String, serde_json::Value>,
    ) -> Result<()> {
        Ok(())
    }

    fn metadata(&self) -> Option<RuntimeMetadata> {
        None
    }

    fn warnings(&self) -> Vec<String> {
        Vec::new()
    }

    async fn health_check(&self) -> Result<()> {
        Ok(())
    }

    fn supports_session_catalog(&self) -> bool {
        false
    }

    async fn catalog_for_request(&self, _request: &Request) -> Result<Option<Catalog>> {
        Ok(None)
    }

    async fn close(&self) -> Result<()> {
        Ok(())
    }
}

impl From<Infallible> for Error {
    fn from(_value: Infallible) -> Self {
        Error::internal("unreachable infallible error")
    }
}
