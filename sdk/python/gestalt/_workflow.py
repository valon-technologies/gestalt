from __future__ import annotations

import os
from typing import Any

import grpc

from .gen.v1 import workflow_pb2 as _pb
from .gen.v1 import workflow_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET"


class WorkflowHost:
    def __init__(self) -> None:
        socket_path = os.environ.get(ENV_WORKFLOW_HOST_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"{ENV_WORKFLOW_HOST_SOCKET} is not set")
        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.WorkflowHostStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def invoke_operation(self, request: Any) -> Any:
        return _grpc_call(self._stub.InvokeOperation, request)

    def __enter__(self) -> WorkflowHost:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
