from __future__ import annotations

import os
from collections.abc import Mapping, Sequence
from typing import Any, Protocol, cast
from urllib import parse as _urlparse

import grpc

from ._api import Response, ResponseHeaders
from ._gen.v1 import app_pb2 as _pb
from ._gen.v1 import app_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)
from ._protocol import (
    JsonObjectInput,
    _struct_from_normalized_object,
    json_from_native,
    string_lists_from_proto_map,
)

pb: Any = _pb
pb_grpc: Any = _pb_grpc

# Matches the app socket name exposed by gestaltd.
_APP_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"


class AppProtocol(Protocol):
    """Fakeable contract for app invocation calls."""

    def __enter__(self) -> AppProtocol:
        """Return the client for ``with`` statements."""

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

    def close(self) -> None:
        """Close the client."""

    def invoke(
        self,
        plugin: str,
        operation: str,
        params: JsonObjectInput | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke one operation on another app."""

    def invoke_graphql(
        self,
        plugin: str,
        document: str,
        variables: JsonObjectInput | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke another app's GraphQL surface."""

    def exchange_invocation_token(
        self,
        *,
        grants: Sequence[Any] | None = None,
        ttl_seconds: int = 0,
    ) -> str:
        """Exchange this invocation token for a narrower child token."""


class _AppClient:
    """Transport-backed implementation for invoking sibling app operations.

    Provider code should obtain this through :meth:`gestalt.Request.app`.
    """

    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("app: invocation token is not available")

        socket_path = os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
        if not socket_path:
            raise RuntimeError(
                f"app: {ENV_HOST_SERVICE_SOCKET} is not set"
            )
        relay_token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "")

        self._channel = _app_channel(socket_path, token=relay_token)
        self._stub = pb_grpc.AppStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def invoke(
        self,
        plugin: str,
        operation: str,
        params: JsonObjectInput | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke one operation on another app.

        ``params`` accepts a JSON-compatible object. ``connection`` and
        ``instance`` select the connected account or provider instance that the
        target app should invoke against.
        """

        request = pb.AppInvokeRequest(
            invocation_token=self._invocation_token,
            app=plugin,
            operation=operation,
            connection=connection,
            instance=instance,
            idempotency_key=idempotency_key.strip(),
        )
        message = _struct_from_dict(params)
        if message is not None:
            request.params.CopyFrom(message)

        return _response_from_proto(self._stub.Invoke(request))

    def invoke_graphql(
        self,
        plugin: str,
        document: str,
        variables: JsonObjectInput | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke another plugin's GraphQL surface."""

        trimmed_document = document.strip()
        if not trimmed_document:
            raise RuntimeError("app: graphql document is required")

        request = pb.AppInvokeGraphQLRequest(
            invocation_token=self._invocation_token,
            app=plugin,
            document=trimmed_document,
            connection=connection,
            instance=instance,
            idempotency_key=idempotency_key.strip(),
        )
        message = _struct_from_dict_optional(
            variables, preserve_empty=False, path="app variables"
        )
        if message is not None:
            request.variables.CopyFrom(message)

        return _response_from_proto(self._stub.InvokeGraphQL(request))

    def exchange_invocation_token(
        self,
        *,
        grants: Sequence[Any] | None = None,
        ttl_seconds: int = 0,
    ) -> str:
        """Exchange this invocation token for a narrower child token."""

        request = pb.ExchangeInvocationTokenRequest(
            parent_invocation_token=self._invocation_token,
        )
        request.grants.extend(_grants_from_values(grants))
        request.ttl_seconds = max(int(ttl_seconds), 0)

        response = self._stub.ExchangeInvocationToken(request)
        return response.invocation_token

    def __enter__(self) -> _AppClient:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()

def _struct_from_dict(values: JsonObjectInput | None) -> Any:
    if values is None:
        return None

    return _struct_from_dict_optional(
        values, preserve_empty=True, path="app params"
    )


def _struct_from_dict_optional(
    values: JsonObjectInput | None,
    *,
    preserve_empty: bool,
    path: str,
) -> Any:
    if values is None:
        return None
    normalized = json_from_native(values, path=path)
    if not isinstance(normalized, dict):
        raise TypeError(f"{path} must be a JSON object, got {type(values).__name__}")
    if not preserve_empty and not normalized:
        return None

    return _struct_from_normalized_object(normalized)


def _grants_from_values(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []

    grants: list[Any] = []
    for value in values:
        plugin, operations, surfaces, all_operations = _grant_parts(value)
        if not plugin:
            continue
        grants.append(
            pb.AppInvocationGrant(
                app=plugin,
                operations=operations,
                surfaces=surfaces,
                all_operations=all_operations,
            )
        )
    return grants


def _grant_parts(value: Any) -> tuple[str, list[str], list[str], bool]:
    if isinstance(value, Mapping):
        raw_plugin = value.get("app", "")
        raw_operations = value.get("operations", ())
        raw_surfaces = value.get("surfaces", ())
        raw_all_operations = value.get(
            "all_operations", value.get("allOperations", False)
        )
    else:
        raw_plugin = getattr(value, "app", "")
        raw_operations = getattr(value, "operations", ())
        raw_surfaces = getattr(value, "surfaces", ())
        raw_all_operations = getattr(
            value,
            "all_operations",
            getattr(value, "allOperations", False),
        )

    app = str(raw_plugin).strip()
    if isinstance(raw_operations, str):
        raw_operations = [raw_operations]
    if isinstance(raw_surfaces, str):
        raw_surfaces = [raw_surfaces]

    operations = [str(operation).strip() for operation in raw_operations or ()]
    surfaces = [str(surface).strip().lower() for surface in raw_surfaces or ()]
    return (
        app,
        [operation for operation in operations if operation],
        [surface for surface in surfaces if surface],
        bool(raw_all_operations),
    )


def _app_channel(raw_target: str, *, token: str = "") -> grpc.Channel:
    target = raw_target.strip()
    if not target:
        raise RuntimeError("app: transport target is required")
    if target.startswith("tcp://"):
        address = target[len("tcp://") :].strip()
        if not address:
            raise RuntimeError(
                f"app: tcp target {raw_target!r} is missing host:port"
            )
        return _with_app_relay_token(
            insecure_internal_channel(internal_channel_target("tcp", address)),
            token,
        )
    if target.startswith("tls://"):
        address = target[len("tls://") :].strip()
        if not address:
            raise RuntimeError(
                f"app: tls target {raw_target!r} is missing host:port"
            )
        return _with_app_relay_token(
            secure_internal_channel(internal_channel_target("tls", address)),
            token,
        )
    if target.startswith("unix://"):
        socket_path = target[len("unix://") :].strip()
        if not socket_path:
            raise RuntimeError(
                f"app: unix target {raw_target!r} is missing a socket path"
            )
        return _with_app_relay_token(
            insecure_internal_channel(internal_channel_target("unix", socket_path)),
            token,
        )
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"app: unsupported target scheme {parsed.scheme!r}"
        )
    return _with_app_relay_token(
        insecure_internal_channel(internal_channel_target("unix", target)),
        token,
    )


def _with_app_relay_token(channel: grpc.Channel, token: str) -> grpc.Channel:
    token = token.strip()
    if not token:
        return channel
    interceptor = _RelayTokenInterceptor(token)
    return grpc.intercept_channel(channel, interceptor)


def _response_from_proto(response: Any) -> Response[str]:
    return Response[str](
        status=int(response.status),
        body=cast(str, response.body),
        headers=cast(
            ResponseHeaders,
            string_lists_from_proto_map(getattr(response, "headers", {})),
        ),
    )


class _ClientCallDetails(grpc.ClientCallDetails):
    def __init__(
        self,
        method: str,
        timeout: float | None,
        metadata: Any,
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


class _ClientCallDetailsFields(Protocol):
    method: str
    timeout: float | None
    metadata: Any
    credentials: Any
    wait_for_ready: bool | None
    compression: Any


class _RelayTokenInterceptor(grpc.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    def intercept_unary_unary(
        self,
        continuation: Any,
        client_call_details: grpc.ClientCallDetails,
        request: Any,
    ) -> Any:
        fields = cast(_ClientCallDetailsFields, client_call_details)
        metadata = list(fields.metadata or [])
        metadata.append((_APP_RELAY_TOKEN_HEADER, self._token))
        return continuation(
            _ClientCallDetails(
                fields.method,
                fields.timeout,
                metadata,
                fields.credentials,
                fields.wait_for_ready,
                fields.compression,
            ),
            request,
        )
