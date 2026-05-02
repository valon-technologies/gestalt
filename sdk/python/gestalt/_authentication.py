from __future__ import annotations

from typing import Any

from ._gen.v1 import authentication_pb2 as _pb

pb: Any = _pb


def AuthenticatedUser(*args: Any, **kwargs: Any) -> Any:
    """Create an authenticated-user protocol value."""

    return pb.AuthenticatedUser(*args, **kwargs)


def BeginLoginRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authentication begin-login request."""

    return pb.BeginLoginRequest(*args, **kwargs)


def BeginLoginResponse(*args: Any, **kwargs: Any) -> Any:
    """Create an authentication begin-login response."""

    return pb.BeginLoginResponse(*args, **kwargs)


def CompleteLoginRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authentication complete-login request."""

    return pb.CompleteLoginRequest(*args, **kwargs)


def ValidateExternalTokenRequest(*args: Any, **kwargs: Any) -> Any:
    """Create an authentication external-token validation request."""

    return pb.ValidateExternalTokenRequest(*args, **kwargs)


def AuthSessionSettings(*args: Any, **kwargs: Any) -> Any:
    """Create authentication session settings."""

    return pb.AuthSessionSettings(*args, **kwargs)
