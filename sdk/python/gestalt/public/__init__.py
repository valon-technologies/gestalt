"""Public gestaltd transport client for external applications."""

from __future__ import annotations

from .auth import BearerAuth, Unauthenticated, bearer, unauthenticated
from .bound import gestalt_from_request
from .client import (
    GestaltClient,
    GrpcGestaltClient,
    GrpcTransport,
    RestGestaltClient,
    RestTransport,
    create_gestalt_client,
    grpc,
    rest,
)

__all__ = [
    "BearerAuth",
    "GestaltClient",
    "GrpcGestaltClient",
    "GrpcTransport",
    "RestGestaltClient",
    "RestTransport",
    "Unauthenticated",
    "bearer",
    "create_gestalt_client",
    "gestalt_from_request",
    "grpc",
    "rest",
    "unauthenticated",
]
