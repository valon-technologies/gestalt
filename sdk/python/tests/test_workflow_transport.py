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
    ENV_WORKFLOW_MANAGER_SOCKET,
    ENV_WORKFLOW_MANAGER_SOCKET_TOKEN,
    Request,
    WorkflowHost,
    WorkflowManager,
)
from gestalt.gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt.gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc
_server: grpc.Server | None = None
_manager_server: grpc.Server | None = None
_socket_path: str = ""
_manager_socket_path: str = ""
_manager_requests: list[dict[str, str]] = []
_manager_relay_tokens: list[str] = []


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
        _record_manager_relay_tokens(context)
        event = workflow_pb2.WorkflowEvent()
        event.CopyFrom(request.event)
        _manager_requests.append(
            {
                "method": "publish_event",
                "invocation_token": request.invocation_token,
                "event_id": event.id,
                "event_type": event.type,
                "event_source": event.source,
                "event_subject": event.subject,
            }
        )
        if not event.id:
            event.id = "published-event-1"
        return event


def _record_manager_relay_tokens(context: grpc.ServicerContext) -> None:
    _manager_relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def setUpModule() -> None:
    global _server, _manager_server, _socket_path, _manager_socket_path
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-workflow-test-{os.getpid()}.sock"
    )
    _manager_socket_path = os.path.join(
        tempfile.gettempdir(), f"py-workflow-manager-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)
    if os.path.exists(_manager_socket_path):
        os.remove(_manager_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    workflow_pb2_grpc.add_WorkflowHostServicer_to_server(
        _WorkflowHostServicer(), _server
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    _manager_server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    workflow_pb2_grpc.add_WorkflowManagerHostServicer_to_server(
        _WorkflowManagerServicer(), _manager_server
    )
    _manager_server.add_insecure_port(f"unix:{_manager_socket_path}")
    _manager_server.start()

    os.environ[ENV_WORKFLOW_HOST_SOCKET] = _socket_path
    os.environ[ENV_WORKFLOW_MANAGER_SOCKET] = _manager_socket_path
    os.environ[ENV_WORKFLOW_MANAGER_SOCKET_TOKEN] = "relay-token-py"


def tearDownModule() -> None:
    if _server is not None:
        _server.stop(None)
    if _manager_server is not None:
        _manager_server.stop(None)
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)
    if _manager_socket_path and os.path.exists(_manager_socket_path):
        os.remove(_manager_socket_path)


class WorkflowTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _manager_requests.clear()
        _manager_relay_tokens.clear()

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
        event = workflow_pb2.WorkflowEvent(
            id="delivery-123",
            source="github",
            type="github.app.webhook",
            subject="acme/widgets",
            datacontenttype="application/json",
        )
        event.data.update({"github_event": "pull_request", "github_action": "opened"})

        with WorkflowManager("token-123") as manager:
            published = manager.publish_event(
                workflow_pb2.WorkflowManagerPublishEventRequest(event=event)
            )

        self.assertEqual(published.id, "delivery-123")
        self.assertEqual(published.type, "github.app.webhook")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "publish_event",
                    "invocation_token": "token-123",
                    "event_id": "delivery-123",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "acme/widgets",
                }
            ],
        )

    def test_request_workflow_manager_roundtrip(self) -> None:
        request = Request(invocation_token="token-embedded")

        with request.workflow_manager() as manager:
            published = manager.publish_event(
                workflow_pb2.WorkflowManagerPublishEventRequest(
                    event=workflow_pb2.WorkflowEvent(
                        source="github",
                        type="github.app.webhook",
                        subject="installation:99",
                    )
                )
            )

        self.assertEqual(published.id, "published-event-1")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "publish_event",
                    "invocation_token": "token-embedded",
                    "event_id": "",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "installation:99",
                }
            ],
        )
