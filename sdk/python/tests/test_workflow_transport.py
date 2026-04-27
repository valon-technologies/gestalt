"""Transport-backed Workflow SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import (
    ENV_WORKFLOW_HOST_SOCKET,
    WorkflowHost,
)
from gestalt.gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt.gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc
_server: grpc.Server | None = None
_socket_path: str = ""


class _WorkflowHostServicer(workflow_pb2_grpc.WorkflowHostServicer):
    def InvokeOperation(self, request: Any, context: grpc.ServicerContext) -> Any:
        target = request.target
        plugin = target.plugin if target is not None else None
        operation = plugin.operation if plugin is not None else ""
        return workflow_pb2.InvokeWorkflowOperationResponse(
            status=202,
            body=f"{request.run_id}:{operation}",
        )


def setUpModule() -> None:
    global _server, _socket_path
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-workflow-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    workflow_pb2_grpc.add_WorkflowHostServicer_to_server(_WorkflowHostServicer(), _server)
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    os.environ[ENV_WORKFLOW_HOST_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _server is not None:
        _server.stop(None)
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class WorkflowTransportTests(unittest.TestCase):
    def test_workflow_host_roundtrip(self) -> None:
        with WorkflowHost() as host:
            response = host.invoke_operation(
                workflow_pb2.InvokeWorkflowOperationRequest(
                    run_id="run-42",
                    target=workflow_pb2.BoundWorkflowTarget(
                        plugin=workflow_pb2.BoundWorkflowPluginTarget(
                            plugin_name="demo",
                            operation="sync",
                        ),
                    ),
                )
            )
        self.assertEqual(response.status, 202)
        self.assertEqual(response.body, "run-42:sync")
