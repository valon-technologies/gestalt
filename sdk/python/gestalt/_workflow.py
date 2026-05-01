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
ENV_WORKFLOW_HOST_SOCKET_TOKEN = f"{ENV_WORKFLOW_HOST_SOCKET}_TOKEN"
ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET"
ENV_WORKFLOW_MANAGER_SOCKET_TOKEN = f"{ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN"


class WorkflowHost:
    """Client for the workflow host service available inside workflow code.

    ``WorkflowHost`` reads ``GESTALT_WORKFLOW_HOST_SOCKET`` and its optional
    relay token from the environment, then exposes the host operation-invocation
    RPC used by workflow providers.
    """

    def __init__(self) -> None:
        target = os.environ.get(ENV_WORKFLOW_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_WORKFLOW_HOST_SOCKET} is not set")
        relay_token = os.environ.get(ENV_WORKFLOW_HOST_SOCKET_TOKEN, "")
        self._channel = host_service_channel("workflow host", target, token=relay_token)
        self._stub = pb_grpc.WorkflowHostStub(self._channel)

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def invoke_operation(self, request: Any) -> Any:
        """Invoke an operation through the workflow host."""

        return _grpc_call(self._stub.InvokeOperation, request)

    def __enter__(self) -> WorkflowHost:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


class WorkflowManager:
    """Client for starting runs and managing workflow schedules or triggers.

    The manager is for provider code that receives an invocation token. Methods
    attach that token to each request before calling the host service. The
    optional ``idempotency_key`` is used for create requests that do not already
    include one.
    """

    def __init__(self, invocation_token: str, *, idempotency_key: str = "") -> None:
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
        self._idempotency_key = idempotency_key.strip()

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def start_run(self, request: Any) -> Any:
        """Start a workflow run."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.StartRun, request)

    def signal_run(self, request: Any) -> Any:
        """Signal an existing workflow run."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.SignalRun, request)

    def signal_or_start_run(self, request: Any) -> Any:
        """Signal a run, or start it when no matching run exists."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.SignalOrStartRun, request)

    def create_schedule(self, request: Any) -> Any:
        """Create a workflow schedule."""

        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return _grpc_call(self._stub.CreateSchedule, request)

    def get_schedule(self, request: Any) -> Any:
        """Fetch one workflow schedule."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetSchedule, request)

    def update_schedule(self, request: Any) -> Any:
        """Update a workflow schedule."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.UpdateSchedule, request)

    def delete_schedule(self, request: Any) -> Any:
        """Delete a workflow schedule."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.DeleteSchedule, request)

    def pause_schedule(self, request: Any) -> Any:
        """Pause a workflow schedule."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.PauseSchedule, request)

    def resume_schedule(self, request: Any) -> Any:
        """Resume a workflow schedule."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ResumeSchedule, request)

    def create_trigger(self, request: Any) -> Any:
        """Create an event trigger."""

        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return _grpc_call(self._stub.CreateEventTrigger, request)

    def get_trigger(self, request: Any) -> Any:
        """Fetch one event trigger."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.GetEventTrigger, request)

    def update_trigger(self, request: Any) -> Any:
        """Update an event trigger."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.UpdateEventTrigger, request)

    def delete_trigger(self, request: Any) -> Any:
        """Delete an event trigger."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.DeleteEventTrigger, request)

    def pause_trigger(self, request: Any) -> Any:
        """Pause an event trigger."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.PauseEventTrigger, request)

    def resume_trigger(self, request: Any) -> Any:
        """Resume an event trigger."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.ResumeEventTrigger, request)

    def publish_event(self, request: Any) -> Any:
        """Publish an event into the workflow host."""

        request.invocation_token = self._invocation_token
        return _grpc_call(self._stub.PublishEvent, request)

    def __enter__(self) -> WorkflowManager:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
