//! gRPC authentication helpers for public clients.

use std::sync::Arc;

use tonic::service::Interceptor;
use tonic::transport::Channel;
use tonic::{Request, Status};

use crate::public::auth::Auth;

/// Channel type used by generated public gRPC clients.
pub type AuthChannel = tonic::service::interceptor::InterceptedService<Channel, AuthInterceptor>;

/// Builds a gRPC service with bearer auth metadata attached to every request.
pub fn auth_channel(channel: Channel, auth: Arc<dyn Auth>) -> AuthChannel {
    tonic::service::interceptor::InterceptedService::new(channel, AuthInterceptor { auth })
}

/// gRPC interceptor attaching bearer auth metadata to every request.
#[derive(Clone)]
pub struct AuthInterceptor {
    auth: Arc<dyn Auth>,
}

impl AuthInterceptor {
    /// Creates an interceptor from an auth provider.
    pub fn new(auth: Arc<dyn Auth>) -> Self {
        Self { auth }
    }
}

impl Interceptor for AuthInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        if let Some(authorization) = self.auth.authorization_header() {
            request.metadata_mut().insert(
                "authorization",
                authorization.parse().map_err(
                    |err: tonic::metadata::errors::InvalidMetadataValue| {
                        Status::invalid_argument(err.to_string())
                    },
                )?,
            );
        }
        for (key, value) in self.auth.extra_metadata() {
            request.metadata_mut().insert(
                key,
                value
                    .parse()
                    .map_err(|err: tonic::metadata::errors::InvalidMetadataValue| {
                        Status::invalid_argument(err.to_string())
                    })?,
            );
        }
        Ok(request)
    }
}
