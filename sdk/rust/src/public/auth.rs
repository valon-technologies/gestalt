//! Authentication helpers for the public Gestalt transport client.

use std::sync::{Arc, RwLock};

/// Supplies credentials for public gestaltd requests.
pub trait Auth: Send + Sync {
    /// Returns an `Authorization` header value when credentials are present.
    fn authorization_header(&self) -> Option<String>;

    /// Returns additional gRPC metadata entries to attach to every request.
    fn extra_metadata(&self) -> Vec<(&'static str, String)> {
        Vec::new()
    }
}

/// Bearer token authentication for REST and gRPC.
#[derive(Clone)]
pub struct BearerAuth {
    provider: Arc<dyn Fn() -> String + Send + Sync>,
}

impl std::fmt::Debug for BearerAuth {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BearerAuth").finish_non_exhaustive()
    }
}

impl BearerAuth {
    /// Creates bearer auth from a static token string.
    pub fn new(token: impl Into<String>) -> Self {
        let token = token.into();
        Self::dynamic(move || token.clone())
    }

    /// Creates bearer auth from a provider evaluated for each request.
    pub fn dynamic(provider: impl Fn() -> String + Send + Sync + 'static) -> Self {
        Self {
            provider: Arc::new(provider),
        }
    }

    /// Creates bearer auth backed by a shared, rotatable token value.
    pub fn shared(token: Arc<RwLock<String>>) -> Self {
        Self::dynamic(move || token.read().expect("token lock poisoned").clone())
    }
}

impl Auth for BearerAuth {
    fn authorization_header(&self) -> Option<String> {
        let token = (self.provider)().trim().to_string();
        if token.is_empty() {
            None
        } else {
            Some(format!("Bearer {token}"))
        }
    }
}

/// Unauthenticated requests.
#[derive(Clone, Copy, Debug, Default)]
pub struct NoAuth;

impl Auth for NoAuth {
    fn authorization_header(&self) -> Option<String> {
        None
    }
}

/// Creates bearer auth from a token or provider.
pub fn bearer(token: impl Into<String>) -> BearerAuth {
    BearerAuth::new(token)
}

/// Creates unauthenticated auth.
pub fn unauthenticated() -> NoAuth {
    NoAuth
}
