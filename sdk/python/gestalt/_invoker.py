from __future__ import annotations

import os
from collections.abc import Mapping, Sequence
from typing import Any, Protocol, cast
from urllib import parse as _urlparse

import grpc
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from ._api import Response
from ._grpc_transport import (
    insecure_internal_channel,
    internal_channel_target,
    secure_internal_channel,
)
from .gen.v1 import plugin_pb2 as _pb
from .gen.v1 import plugin_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
struct_pb2: Any = _struct_pb2

# Matches the host-side socket name exposed by gestaltd.
ENV_PLUGIN_INVOKER_SOCKET = "GESTALT_PLUGIN_INVOKER_SOCKET"
ENV_PLUGIN_INVOKER_SOCKET_TOKEN = f"{ENV_PLUGIN_INVOKER_SOCKET}_TOKEN"
_PLUGIN_INVOKER_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token"


class PluginInvoker:
    """Client for invoking sibling plugin operations from provider code.

    ``PluginInvoker`` connects to the host plugin-invoker service exposed in
    ``GESTALT_PLUGIN_INVOKER_SOCKET``. It attaches the invocation token supplied
    by the host to each request and returns regular :class:`gestalt.Response`
    objects for operation and GraphQL calls.
    """

    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("plugin invoker: invocation token is not available")

        socket_path = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"plugin invoker: {ENV_PLUGIN_INVOKER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET_TOKEN, "")

        self._channel = _plugin_invoker_channel(socket_path, token=relay_token)
        self._stub = pb_grpc.PluginInvokerStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def invoke(
        self,
        plugin: str,
        operation: str,
        params: dict[str, Any] | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke one operation on another plugin.

        ``params`` is encoded as a protobuf ``Struct``. ``connection`` and
        ``instance`` select the connected account or provider instance that the
        target plugin should invoke against.
        """

        request = pb.PluginInvokeRequest(
            invocation_token=self._invocation_token,
            plugin=plugin,
            operation=operation,
            connection=connection,
            instance=instance,
            idempotency_key=idempotency_key.strip(),
        )
        message = _struct_from_dict(params)
        if message is not None:
            request.params.CopyFrom(message)

        response = self._stub.Invoke(request)
        return Response(status=int(response.status), body=response.body)

    def invoke_graphql(
        self,
        plugin: str,
        document: str,
        variables: dict[str, Any] | None = None,
        *,
        connection: str = "",
        instance: str = "",
        idempotency_key: str = "",
    ) -> Response[str]:
        """Invoke another plugin's GraphQL surface."""

        trimmed_document = document.strip()
        if not trimmed_document:
            raise RuntimeError("plugin invoker: graphql document is required")

        request = pb.PluginInvokeGraphQLRequest(
            invocation_token=self._invocation_token,
            plugin=plugin,
            document=trimmed_document,
            connection=connection,
            instance=instance,
            idempotency_key=idempotency_key.strip(),
        )
        message = _struct_from_dict_optional(variables, preserve_empty=False)
        if message is not None:
            request.variables.CopyFrom(message)

        response = self._stub.InvokeGraphQL(request)
        return Response(status=int(response.status), body=response.body)

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

    def __enter__(self) -> PluginInvoker:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _struct_from_dict(values: dict[str, Any] | None) -> Any:
    if values is None:
        return None

    return _struct_from_dict_optional(values, preserve_empty=True)


def _struct_from_dict_optional(
    values: dict[str, Any] | None,
    *,
    preserve_empty: bool,
) -> Any:
    if values is None:
        return None
    if not preserve_empty and not values:
        return None

    message = struct_pb2.Struct()
    json_format.ParseDict(values, message)
    return message


def _grants_from_values(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []

    grants: list[Any] = []
    for value in values:
        plugin, operations, surfaces, all_operations = _grant_parts(value)
        if not plugin:
            continue
        grants.append(
            pb.PluginInvocationGrant(
                plugin=plugin,
                operations=operations,
                surfaces=surfaces,
                all_operations=all_operations,
            )
        )
    return grants


def _grant_parts(value: Any) -> tuple[str, list[str], list[str], bool]:
    if isinstance(value, Mapping):
        raw_plugin = value.get("plugin", "")
        raw_operations = value.get("operations", ())
        raw_surfaces = value.get("surfaces", ())
        raw_all_operations = value.get("all_operations", value.get("allOperations", False))
    else:
        raw_plugin = getattr(value, "plugin", "")
        raw_operations = getattr(value, "operations", ())
        raw_surfaces = getattr(value, "surfaces", ())
        raw_all_operations = getattr(
            value,
            "all_operations",
            getattr(value, "allOperations", False),
        )

    plugin = str(raw_plugin).strip()
    if isinstance(raw_operations, str):
        raw_operations = [raw_operations]
    if isinstance(raw_surfaces, str):
        raw_surfaces = [raw_surfaces]

    operations = [str(operation).strip() for operation in raw_operations or ()]
    surfaces = [str(surface).strip().lower() for surface in raw_surfaces or ()]
    return (
        plugin,
        [operation for operation in operations if operation],
        [surface for surface in surfaces if surface],
        bool(raw_all_operations),
    )


def _plugin_invoker_channel(raw_target: str, *, token: str = "") -> grpc.Channel:
    target = raw_target.strip()
    if not target:
        raise RuntimeError("plugin invoker: transport target is required")
    if target.startswith("tcp://"):
        address = target[len("tcp://") :].strip()
        if not address:
            raise RuntimeError(
                f"plugin invoker: tcp target {raw_target!r} is missing host:port"
            )
        return _with_plugin_invoker_relay_token(
            insecure_internal_channel(internal_channel_target("tcp", address)),
            token,
        )
    if target.startswith("tls://"):
        address = target[len("tls://") :].strip()
        if not address:
            raise RuntimeError(
                f"plugin invoker: tls target {raw_target!r} is missing host:port"
            )
        return _with_plugin_invoker_relay_token(
            secure_internal_channel(internal_channel_target("tls", address)),
            token,
        )
    if target.startswith("unix://"):
        socket_path = target[len("unix://") :].strip()
        if not socket_path:
            raise RuntimeError(
                f"plugin invoker: unix target {raw_target!r} is missing a socket path"
            )
        return _with_plugin_invoker_relay_token(
            insecure_internal_channel(internal_channel_target("unix", socket_path)),
            token,
        )
    if "://" in target:
        parsed = _urlparse.urlparse(target)
        raise RuntimeError(
            f"plugin invoker: unsupported target scheme {parsed.scheme!r}"
        )
    return _with_plugin_invoker_relay_token(
        insecure_internal_channel(internal_channel_target("unix", target)),
        token,
    )


def _with_plugin_invoker_relay_token(
    channel: grpc.Channel, token: str
) -> grpc.Channel:
    token = token.strip()
    if not token:
        return channel
    interceptor = _RelayTokenInterceptor(token)
    return grpc.intercept_channel(channel, interceptor)


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
        metadata.append((_PLUGIN_INVOKER_RELAY_TOKEN_HEADER, self._token))
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
