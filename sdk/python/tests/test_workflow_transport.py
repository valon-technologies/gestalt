"""Transport-backed Workflow SDK tests over a real Unix socket."""

from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    BoundWorkflowTarget,
    Request,
    Workflow,
    WorkflowActivation,
    WorkflowApplyDefinition,
    WorkflowDefinitionSpec,
    WorkflowDeliverEvent,
    WorkflowEvent,
    WorkflowSignal,
    WorkflowSignalOrStartRun,
    WorkflowStartRun,
    WorkflowStep,
    WorkflowStepAppCall,
    WorkflowValue,
)
from gestalt._gen.v1 import app_pb2 as _app_pb2
from gestalt._gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt._gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

app_pb2: Any = _app_pb2
workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc
_server: grpc.Server | None = None
_socket_path: str = ""
_manager_requests: list[dict[str, str]] = []
_manager_contexts: list[dict[str, Any]] = []
_manager_relay_tokens: list[str] = []


class _WorkflowServicer(workflow_pb2_grpc.WorkflowServicer):
    def ApplyDefinition(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _record_manager_context(request)
        spec = request.spec
        _manager_requests.append(
            {
                "method": "apply_definition",
                "idempotency_key": request.idempotency_key,
                "provider": request.provider,
                "definition_id": spec.id,
                "activation_count": str(len(spec.activations)),
            }
        )
        return workflow_pb2.WorkflowDefinition(
            provider=request.provider or "basic",
            id=spec.id or "def-1",
            generation=7,
            target=spec.target,
            activations=spec.activations,
            paused=spec.paused,
        )

    def StartRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _record_manager_context(request)
        _manager_requests.append(
            {
                "method": "start_run",
                "idempotency_key": request.idempotency_key,
                "provider": request.provider,
                "definition_id": request.definition_id,
                "workflow_key": request.workflow_key,
            }
        )
        return workflow_pb2.WorkflowRun(
            provider=request.provider or "basic",
            id="run-1",
            definition_id=request.definition_id,
            workflow_key=request.workflow_key,
            status=workflow_pb2.WORKFLOW_RUN_STATUS_PENDING,
            input=request.input,
        )

    def SignalOrStartRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _record_manager_context(request)
        _manager_requests.append(
            {
                "method": "signal_or_start_run",
                "idempotency_key": request.idempotency_key,
                "provider": request.provider,
                "definition_id": request.definition_id,
                "workflow_key": request.workflow_key,
                "signal_name": request.signal.name,
            }
        )
        return workflow_pb2.SignalWorkflowRunResponse(
            run=workflow_pb2.WorkflowRun(
                provider=request.provider or "basic",
                id="run-signal",
                definition_id=request.definition_id,
                workflow_key=request.workflow_key,
                status=workflow_pb2.WORKFLOW_RUN_STATUS_RUNNING,
                input=request.input,
            ),
            signal=request.signal,
            started_run=True,
            workflow_key=request.workflow_key,
        )

    def DeliverEvent(self, request: Any, context: grpc.ServicerContext) -> Any:
        _record_manager_relay_tokens(context)
        _record_manager_context(request)
        event = workflow_pb2.WorkflowEvent()
        event.CopyFrom(request.event)
        _manager_requests.append(
            {
                "method": "deliver_event",
                "provider": request.provider,
                "event_id": event.id,
                "event_type": event.type,
                "event_source": event.source,
                "event_subject": event.subject,
            }
        )
        if not event.id:
            event.id = "delivered-event-1"
        return event


def _record_manager_relay_tokens(context: grpc.ServicerContext) -> None:
    _manager_relay_tokens.extend(
        value
        for key, value in context.invocation_metadata()
        if key == "x-gestalt-host-service-relay-token"
    )


def _record_manager_context(request: Any) -> None:
    _manager_contexts.append(
        {
            "subject_id": request.context.subject.id,
        }
        if request.HasField("context")
        else {}
    )


def setUpModule() -> None:
    global _server, _socket_path
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-workflow-test-{os.getpid()}.sock"
    )
    if os.path.exists(_socket_path):
        os.remove(_socket_path)

    _server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    workflow_pb2_grpc.add_WorkflowServicer_to_server(
        _WorkflowServicer(), _server
    )
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    os.environ[ENV_HOST_SERVICE_SOCKET] = _socket_path
    os.environ[ENV_HOST_SERVICE_TOKEN] = "relay-token-py"


