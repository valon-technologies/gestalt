"""Transport-backed Workflow SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import struct_pb2 as _struct_pb2

from gestalt import (
    ENV_WORKFLOW_HOST_SOCKET,
    ENV_WORKFLOW_MANAGER_SOCKET,
    Request,
    WorkflowHost,
    WorkflowManager,
)
from gestalt.gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt.gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc
struct_pb2: Any = _struct_pb2
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


class _WorkflowManagerServicer(workflow_pb2_grpc.WorkflowManagerHostServicer):
    def PublishEvent(self, request: Any, context: grpc.ServicerContext) -> Any:
        data = struct_pb2.Struct()
        data.CopyFrom(request.private_input)
        data.update({"provider_name": request.provider_name})
        return workflow_pb2.WorkflowEvent(
            id=request.invocation_token,
            source=request.event.source,
            type=request.event.type,
            data=data,
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
    workflow_pb2_grpc.add_WorkflowManagerHostServicer_to_server(
        _WorkflowManagerServicer(), _server
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    os.environ[ENV_WORKFLOW_HOST_SOCKET] = _socket_path
    os.environ[ENV_WORKFLOW_MANAGER_SOCKET] = _socket_path


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

    def test_workflow_manager_publish_event_roundtrip(self) -> None:
        private_input = struct_pb2.Struct()
        private_input.update({"reply_ref": "signed-ref"})

        with WorkflowManager("inv-123") as manager:
            response = manager.publish_event(
                workflow_pb2.WorkflowManagerPublishEventRequest(
                    event=workflow_pb2.WorkflowEvent(
                        source="slack",
                        type="com.valon.slack.event",
                    ),
                    private_input=private_input,
                    provider_name="indexeddb",
                )
            )

        self.assertEqual(response.id, "inv-123")
        self.assertEqual(response.source, "slack")
        self.assertEqual(response.type, "com.valon.slack.event")
        self.assertEqual(response.data["reply_ref"], "signed-ref")
        self.assertEqual(response.data["provider_name"], "indexeddb")

    def test_request_exposes_workflow_manager(self) -> None:
        private_input = struct_pb2.Struct()
        private_input.update({"reply_ref": "from-request"})

        request = Request(invocation_token="request-token")
        with request.workflow_manager() as manager:
            response = manager.publish_event(
                workflow_pb2.WorkflowManagerPublishEventRequest(
                    event=workflow_pb2.WorkflowEvent(source="slack", type="event"),
                    private_input=private_input,
                )
            )

        self.assertEqual(response.id, "request-token")
        self.assertEqual(response.data["reply_ref"], "from-request")
