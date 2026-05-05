"""Shared gRPC transport helpers for Gestalt-internal host-service channels."""

from __future__ import annotations

from typing import Any, cast
from urllib import parse as _urlparse

import grpc as _grpc

grpc: Any = cast(Any, _grpc)

INTERNAL_GRPC_MAX_MESSAGE_BYTES = 64 * 1024 * 1024
INTERNAL_GRPC_MESSAGE_OPTIONS = (
    ("grpc.max_receive_message_length", INTERNAL_GRPC_MAX_MESSAGE_BYTES),
    ("grpc.max_send_message_length", INTERNAL_GRPC_MAX_MESSAGE_BYTES),
)
_INTERNAL_CHANNEL_OPTIONS = (
    ("grpc.enable_http_proxy", 0),
    *INTERNAL_GRPC_MESSAGE_OPTIONS,
)
_HOST_SERVICE_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"


class RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(
        self, continuation: Any, client_call_details: Any, request: Any
    ) -> Any:
        metadata = list(client_call_details.metadata or [])
        metadata.append((_HOST_SERVICE_RELAY_TOKEN_HEADER, self._token))
        details = _ClientCallDetails(
            method=client_call_details.method,
            timeout=client_call_details.timeout,
            metadata=metadata,
            credentials=client_call_details.credentials,
            wait_for_ready=client_call_details.wait_for_ready,
            compression=client_call_details.compression,
        )
        return continuation(details, request)


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        *,
        method: str,
        timeout: float | None,
        metadata: list[tuple[str, str]],
        credentials: Any,
        wait_for_ready: bool | None,
        compression: Any,
    ) -> None:
        self.method = method
        self.timeout = timeout
        self.metadata = metadata
        self.credentials = credentials
        self.wait_for_ready = wait_for_ready
        self.compression = compression


def internal_channel_target(scheme: str, address: str) -> str:
    """Normalize an internal gRPC transport target for grpcio."""

    normalized_scheme = scheme.strip().lower()
    normalized_address = address.strip()
    if not normalized_address:
        raise RuntimeError("internal gRPC transport target is required")
    if normalized_scheme == "unix":
        return f"unix:{normalized_address}"
    if normalized_scheme in {"tcp", "tls"}:
        return normalized_address
    raise RuntimeError(f"unsupported internal gRPC transport scheme {scheme!r}")


def insecure_internal_channel(target: str) -> grpc.Channel:
    """Return an insecure internal gRPC channel with proxy use disabled."""

    return grpc.insecure_channel(target, options=_INTERNAL_CHANNEL_OPTIONS)


def secure_internal_channel(
    target: str, credentials: grpc.ChannelCredentials | None = None
) -> grpc.Channel:
    """Return a TLS internal gRPC channel with proxy use disabled."""

    if credentials is None:
        credentials = grpc.ssl_channel_credentials()
    return grpc.secure_channel(
        target,
        credentials,
        options=_INTERNAL_CHANNEL_OPTIONS,
    )


def host_service_channel(
    service_name: str, target: str, *, token: str = ""
) -> grpc.Channel:
    scheme, address = parse_host_service_target(service_name, target)
    if scheme == "unix":
        channel = insecure_internal_channel(internal_channel_target("unix", address))
    elif scheme == "tcp":
        channel = insecure_internal_channel(internal_channel_target("tcp", address))
    elif scheme == "tls":
        channel = secure_internal_channel(internal_channel_target("tls", address))
    else:
        raise RuntimeError(f"unsupported {service_name} transport scheme {scheme!r}")
    if token:
        channel = grpc.intercept_channel(channel, RelayTokenInterceptor(token))
    return channel


def parse_host_service_target(service_name: str, raw: str) -> tuple[str, str]:
    target = raw.strip()
    if not target:
        raise RuntimeError(f"{service_name}: transport target is required")
    if target.startswith("tcp://"):
        address = target.removeprefix("tcp://").strip()
        if not address:
            raise RuntimeError(
                f"{service_name}: tcp target {raw!r} is missing host:port"
            )
        return "tcp", address
    if target.startswith("tls://"):
        address = target.removeprefix("tls://").strip()
        if not address:
            raise RuntimeError(
                f"{service_name}: tls target {raw!r} is missing host:port"
            )
        return "tls", address
    if target.startswith("unix://"):
        address = target.removeprefix("unix://").strip()
        if not address:
            raise RuntimeError(
                f"{service_name}: unix target {raw!r} is missing a socket path"
            )
        return "unix", address
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"{service_name}: unsupported target scheme {parsed.scheme!r}"
        )
    return "unix", target
