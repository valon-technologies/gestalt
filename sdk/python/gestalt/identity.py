"""Canonical alias client for the identity provider wire service.

This module re-exports the generated :mod:`gestalt.authentication` client
under the canonical ``identity`` naming. The wire protocol, gRPC service, and
host binding remain ``authentication`` for compatibility.
"""

from .authentication import (
    Authentication as Identity,
    AuthorizeRequest,
    AuthorizeResponse,
    GetGrantRequest,
    GetGrantResponse,
    GrantScope,
    IntrospectRequest,
    IntrospectResponse,
    ListGrantsRequest,
    ListGrantsResponse,
    RevokeGrantRequest,
    RevokeGrantResponse,
    TokenRequest,
    TokenResponse,
    UserInfoRequest,
    UserInfoResponse,
)

__all__ = [
    "Identity",
    "AuthorizeRequest",
    "AuthorizeResponse",
    "GetGrantRequest",
    "GetGrantResponse",
    "GrantScope",
    "IntrospectRequest",
    "IntrospectResponse",
    "ListGrantsRequest",
    "ListGrantsResponse",
    "RevokeGrantRequest",
    "RevokeGrantResponse",
    "TokenRequest",
    "TokenResponse",
    "UserInfoRequest",
    "UserInfoResponse",
]
