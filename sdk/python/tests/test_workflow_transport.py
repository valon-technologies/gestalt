"""Transport-backed Workflow SDK tests over a real Unix socket."""

from __future__ import annotations

import dataclasses
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
    BoundWorkflowTarget,
    Request,
    WorkflowActivation,
    WorkflowDeploymentSpec,
    WorkflowEvent,
    WorkflowHost,
    WorkflowManager,
    WorkflowManagerApplyDeploymentRequest,
    WorkflowManagerDeliverEventRequest,
    WorkflowStep,
    WorkflowStepPluginCall,
)
from gestalt._gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt._gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc
_server: grpc.Server | None = None
_manager_server: grpc.Server | None = None
_socket_path: str = ""
_manager_socket_path: str = ""
_manager_requests: list[dict[str, str]] = []
_manager_relay_tokens: list[str] = []


@dataclasses.dataclass(slots=True)
class _InvokeActionRequestInput:
    selector: dict[str, str]
    plugin: dict[str, dict[str, str]]


class _WorkflowHostServicer(workflow_pb2_grpc.WorkflowHostServicer):
    def InvokeWorkflowAction(self, request: Any, context: grpc.ServicerContext) -> Any:
        operation = request.plugin.input.fields["operation"].string_value
        return workflow_pb2.WorkflowActionResult(
            status=202,
            body=f"{request.selector.run_id}:{operation}",
        )


class _WorkflowManagerServicer(workflow_pb2_grpc.WorkflowManagerHostServicer):
    def ApplyDeployment(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "apply_deployment",
                "invocation_token": request.invocation_token,
                "idempotency_key": request.idempotency_key,
                "provider_name": request.provider_name,
                "deployment_id": request.spec.id,
            }
        )
        return workflow_pb2.ManagedWorkflowDeployment(
            provider_name=request.provider_name or "basic",
            deployment=workflow_pb2.WorkflowDeployment(
                spec=request.spec,
                status=workflow_pb2.WORKFLOW_DEPLOYMENT_STATUS_ACTIVE,
            ),
        )

    def StartRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _manager_requests.append(
            {
                "method": "start_run",
                "invocation_token": request.invocation_token,
                "idempotency_key": request.idempotency_key,
                "provider_name": request.provider_name,
                "deployment_id": request.deployment_id,
                "workflow_key": request.workflow_key,
            }
        )
        return workflow_pb2.ManagedWorkflowRun(
            provider_name=request.provider_name or "basic",
            run=workflow_pb2.WorkflowRun(
                id="run-1",
                deployment_id=request.deployment_id,
                workflow_key=request.workflow_key,
                status=workflow_pb2.WORKFLOW_RUN_STATUS_RUNNING,
            ),
        )

    def DeliverEvent(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        event = workflow_pb2.WorkflowEvent()
        event.CopyFrom(request.event)
        _manager_requests.append(
            {
                "method": "deliver_event",
                "invocation_token": request.invocation_token,
                "idempotency_key": request.idempotency_key,
                "event_id": event.id,
                "event_type": event.type,
                "event_source": event.source,
                "event_subject": event.subject,
                "provider_name": request.provider_name,
            }
        )
        return workflow_pb2.WorkflowManagerDeliverEventResponse(
            results=[
                workflow_pb2.WorkflowEventDeliveryResult(
                    deployment_id="deployment-1",
                    activation_id="event",
                    started_run=True,
                    run=workflow_pb2.WorkflowRun(id="run-from-event"),
                )
            ]
        )


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
            response = host.invoke_action(
                _InvokeActionRequestInput(
                    selector={"run_id": "run-42"},
                    plugin={"input": {"operation": "sync"}},
                )
            )
        self.assertEqual(response.status, 202)
        self.assertEqual(response.body, "run-42:sync")

    def test_workflow_manager_deliver_event_roundtrip(self) -> None:
        event = workflow_pb2.WorkflowEvent(
            id="delivery-123",
            source="github",
            type="github.app.webhook",
            subject="acme/widgets",
            datacontenttype="application/json",
        )
        event.data.update({"github_event": "pull_request", "github_action": "opened"})

        with WorkflowManager("token-123") as manager:
            delivered = manager.deliver_event(
                workflow_pb2.WorkflowManagerDeliverEventRequest(
                    event=event,
                    provider_name="advanced",
                    idempotency_key="delivery-key",
                )
            )

        results = delivered.results
        assert results is not None
        self.assertEqual(results[0].run.id, "run-from-event")
        self.assertTrue(results[0].started_run)
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "deliver_event",
                    "invocation_token": "token-123",
                    "idempotency_key": "delivery-key",
                    "event_id": "delivery-123",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "acme/widgets",
                    "provider_name": "advanced",
                }
            ],
        )

    def test_request_workflow_manager_roundtrip(self) -> None:
        request = Request(
            invocation_token="token-embedded",
            idempotency_key="workflow-request-key-py",
        )

        with request.workflow_manager() as manager:
            deployment = manager.apply_deployment(
                WorkflowManagerApplyDeploymentRequest(
                    provider_name="managed",
                    spec=WorkflowDeploymentSpec(
                        id="deployment-1",
                        target=BoundWorkflowTarget(
                            steps=[
                                WorkflowStep(
                                    id="sync",
                                    plugin=WorkflowStepPluginCall(
                                        name="demo",
                                        operation="sync",
                                    ),
                                )
                            ]
                        ),
                        activations=[WorkflowActivation(id="manual", manual=True)],
                    ),
                )
            )
            run = manager.start_run(
                provider_name="managed",
                deployment_id="deployment-1",
                workflow_key="sync",
            )
            delivered = manager.deliver_event(
                WorkflowManagerDeliverEventRequest(
                    provider_name="managed",
                    event=WorkflowEvent(
                        source="github",
                        type="github.app.webhook",
                        subject="installation:99",
                    ),
                )
            )

        assert deployment.deployment is not None
        assert run.run is not None
        self.assertEqual(deployment.deployment.spec.id, "deployment-1")
        self.assertEqual(run.run.id, "run-1")
        results = delivered.results
        assert results is not None
        self.assertEqual(results[0].run.id, "run-from-event")
        self.assertEqual(
            _manager_relay_tokens,
            ["relay-token-py", "relay-token-py", "relay-token-py"],
        )
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "apply_deployment",
                    "invocation_token": "token-embedded",
                    "idempotency_key": "workflow-request-key-py",
                    "provider_name": "managed",
                    "deployment_id": "deployment-1",
                },
                {
                    "method": "start_run",
                    "invocation_token": "token-embedded",
                    "idempotency_key": "workflow-request-key-py",
                    "provider_name": "managed",
                    "deployment_id": "deployment-1",
                    "workflow_key": "sync",
                },
                {
                    "method": "deliver_event",
                    "invocation_token": "token-embedded",
                    "idempotency_key": "workflow-request-key-py",
                    "event_id": "",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "installation:99",
                    "provider_name": "managed",
                },
            ],
        )
