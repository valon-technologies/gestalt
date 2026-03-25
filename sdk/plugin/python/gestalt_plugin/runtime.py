from __future__ import annotations

import os
import signal
import sys
from concurrent import futures
from typing import Any, Callable, Dict, List, Optional

import grpc
from google.protobuf import empty_pb2, struct_pb2
from google.protobuf.json_format import MessageToDict, ParseDict

from gestalt_plugin._proto import pb2, pb2_grpc
from gestalt_plugin.types import Capability, OperationResult, ParameterDef

ENV_PLUGIN_SOCKET = "GESTALT_PLUGIN_SOCKET"
ENV_RUNTIME_HOST_SOCKET = "GESTALT_RUNTIME_HOST_SOCKET"


class RuntimeHostClient:
    def __init__(self, channel: grpc.Channel):
        self._channel = channel
        self._stub = pb2_grpc.RuntimeHostStub(channel)

    def invoke(
        self,
        provider: str,
        operation: str,
        params: Optional[Dict[str, Any]] = None,
        instance: str = "",
    ) -> OperationResult:
        proto_params = ParseDict(params or {}, struct_pb2.Struct())
        req = pb2.InvokeRequest(
            principal=pb2.Principal(),
            provider=provider,
            operation=operation,
            instance=instance,
            params=proto_params,
        )
        resp = self._stub.Invoke(req)
        return OperationResult(status=resp.status, body=resp.body)

    def list_capabilities(self) -> List[Capability]:
        resp = self._stub.ListCapabilities(empty_pb2.Empty())
        caps = []
        for c in resp.capabilities:
            params = []
            for p in c.parameters:
                params.append(
                    ParameterDef(
                        name=p.name,
                        type=p.type,
                        description=p.description,
                        required=p.required,
                    )
                )
            caps.append(
                Capability(
                    provider=c.provider,
                    operation=c.operation,
                    description=c.description,
                    parameters=params,
                )
            )
        return caps

    def close(self):
        self._channel.close()


def dial_runtime_host() -> RuntimeHostClient:
    socket_path = os.environ.get(ENV_RUNTIME_HOST_SOCKET)
    if not socket_path:
        raise RuntimeError(f"{ENV_RUNTIME_HOST_SOCKET} is required")
    channel = grpc.insecure_channel(f"unix:{socket_path}")
    return RuntimeHostClient(channel)


class _RuntimeServicer(pb2_grpc.RuntimePluginServicer):
    def __init__(
        self,
        start: Callable[[str, Dict[str, Any]], None],
        stop: Callable[[], None],
    ):
        self._start = start
        self._stop = stop

    def Start(self, request, context):
        config: Dict[str, Any] = {}
        if request.HasField("config"):
            config = MessageToDict(request.config, preserving_proto_field_name=True)
        self._start(request.name, config)
        return empty_pb2.Empty()

    def Stop(self, request, context):
        self._stop()
        return empty_pb2.Empty()


def serve_runtime(
    start: Callable[[str, Dict[str, Any]], None],
    stop: Callable[[], None],
) -> None:
    socket_path = os.environ.get(ENV_PLUGIN_SOCKET)
    if not socket_path:
        print(f"error: {ENV_PLUGIN_SOCKET} is required", file=sys.stderr)
        sys.exit(1)

    if os.path.exists(socket_path):
        os.unlink(socket_path)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    servicer = _RuntimeServicer(start=start, stop=stop)
    pb2_grpc.add_RuntimePluginServicer_to_server(servicer, server)
    server.add_insecure_port(f"unix:{socket_path}")
    server.start()

    def _shutdown(signum, frame):
        server.stop(grace=2)

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    server.wait_for_termination()
