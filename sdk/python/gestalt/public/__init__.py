"""Public gestaltd transport client for external applications."""

from __future__ import annotations

from .auth import BearerAuth, Unauthenticated, bearer, unauthenticated
from .bound import gestalt_from_request
from .client import (
    GestaltClient,
    GrpcTransport,
    RestTransport,
    create_gestalt_client,
    grpc,
    rest,
)

__all__ = [
    "BearerAuth",
    "GestaltClient",
    "GrpcTransport",
    "RestTransport",
    "Unauthenticated",
    "bearer",
    "create_gestalt_client",
    "gestalt_from_request",
    "grpc",
    "rest",
    "unauthenticated",
]
