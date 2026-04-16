from __future__ import annotations

import os
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
    def __init__(self, request_handle: str) -> None:
        trimmed_handle = request_handle.strip()
        if not trimmed_handle:
            raise RuntimeError("plugin invoker: request handle is not available")

        socket_path = os.environ.get(ENV_PLUGIN_INVOKER_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"plugin invoker: {ENV_PLUGIN_INVOKER_SOCKET} is not set")

        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.PluginInvokerStub(self._channel)
        self._request_handle = trimmed_handle

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
            request_handle=self._request_handle,
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
