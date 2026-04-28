from __future__ import annotations

import os
from typing import Any

import grpc

from ._grpc_transport import host_service_channel
from .gen.v1 import workflow_pb2 as _pb
from .gen.v1 import workflow_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET"
ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET"
ENV_WORKFLOW_MANAGER_SOCKET_TOKEN = f"{ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN"


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


class WorkflowManager:
    def __init__(self, invocation_token: str) -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("workflow manager: invocation token is not available")

        target = os.environ.get(ENV_WORKFLOW_MANAGER_SOCKET, "")
        if not target:
            raise RuntimeError(
                f"workflow manager: {ENV_WORKFLOW_MANAGER_SOCKET} is not set"
            )
        relay_token = os.environ.get(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, "")

        self._channel = host_service_channel(
            "workflow manager", target, token=relay_token
        )
        self._stub = pb_grpc.WorkflowManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        self._channel.close()

    def publish_event(self, request: Any) -> Any:
        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.PublishEvent, request)

    def __enter__(self) -> WorkflowManager:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
