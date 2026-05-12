from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Any

from ._gen.v1 import authentication_pb2 as _pb
from ._protocol import coerce_model as _coerce
from ._protocol import copy_message as _copy

pb: Any = _pb


@dataclass(slots=True)
class AuthenticatedUser:
    """Authenticated user returned by an authentication provider."""

    subject: str = ""
    email: str = ""
    email_verified: bool = False
    display_name: str = ""
    avatar_url: str = ""
    claims: Mapping[str, str] = field(default_factory=dict)


@dataclass(slots=True)
class BeginLoginRequest:
    """Begin-login request passed to authentication providers."""

    callback_url: str = ""
    host_state: str = ""
    scopes: list[str] = field(default_factory=list)
    options: Mapping[str, str] = field(default_factory=dict)


@dataclass(slots=True)
class BeginLoginResponse:
    """Begin-login response returned by authentication providers."""

    authorization_url: str = ""
    provider_state: bytes = b""


@dataclass(slots=True)
class CompleteLoginRequest:
    """Complete-login request passed to authentication providers."""

    query: Mapping[str, str] = field(default_factory=dict)
    provider_state: bytes = b""
    callback_url: str = ""


def authenticated_user_from_proto(value: Any) -> AuthenticatedUser:
    return AuthenticatedUser(
        subject=value.subject,
        email=value.email,
        email_verified=value.email_verified,
        display_name=value.display_name,
        avatar_url=value.avatar_url,
        claims=dict(value.claims),
    )


def authenticated_user_to_proto(value: Any) -> Any:
    if isinstance(value, pb.AuthenticatedUser):
        return _copy(value)
    user = _coerce(value, AuthenticatedUser, "AuthenticatedUser")
    return pb.AuthenticatedUser(
        subject=user.subject,
        email=user.email,
        email_verified=user.email_verified,
        display_name=user.display_name,
        avatar_url=user.avatar_url,
        claims=dict(user.claims),
    )


def begin_login_request_from_proto(value: Any) -> BeginLoginRequest:
    return BeginLoginRequest(
        callback_url=value.callback_url,
        host_state=value.host_state,
        scopes=list(value.scopes),
        options=dict(value.options),
    )


def begin_login_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.BeginLoginResponse):
        return _copy(value)
    response = _coerce(value, BeginLoginResponse, "BeginLoginResponse")
    return pb.BeginLoginResponse(
        authorization_url=response.authorization_url,
        provider_state=bytes(response.provider_state),
    )


def complete_login_request_from_proto(value: Any) -> CompleteLoginRequest:
    return CompleteLoginRequest(
        query=dict(value.query),
        provider_state=bytes(value.provider_state),
        callback_url=value.callback_url,
    )