def tearDownModule() -> None:
    if _server is not None:
        _server.stop(None)
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class WorkflowTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        _manager_requests.clear()
        _manager_contexts.clear()
        _manager_relay_tokens.clear()

    def test_workflow_deliver_event_roundtrip(self) -> None:
        event = workflow_pb2.WorkflowEvent(
            id="delivery-123",
            source="github",
            type="github.app.webhook",
            subject="acme/widgets",
            datacontenttype="application/json",
        )
        event.data.update({"github_event": "pull_request", "github_action": "opened"})

        with Workflow(
            Request(
                context=app_pb2.RequestContext(
                    subject=app_pb2.SubjectContext(id="user:workflow-direct")
                )
            )
        ) as manager:
            delivered = manager.deliver_event(
                WorkflowDeliverEvent(
                    provider="advanced",
                    event=event,
                )
            )

        assert delivered is not None
        self.assertEqual(delivered.id, "delivery-123")
        self.assertEqual(delivered.type, "github.app.webhook")
        self.assertEqual(_manager_relay_tokens, ["relay-token-py"])
        self.assertEqual(_manager_contexts, [{"subject_id": "user:workflow-direct"}])
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "deliver_event",
                    "provider": "advanced",
                    "event_id": "delivery-123",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "acme/widgets",
                }
            ],
        )

    def test_request_workflow_roundtrip(self) -> None:
        request = Request(
            context=app_pb2.RequestContext(
                subject=app_pb2.SubjectContext(id="user:workflow-request")
            ),
            idempotency_key="workflow-request-key-py",
        )

        with request.workflows() as manager:
            definition = manager.apply_definition(
                WorkflowApplyDefinition(
                    provider="managed",
                    spec=WorkflowDefinitionSpec(
                        id="def-managed",
                        target=BoundWorkflowTarget(
                            steps=[
                                WorkflowStep(
                                    id="sync",
                                    app=WorkflowStepAppCall(
                                        name="demo",
                                        operation="sync",
                                        input=WorkflowValue(input="repository"),
                                    ),
                                )
                            ],
                        ),
                        activations=[
                            WorkflowActivation(
                                id="github",
                                event={
                                    "match": {
                                        "type": "github.app.webhook",
                                        "source": "github",
                                    }
                                },
                                input=WorkflowValue(signal="data"),
                            )
                        ],
                    ),
                )
            )
            run = manager.start_run(
                WorkflowStartRun(
                    provider="managed",
                    definition_id="def-managed",
                    workflow_key="repo:valon/app",
                    input={"repository": "valon/app"},
                )
            )
            signal = manager.signal_or_start_run(
                WorkflowSignalOrStartRun(
                    provider="managed",
                    definition_id="def-managed",
                    workflow_key="repo:valon/app",
                    signal=WorkflowSignal(name="github", payload={"state": "opened"}),
                    input={"repository": "valon/app"},
                )
            )
            delivered = manager.deliver_event(
                WorkflowDeliverEvent(
                    provider="managed",
                    event=WorkflowEvent(
                        source="github",
                        type="github.app.webhook",
                        subject="installation:99",
                    ),
                )
            )

        assert signal.run is not None
        assert delivered is not None
        self.assertEqual(definition.id, "def-managed")
        self.assertEqual(definition.generation, 7)
        self.assertEqual(run.id, "run-1")
        self.assertEqual(signal.run.id, "run-signal")
        self.assertEqual(delivered.id, "delivered-event-1")
        self.assertEqual(
            _manager_relay_tokens,
            ["relay-token-py", "relay-token-py", "relay-token-py", "relay-token-py"],
        )
        self.assertEqual(
            _manager_contexts,
            [{"subject_id": "user:workflow-request"}] * 4,
        )
        self.assertEqual(
            _manager_requests,
            [
                {
                    "method": "apply_definition",
                    "idempotency_key": "workflow-request-key-py",
                    "provider": "managed",
                    "definition_id": "def-managed",
                    "activation_count": "1",
                },
                {
                    "method": "start_run",
                    "idempotency_key": "workflow-request-key-py",
                    "provider": "managed",
                    "definition_id": "def-managed",
                    "workflow_key": "repo:valon/app",
                },
                {
                    "method": "signal_or_start_run",
                    "idempotency_key": "workflow-request-key-py",
                    "provider": "managed",
                    "definition_id": "def-managed",
                    "workflow_key": "repo:valon/app",
                    "signal_name": "github",
                },
                {
                    "method": "deliver_event",
                    "provider": "managed",
                    "event_id": "",
                    "event_type": "github.app.webhook",
                    "event_source": "github",
                    "event_subject": "installation:99",
                },
            ],
        )
