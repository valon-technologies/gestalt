//! Canonical alias module for the identity provider client surface.
//!
//! Re-exports the generated `authentication` module types under the canonical
//! `identity` naming. The wire protocol, gRPC service, and host binding
//! remain `authentication` for compatibility.

pub use crate::authentication::{
    AuthorizeRequest, AuthorizeResponse, GetGrantRequest, GetGrantResponse, GrantScope,
    IntrospectRequest, IntrospectResponse, ListGrantsRequest, ListGrantsResponse,
    RevokeGrantRequest, RevokeGrantResponse, TokenRequest, TokenResponse, UserInfoRequest,
    UserInfoResponse,
};

/// Canonical alias for the generated `authentication::Authentication` client.
pub type Identity = crate::authentication::Authentication;
