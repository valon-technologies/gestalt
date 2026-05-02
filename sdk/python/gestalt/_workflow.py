from __future__ import annotations

import os
from typing import Any

import grpc

from ._gen.v1 import workflow_pb2 as _pb
from ._gen.v1 import workflow_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel

pb: Any = _pb
pb_grpc: Any = _pb_grpc

ENV_WORKFLOW_HOST_SOCKET = "GESTALT_WORKFLOW_HOST_SOCKET"
ENV_WORKFLOW_HOST_SOCKET_TOKEN = f"{ENV_WORKFLOW_HOST_SOCKET}_TOKEN"
ENV_WORKFLOW_MANAGER_SOCKET = "GESTALT_WORKFLOW_MANAGER_SOCKET"
ENV_WORKFLOW_MANAGER_SOCKET_TOKEN = f"{ENV_WORKFLOW_MANAGER_SOCKET}_TOKEN"

WORKFLOW_RUN_STATUS_UNSPECIFIED = pb.WORKFLOW_RUN_STATUS_UNSPECIFIED
WORKFLOW_RUN_STATUS_PENDING = pb.WORKFLOW_RUN_STATUS_PENDING
WORKFLOW_RUN_STATUS_RUNNING = pb.WORKFLOW_RUN_STATUS_RUNNING
WORKFLOW_RUN_STATUS_SUCCEEDED = pb.WORKFLOW_RUN_STATUS_SUCCEEDED
WORKFLOW_RUN_STATUS_FAILED = pb.WORKFLOW_RUN_STATUS_FAILED
WORKFLOW_RUN_STATUS_CANCELED = pb.WORKFLOW_RUN_STATUS_CANCELED


def BoundWorkflowTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound workflow target protocol value."""

    return pb.BoundWorkflowTarget(*args, **kwargs)


def BoundWorkflowPluginTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound plugin workflow target protocol value."""

    return pb.BoundWorkflowPluginTarget(*args, **kwargs)


def BoundWorkflowAgentTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound agent workflow target protocol value."""

    return pb.BoundWorkflowAgentTarget(*args, **kwargs)


def WorkflowOutputDelivery(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output delivery protocol value."""

    return pb.WorkflowOutputDelivery(*args, **kwargs)


def WorkflowOutputBinding(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output binding protocol value."""

    return pb.WorkflowOutputBinding(*args, **kwargs)


def WorkflowOutputValueSource(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output value source protocol value."""

    return pb.WorkflowOutputValueSource(*args, **kwargs)


def WorkflowActor(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow actor protocol value."""

    return pb.WorkflowActor(*args, **kwargs)


def WorkflowSignal(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow signal protocol value."""

    return pb.WorkflowSignal(*args, **kwargs)


def BoundWorkflowRun(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider run protocol value."""

    return pb.BoundWorkflowRun(*args, **kwargs)


def BoundWorkflowSchedule(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider schedule protocol value."""

    return pb.BoundWorkflowSchedule(*args, **kwargs)


def BoundWorkflowEventTrigger(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider event trigger protocol value."""

    return pb.BoundWorkflowEventTrigger(*args, **kwargs)


def ListWorkflowProviderRunsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-runs response."""

    return pb.ListWorkflowProviderRunsResponse(*args, **kwargs)


def ListWorkflowProviderSchedulesResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-schedules response."""

    return pb.ListWorkflowProviderSchedulesResponse(*args, **kwargs)


def ListWorkflowProviderEventTriggersResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-event-triggers response."""

    return pb.ListWorkflowProviderEventTriggersResponse(*args, **kwargs)


def WorkflowManagerStartRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager start-run request."""

    return pb.WorkflowManagerStartRunRequest(*args, **kwargs)


def WorkflowManagerSignalRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager signal-run request."""

    return pb.WorkflowManagerSignalRunRequest(*args, **kwargs)


def WorkflowManagerSignalOrStartRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager signal-or-start-run request."""

    return pb.WorkflowManagerSignalOrStartRunRequest(*args, **kwargs)


def WorkflowManagerCreateScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager create-schedule request."""

    return pb.WorkflowManagerCreateScheduleRequest(*args, **kwargs)


def WorkflowManagerGetScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager get-schedule request."""

    return pb.WorkflowManagerGetScheduleRequest(*args, **kwargs)


def WorkflowManagerUpdateScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager update-schedule request."""

    return pb.WorkflowManagerUpdateScheduleRequest(*args, **kwargs)


def WorkflowManagerDeleteScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager delete-schedule request."""

    return pb.WorkflowManagerDeleteScheduleRequest(*args, **kwargs)


def WorkflowManagerPauseScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager pause-schedule request."""

    return pb.WorkflowManagerPauseScheduleRequest(*args, **kwargs)


def WorkflowManagerResumeScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager resume-schedule request."""

    return pb.WorkflowManagerResumeScheduleRequest(*args, **kwargs)


def WorkflowManagerCreateEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager create-event-trigger request."""

    return pb.WorkflowManagerCreateEventTriggerRequest(*args, **kwargs)


def WorkflowManagerGetEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager get-event-trigger request."""

    return pb.WorkflowManagerGetEventTriggerRequest(*args, **kwargs)


def WorkflowManagerUpdateEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager update-event-trigger request."""

    return pb.WorkflowManagerUpdateEventTriggerRequest(*args, **kwargs)


def WorkflowManagerDeleteEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager delete-event-trigger request."""

    return pb.WorkflowManagerDeleteEventTriggerRequest(*args, **kwargs)


def WorkflowManagerPauseEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager pause-event-trigger request."""

    return pb.WorkflowManagerPauseEventTriggerRequest(*args, **kwargs)


def WorkflowManagerResumeEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager resume-event-trigger request."""

    return pb.WorkflowManagerResumeEventTriggerRequest(*args, **kwargs)


def WorkflowManagerPublishEventRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager publish-event request."""

    return pb.WorkflowManagerPublishEventRequest(*args, **kwargs)


def InvokeWorkflowOperationRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow host InvokeOperation request."""

    return pb.InvokeWorkflowOperationRequest(*args, **kwargs)


def workflow_run_status_name(status: int) -> str:
    """Return the protocol enum name for a workflow run status value."""

    if not status:
        return ""
    try:
        return pb.WorkflowRunStatus.Name(status)
    except ValueError:
        return str(status)


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
