"""Transport-backed Workflow SDK tests over a real Unix socket."""
from __future__ import annotations

import os
import tempfile
import unittest
from concurrent import futures
from typing import Any

import grpc
from google.protobuf import empty_pb2

from gestalt import (
    ENV_WORKFLOW_HOST_SOCKET,
    ENV_WORKFLOW_SOCKET,
    Workflow,
    WorkflowHost,
)
from gestalt.gen.v1 import workflow_pb2 as _workflow_pb2
from gestalt.gen.v1 import workflow_pb2_grpc as _workflow_pb2_grpc

workflow_pb2: Any = _workflow_pb2
workflow_pb2_grpc: Any = _workflow_pb2_grpc

_server: grpc.Server | None = None
_socket_path: str = ""
_published_events: list[str] = []


class _WorkflowServicer(workflow_pb2_grpc.WorkflowServicer):
    def StartRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowRun(
            id=request.idempotency_key or "run-1",
            status=workflow_pb2.WORKFLOW_RUN_STATUS_PENDING,
            target=request.target,
        )

    def GetRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowRun(
            id=request.run_id,
            status=workflow_pb2.WORKFLOW_RUN_STATUS_RUNNING,
        )

    def ListRuns(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.ListWorkflowRunsResponse(
            runs=[
                workflow_pb2.WorkflowRun(
                    id="run-1",
                    status=workflow_pb2.WORKFLOW_RUN_STATUS_PENDING,
                )
            ]
        )

    def CancelRun(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowRun(
            id=request.run_id,
            status=workflow_pb2.WORKFLOW_RUN_STATUS_CANCELED,
            status_message=request.reason,
        )

    def UpsertSchedule(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowSchedule(
            id=request.schedule_id or "schedule-1",
            cron=request.cron,
            timezone=request.timezone,
            target=request.target,
            paused=request.paused,
        )

    def GetSchedule(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowSchedule(
            id=request.schedule_id,
            cron="*/5 * * * *",
            timezone="UTC",
        )

    def ListSchedules(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.ListWorkflowSchedulesResponse(
            schedules=[
                workflow_pb2.WorkflowSchedule(
                    id="schedule-1",
                    cron="*/5 * * * *",
                    timezone="UTC",
                )
            ]
        )

    def DeleteSchedule(self, request: Any, context: grpc.ServicerContext) -> Any:
        return empty_pb2.Empty()

    def PauseSchedule(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowSchedule(id=request.schedule_id, paused=True)

    def ResumeSchedule(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowSchedule(id=request.schedule_id, paused=False)

    def UpsertEventTrigger(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowEventTrigger(
            id=request.trigger_id or "trigger-1",
            match=request.match,
            target=request.target,
            paused=request.paused,
        )

    def GetEventTrigger(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowEventTrigger(
            id=request.trigger_id,
            match=workflow_pb2.WorkflowEventMatch(type="demo.refresh"),
        )

    def ListEventTriggers(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.ListWorkflowEventTriggersResponse(
            triggers=[
                workflow_pb2.WorkflowEventTrigger(
                    id="trigger-1",
                    match=workflow_pb2.WorkflowEventMatch(type="demo.refresh"),
                )
            ]
        )

    def DeleteEventTrigger(self, request: Any, context: grpc.ServicerContext) -> Any:
        return empty_pb2.Empty()

    def PauseEventTrigger(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowEventTrigger(id=request.trigger_id, paused=True)

    def ResumeEventTrigger(self, request: Any, context: grpc.ServicerContext) -> Any:
        return workflow_pb2.WorkflowEventTrigger(id=request.trigger_id, paused=False)

    def PublishEvent(self, request: Any, context: grpc.ServicerContext) -> Any:
        event = request.event
        if event is not None:
            _published_events.append(str(event.type))
        return empty_pb2.Empty()


class _WorkflowHostServicer(workflow_pb2_grpc.WorkflowHostServicer):
    def InvokeOperation(self, request: Any, context: grpc.ServicerContext) -> Any:
        target = request.target
        operation = target.operation if target is not None else ""
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
    workflow_pb2_grpc.add_WorkflowServicer_to_server(_WorkflowServicer(), _server)
    workflow_pb2_grpc.add_WorkflowHostServicer_to_server(_WorkflowHostServicer(), _server)
    _server.add_insecure_port(f"unix:{_socket_path}")
    _server.start()

    os.environ[ENV_WORKFLOW_SOCKET] = _socket_path
    os.environ[ENV_WORKFLOW_HOST_SOCKET] = _socket_path


def tearDownModule() -> None:
    if _server is not None:
        _server.stop(None)
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


class WorkflowTransportTests(unittest.TestCase):
    def test_workflow_client_roundtrip(self) -> None:
        _published_events.clear()
        with Workflow() as client:
            started = client.start_run(
                workflow_pb2.StartWorkflowRunRequest(
                    target=workflow_pb2.WorkflowTarget(
                        operation="sync",
                    ),
                    idempotency_key="run-42",
                )
            )
            self.assertEqual(started.id, "run-42")

            fetched = client.get_run("run-42")
            self.assertEqual(fetched.id, "run-42")

            listed = client.list_runs()
            self.assertEqual([run.id for run in listed], ["run-1"])

            canceled = client.cancel_run("run-42", reason="stop")
            self.assertEqual(canceled.status_message, "stop")

            schedule = client.upsert_schedule(
                workflow_pb2.UpsertWorkflowScheduleRequest(
                    schedule_id="schedule-1",
                    cron="*/5 * * * *",
                    timezone="UTC",
                    target=workflow_pb2.WorkflowTarget(operation="sync"),
                )
            )
            self.assertEqual(schedule.id, "schedule-1")

            self.assertEqual(client.get_schedule("schedule-1").id, "schedule-1")
            self.assertEqual(
                [item.id for item in client.list_schedules()],
                ["schedule-1"],
            )
            self.assertTrue(client.pause_schedule("schedule-1").paused)
            self.assertFalse(client.resume_schedule("schedule-1").paused)
            client.delete_schedule("schedule-1")

            trigger = client.upsert_event_trigger(
                workflow_pb2.UpsertWorkflowEventTriggerRequest(
                    trigger_id="trigger-1",
                    match=workflow_pb2.WorkflowEventMatch(type="demo.refresh"),
                    target=workflow_pb2.WorkflowTarget(operation="sync"),
                )
            )
            self.assertEqual(trigger.id, "trigger-1")

            self.assertEqual(client.get_event_trigger("trigger-1").id, "trigger-1")
            self.assertEqual(
                [item.id for item in client.list_event_triggers()],
                ["trigger-1"],
            )
            self.assertTrue(client.pause_event_trigger("trigger-1").paused)
            self.assertFalse(client.resume_event_trigger("trigger-1").paused)
            client.delete_event_trigger("trigger-1")

            client.publish_event(
                workflow_pb2.WorkflowEvent(
                    id="evt-1",
                    source="urn:test",
                    spec_version="1.0",
                    type="demo.refresh",
                )
            )

        self.assertEqual(_published_events, ["demo.refresh"])

    def test_workflow_host_roundtrip(self) -> None:
        with WorkflowHost() as host:
            response = host.invoke_operation(
                workflow_pb2.InvokeWorkflowOperationRequest(
                    run_id="run-42",
                    plugin_name="demo",
                    target=workflow_pb2.BoundWorkflowTarget(
                        plugin_name="demo",
                        operation="sync",
                    ),
                )
            )
        self.assertEqual(response.status, 202)
        self.assertEqual(response.body, "run-42:sync")
