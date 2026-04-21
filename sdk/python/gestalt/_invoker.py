from __future__ import annotations

import os
from collections.abc import Mapping, Sequence
from typing import Any

import grpc
from google.protobuf import json_format
from google.protobuf import struct_pb2 as _struct_pb2

from ._api import Response
from .gen.v1 import plugin_pb2 as _pb
from .gen.v1 import plugin_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc
struct_pb2: Any = _struct_pb2

# Matches the host-side socket name exposed by gestaltd.
ENV_PLUGIN_INVOKER_SOCKET = "GESTALT_PLUGIN_INVOKER_SOCKET"


class PluginInvoker:
    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("plugin invoker: invocation token is not available")

        socket_path = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"plugin invoker: {ENV_PLUGIN_INVOKER_SOCKET} is not set")

        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.PluginInvokerStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        self._channel.close()

    def invoke(
        self,
        plugin: str,
        operation: str,
        params: dict[str, Any] | None = None,
        *,
        connection: str = "",
        instance: str = "",
    ) -> Response[str]:
        request = pb.PluginInvokeRequest(
            invocation_token=self._invocation_token,
            plugin=plugin,
            operation=operation,
            connection=connection,
            instance=instance,
        )
        message = _struct_from_dict(params)
        if message is not None:
            request.params.CopyFrom(message)

        response = self._stub.Invoke(request)
        return Response(status=int(response.status), body=response.body)

    def exchange_invocation_token(
        self,
        *,
        grants: Sequence[Any] | None = None,
        ttl_seconds: int = 0,
    ) -> str:
        request = pb.ExchangeInvocationTokenRequest(
            parent_invocation_token=self._invocation_token,
        )
        request.grants.extend(_grants_from_values(grants))
        request.ttl_seconds = max(int(ttl_seconds), 0)

        response = self._stub.ExchangeInvocationToken(request)
        return response.invocation_token

    def __enter__(self) -> PluginInvoker:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _struct_from_dict(values: dict[str, Any] | None) -> Any:
    if values is None:
        return None

    message = struct_pb2.Struct()
    json_format.ParseDict(values, message)
    return message


def _grants_from_values(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []

    grants: list[Any] = []
    for value in values:
        plugin, operations = _grant_parts(value)
        if not plugin:
            continue
        grants.append(pb.PluginInvocationGrant(plugin=plugin, operations=operations))
    return grants


def _grant_parts(value: Any) -> tuple[str, list[str]]:
    if isinstance(value, Mapping):
        raw_plugin = value.get("plugin", "")
        raw_operations = value.get("operations", ())
    else:
        raw_plugin = getattr(value, "plugin", "")
        raw_operations = getattr(value, "operations", ())

    plugin = str(raw_plugin).strip()
    if isinstance(raw_operations, str):
        raw_operations = [raw_operations]

    operations = [str(operation).strip() for operation in raw_operations or ()]
    return plugin, [operation for operation in operations if operation]
