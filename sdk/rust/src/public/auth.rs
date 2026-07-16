//! Authentication helpers for the public Gestalt transport client.

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
#[derive(Clone, Debug)]
pub struct BearerAuth {
    token: String,
}

impl BearerAuth {
    /// Creates bearer auth from a raw token string.
    pub fn new(token: impl Into<String>) -> Self {
        Self {
            token: token.into(),
        }
    }
}

impl Auth for BearerAuth {
    fn authorization_header(&self) -> Option<String> {
        let token = self.token.trim();
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
