from __future__ import annotations

import os
from typing import Any

import grpc

from ._agent import _host_service_channel
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
            raise RuntimeError(f"workflow manager: {ENV_WORKFLOW_MANAGER_SOCKET} is not set")
        relay_token = os.environ.get(ENV_WORKFLOW_MANAGER_SOCKET_TOKEN, "")

        self._channel = _host_service_channel("workflow manager", target, token=relay_token)
        self._stub = pb_grpc.WorkflowManagerHostStub(self._channel)
        self._invocation_token = trimmed_token

    def close(self) -> None:
        self._channel.close()

    def create_schedule(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.CreateSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerCreateScheduleRequest),
        )

    def get_schedule(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.GetSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerGetScheduleRequest),
        )

    def update_schedule(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.UpdateSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerUpdateScheduleRequest),
        )

    def delete_schedule(self, request: Any | None = None) -> None:
        _grpc_call(
            self._stub.DeleteSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerDeleteScheduleRequest),
        )

    def pause_schedule(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.PauseSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerPauseScheduleRequest),
        )

    def resume_schedule(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.ResumeSchedule,
            self._with_invocation_token(request, pb.WorkflowManagerResumeScheduleRequest),
        )

    def create_trigger(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.CreateEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerCreateEventTriggerRequest),
        )

    def get_trigger(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.GetEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerGetEventTriggerRequest),
        )

    def update_trigger(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.UpdateEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerUpdateEventTriggerRequest),
        )

    def delete_trigger(self, request: Any | None = None) -> None:
        _grpc_call(
            self._stub.DeleteEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerDeleteEventTriggerRequest),
        )

    def pause_trigger(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.PauseEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerPauseEventTriggerRequest),
        )

    def resume_trigger(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.ResumeEventTrigger,
            self._with_invocation_token(request, pb.WorkflowManagerResumeEventTriggerRequest),
        )

    def publish_event(self, request: Any | None = None) -> Any:
        return _grpc_call(
            self._stub.PublishEvent,
            self._with_invocation_token(request, pb.WorkflowManagerPublishEventRequest),
        )

    def _with_invocation_token(self, request: Any | None, request_type: Any) -> Any:
        value = request_type()
        if request is not None:
            value.CopyFrom(request)
        value.invocation_token = self._invocation_token
        return value

    def __enter__(self) -> WorkflowManager:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
