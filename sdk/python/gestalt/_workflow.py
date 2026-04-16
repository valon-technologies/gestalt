from __future__ import annotations

import os
from typing import Any

import grpc

from .gen.v1 import workflow_pb2 as _pb
from .gen.v1 import workflow_pb2_grpc as _pb_grpc

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_WORKFLOW_SOCKET = "GESTALT_WORKFLOW_SOCKET"
ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET"


class Workflow:
    def __init__(self) -> None:
        socket_path = os.environ.get(ENV_WORKFLOW_SOCKET, "")
        if not socket_path:
            raise RuntimeError(f"{ENV_WORKFLOW_SOCKET} is not set")
        self._channel = grpc.insecure_channel(f"unix:{socket_path}")
        self._stub = pb_grpc.WorkflowStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def start_run(self, request: Any) -> Any:
        return _grpc_call(self._stub.StartRun, request)

    def get_run(self, run_id: str) -> Any:
        return _grpc_call(self._stub.GetRun, pb.GetWorkflowRunRequest(run_id=run_id))

    def list_runs(self) -> list[Any]:
        resp = _grpc_call(self._stub.ListRuns, pb.ListWorkflowRunsRequest())
        return list(resp.runs)

    def cancel_run(self, run_id: str, reason: str = "") -> Any:
        return _grpc_call(
            self._stub.CancelRun,
            pb.CancelWorkflowRunRequest(run_id=run_id, reason=reason),
        )

    def upsert_schedule(self, request: Any) -> Any:
        return _grpc_call(self._stub.UpsertSchedule, request)

    def get_schedule(self, schedule_id: str) -> Any:
        return _grpc_call(
            self._stub.GetSchedule,
            pb.GetWorkflowScheduleRequest(schedule_id=schedule_id),
        )

    def list_schedules(self) -> list[Any]:
        resp = _grpc_call(self._stub.ListSchedules, pb.ListWorkflowSchedulesRequest())
        return list(resp.schedules)

    def delete_schedule(self, schedule_id: str) -> None:
        _grpc_call(
            self._stub.DeleteSchedule,
            pb.DeleteWorkflowScheduleRequest(schedule_id=schedule_id),
        )

    def pause_schedule(self, schedule_id: str) -> Any:
        return _grpc_call(
            self._stub.PauseSchedule,
            pb.PauseWorkflowScheduleRequest(schedule_id=schedule_id),
        )

    def resume_schedule(self, schedule_id: str) -> Any:
        return _grpc_call(
            self._stub.ResumeSchedule,
            pb.ResumeWorkflowScheduleRequest(schedule_id=schedule_id),
        )

    def upsert_event_trigger(self, request: Any) -> Any:
        return _grpc_call(self._stub.UpsertEventTrigger, request)

    def get_event_trigger(self, trigger_id: str) -> Any:
        return _grpc_call(
            self._stub.GetEventTrigger,
            pb.GetWorkflowEventTriggerRequest(trigger_id=trigger_id),
        )

    def list_event_triggers(self) -> list[Any]:
        resp = _grpc_call(
            self._stub.ListEventTriggers,
            pb.ListWorkflowEventTriggersRequest(),
        )
        return list(resp.triggers)

    def delete_event_trigger(self, trigger_id: str) -> None:
        _grpc_call(
            self._stub.DeleteEventTrigger,
            pb.DeleteWorkflowEventTriggerRequest(trigger_id=trigger_id),
        )

    def pause_event_trigger(self, trigger_id: str) -> Any:
        return _grpc_call(
            self._stub.PauseEventTrigger,
            pb.PauseWorkflowEventTriggerRequest(trigger_id=trigger_id),
        )

    def resume_event_trigger(self, trigger_id: str) -> Any:
        return _grpc_call(
            self._stub.ResumeEventTrigger,
            pb.ResumeWorkflowEventTriggerRequest(trigger_id=trigger_id),
        )

    def publish_event(self, event: Any) -> None:
        _grpc_call(
            self._stub.PublishEvent,
            pb.PublishWorkflowEventRequest(event=event),
        )

    def __enter__(self) -> Workflow:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


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
