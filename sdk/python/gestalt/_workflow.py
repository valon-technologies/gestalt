from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import os
from collections.abc import Mapping, Sequence
from typing import Any

import grpc
from google.protobuf import message as _message

from ._agent import agent_message_from_dict, agent_tool_ref_from_dict
from ._gen.v1 import agent_pb2 as _agent_pb
from ._gen.v1 import workflow_pb2 as _pb
from ._gen.v1 import workflow_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel
from ._protocol import (
    copy_message as _copy,
)
from ._protocol import (
    dataclass_mapping as _dataclass_mapping,
)
from ._protocol import (
    datetime_from_timestamp,
    has_field,
    struct_from_dict,
    struct_to_dict,
    timestamp_from_datetime,
    value_from_json,
    value_to_json,
    which_oneof,
)
from ._protocol import (
    input_data as _data,
)
from ._protocol import (
    struct_pb2 as _struct_pb2,
)
from ._protocol import (
    timestamp_pb2 as _timestamp_pb2,
)

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


_MISSING = object()


@_dataclasses.dataclass(slots=True)
class BoundWorkflowPluginTargetInput:
    """Input for a bound plugin workflow target."""

    plugin_name: str = ""
    operation: str = ""
    input: Any | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowOutputValueSourceInput:
    """Input for a workflow output value source."""

    agent_output: str | None = None
    signal_payload: str | None = None
    signal_metadata: str | None = None
    literal: Any = _MISSING
    agent_session: str | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowOutputBindingInput:
    """Input for one workflow output binding."""

    input_field: str = ""
    value: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowOutputDeliveryInput:
    """Input for a workflow output delivery."""

    target: Any | None = None
    input_bindings: Sequence[Any] | None = None
    credential_mode: str = ""


@_dataclasses.dataclass(slots=True)
class BoundWorkflowAgentTargetInput:
    """Input for a bound agent workflow target."""

    provider_name: str = ""
    model: str = ""
    prompt: str = ""
    messages: Sequence[Any] | None = None
    tool_refs: Sequence[Any] | None = None
    response_schema: Any | None = None
    metadata: Any | None = None
    timeout_seconds: int = 0
    output_delivery: Any | None = None
    model_options: Any | None = None
    session_ready_delivery: Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowTargetInput:
    """Input for a bound workflow target."""

    plugin: Any | None = None
    agent: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActorInput:
    """Input for a workflow actor."""

    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowEventInput:
    """Input for a workflow event."""

    id: str = ""
    source: str = ""
    spec_version: str = ""
    type: str = ""
    subject: str = ""
    time: _dt.datetime | Any | None = None
    datacontenttype: str = ""
    data: Any | None = None
    extensions: Mapping[str, Any] | None = None


WorkflowManagerPublishedEvent = WorkflowEventInput


@_dataclasses.dataclass(slots=True)
class WorkflowEventMatchInput:
    """Input for workflow event matching fields."""

    type: str = ""
    source: str = ""
    subject: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowSignalInput:
    """Input for a workflow signal."""

    id: str = ""
    name: str = ""
    payload: Any | None = None
    metadata: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None
    idempotency_key: str = ""
    sequence: int = 0


@_dataclasses.dataclass(slots=True)
class WorkflowScheduleTriggerInput:
    """Input for a schedule-triggered workflow run."""

    schedule_id: str = ""
    scheduled_for: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowEventTriggerInvocationInput:
    """Input for an event-triggered workflow run."""

    trigger_id: str = ""
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunTriggerInput:
    """Input for a workflow run trigger."""

    manual: bool = False
    schedule: Any | None = None
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowRunInput:
    """Input for a workflow-provider run."""

    id: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    target: Any | None = None
    trigger: Any | None = None
    created_at: _dt.datetime | Any | None = None
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None
    status_message: str = ""
    result_body: str = ""
    created_by: Any | None = None
    execution_ref: str = ""
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class BoundWorkflowDefinitionInput:
    """Input copied from a workflow-provider definition."""

    id: str = ""
    target: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowScheduleInput:
    """Input for a workflow-provider schedule."""

    id: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    next_run_at: _dt.datetime | Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class BoundWorkflowEventTriggerInput:
    """Input for a workflow-provider event trigger."""

    id: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowAccessPermissionInput:
    """Input for an execution-reference permission."""

    plugin: str = ""
    operations: Sequence[str] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunAsSubjectInput:
    """Input for a workflow run-as subject."""

    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowExecutionReferenceInput:
    """Input for a workflow execution reference."""

    id: str = ""
    provider_name: str = ""
    target: Any | None = None
    subject_id: str = ""
    credential_subject_id: str = ""
    permissions: Sequence[Any] | None = None
    created_at: _dt.datetime | Any | None = None
    revoked_at: _dt.datetime | Any | None = None
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""
    caller_plugin_name: str = ""
    run_as: Any | None = None
    source_definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerStartRunInput:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    workflow_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalRunInput:
    run_id: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalOrStartRunInput:
    provider_name: str = ""
    workflow_key: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    signal: Any | None = None
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateDefinitionInput:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetDefinitionInput:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateDefinitionInput:
    definition_id: str = ""
    provider_name: str = ""
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteDefinitionInput:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateScheduleInput:
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetScheduleInput:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateScheduleInput:
    schedule_id: str = ""
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteScheduleInput:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPauseScheduleInput:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerResumeScheduleInput:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateEventTriggerInput:
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetEventTriggerInput:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateEventTriggerInput:
    trigger_id: str = ""
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteEventTriggerInput:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPauseEventTriggerInput:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerResumeEventTriggerInput:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPublishEventInput:
    provider_name: str = ""
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerBoundRun:
    id: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    target: Any | None = None
    trigger: Any | None = None
    created_at: _dt.datetime | Any | None = None
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None
    status_message: str = ""
    result_body: str = ""
    created_by: Any | None = None
    execution_ref: str = ""
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerBoundDefinition:
    id: str = ""
    target: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerBoundSchedule:
    id: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    next_run_at: _dt.datetime | Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerBoundEventTrigger:
    id: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerRun:
    provider_name: str = ""
    run: WorkflowManagerBoundRun | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerRunSignal:
    provider_name: str = ""
    run: WorkflowManagerBoundRun | None = None
    signal: WorkflowSignalInput | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDefinition:
    provider_name: str = ""
    definition: WorkflowManagerBoundDefinition | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSchedule:
    provider_name: str = ""
    schedule: WorkflowManagerBoundSchedule | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerEventTrigger:
    provider_name: str = ""
    trigger: WorkflowManagerBoundEventTrigger | None = None


@_dataclasses.dataclass(slots=True)
class InvokeWorkflowOperationInput:
    """Input for invoking a workflow operation through the host."""

    target: Any | None = None
    run_id: str = ""
    trigger: Any | None = None
    input: Any | None = None
    metadata: Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""
    signals: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class InvokeWorkflowOperationResponse:
    """Native response returned after invoking a workflow operation."""

    status: int = 0
    body: str = ""


def _optional_struct(value: Any | None) -> Any | None:
    if value is None:
        return None
    if isinstance(value, _struct_pb2.Struct):
        return _copy(value)
    return struct_from_dict(value)


def _optional_value(value: Any | None) -> Any | None:
    if value is None:
        return None
    return _value(value)


def _value(value: Any) -> Any:
    if isinstance(value, _struct_pb2.Value):
        return _copy(value)
    return value_from_json(value)


def _optional_timestamp(value: _dt.datetime | Any | None) -> Any | None:
    if value is None:
        return None
    if isinstance(value, _timestamp_pb2.Timestamp):
        return _copy(value)
    if isinstance(value, _dt.datetime):
        return timestamp_from_datetime(value)
    raise TypeError(f"expected datetime or Timestamp, got {type(value).__name__}")


def _timestamp_to_datetime(value: Any, field: str) -> _dt.datetime | None:
    return (
        datetime_from_timestamp(getattr(value, field))
        if has_field(value, field)
        else None
    )


def _message_list(values: Sequence[Any] | None, message_type: type[Any]) -> list[Any]:
    if values is None:
        return []
    output = []
    for item in values:
        if isinstance(item, _message.Message):
            output.append(_copy(item))
        else:
            mapping = _dataclass_mapping(item)
            if mapping is None:
                if not isinstance(item, Mapping):
                    raise TypeError(
                        f"expected protobuf message, mapping, or dataclass, got {type(item).__name__}"
                    )
                mapping = dict(item)
            if message_type is _agent_pb.AgentMessage:
                output.append(agent_message_from_dict(mapping))
            elif message_type is _agent_pb.AgentToolRef:
                output.append(agent_tool_ref_from_dict(mapping))
            else:
                output.append(message_type(**mapping))
    return output


def workflow_actor(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow actor ."""

    if isinstance(value, pb.WorkflowActor):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowActor(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
    )


def workflow_actor_input_from_actor(value: Any | None) -> WorkflowActorInput | None:
    """Return input copied from a workflow actor."""

    if value is None:
        return None
    return WorkflowActorInput(
        subject_id=value.subject_id,
        subject_kind=value.subject_kind,
        display_name=value.display_name,
        auth_source=value.auth_source,
    )


def workflow_run_as_subject(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow run-as subject ."""

    if isinstance(value, pb.WorkflowRunAsSubject):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunAsSubject(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
    )


def workflow_run_as_subject_input_from_subject(
    value: Any | None,
) -> WorkflowRunAsSubjectInput | None:
    """Return input copied from a workflow run-as subject."""

    if value is None:
        return None
    return WorkflowRunAsSubjectInput(
        subject_id=value.subject_id,
        subject_kind=value.subject_kind,
        display_name=value.display_name,
        auth_source=value.auth_source,
    )


def workflow_access_permission(value: Any | None = None, **kwargs: Any) -> Any:
    """Create an execution-reference permission ."""

    if isinstance(value, pb.WorkflowAccessPermission):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowAccessPermission(
        plugin=data.get("plugin", ""),
        operations=list(data.get("operations") or []),
    )


def workflow_access_permission_input_from_permission(
    value: Any,
) -> WorkflowAccessPermissionInput:
    """Return input copied from an execution-reference permission."""

    return WorkflowAccessPermissionInput(
        plugin=value.plugin,
        operations=list(value.operations),
    )


def workflow_event_match(value: Any | None = None, **kwargs: Any) -> Any:
    """Create workflow event-match fields ."""

    if isinstance(value, pb.WorkflowEventMatch):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowEventMatch(
        type=data.get("type", ""),
        source=data.get("source", ""),
        subject=data.get("subject", ""),
    )


def workflow_event_match_input_from_match(
    value: Any | None,
) -> WorkflowEventMatchInput | None:
    """Return input copied from workflow event-match fields."""

    if value is None:
        return None
    return WorkflowEventMatchInput(
        type=value.type, source=value.source, subject=value.subject
    )


def workflow_output_value_source(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow output value source ."""

    if isinstance(value, pb.WorkflowOutputValueSource):
        return _copy(value)
    data = _data(value, kwargs)
    literal = data.get("literal", _MISSING)
    choices = [
        ("agent_output", data.get("agent_output")),
        ("signal_payload", data.get("signal_payload")),
        ("signal_metadata", data.get("signal_metadata")),
        ("agent_session", data.get("agent_session")),
    ]
    selected = [(name, item) for name, item in choices if item is not None]
    if literal is not _MISSING:
        selected.append(("literal", literal))
    if not selected:
        return pb.WorkflowOutputValueSource()
    if len(selected) > 1:
        raise ValueError("workflow output value source must set exactly one source")
    name, item = selected[0]
    if name == "literal":
        return pb.WorkflowOutputValueSource(literal=_value(item))
    return pb.WorkflowOutputValueSource(**{name: item})


def workflow_output_value_source_input_from_source(
    value: Any | None,
) -> WorkflowOutputValueSourceInput | None:
    """Return input copied from a workflow output value source."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "agent_output":
        return WorkflowOutputValueSourceInput(agent_output=value.agent_output)
    if kind == "signal_payload":
        return WorkflowOutputValueSourceInput(signal_payload=value.signal_payload)
    if kind == "signal_metadata":
        return WorkflowOutputValueSourceInput(signal_metadata=value.signal_metadata)
    if kind == "agent_session":
        return WorkflowOutputValueSourceInput(agent_session=value.agent_session)
    if kind == "literal":
        return WorkflowOutputValueSourceInput(literal=value_to_json(value.literal))
    return WorkflowOutputValueSourceInput()


def workflow_output_binding(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow output binding ."""

    if isinstance(value, pb.WorkflowOutputBinding):
        return _copy(value)
    data = _data(value, kwargs)
    source = data.get("value")
    return pb.WorkflowOutputBinding(
        input_field=data.get("input_field", ""),
        value=workflow_output_value_source(source) if source is not None else None,
    )


def workflow_output_binding_input_from_binding(
    value: Any,
) -> WorkflowOutputBindingInput:
    """Return input copied from a workflow output binding."""

    return WorkflowOutputBindingInput(
        input_field=value.input_field,
        value=workflow_output_value_source_input_from_source(value.value)
        if has_field(value, "value")
        else None,
    )


def workflow_output_delivery(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow output delivery ."""

    if isinstance(value, pb.WorkflowOutputDelivery):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowOutputDelivery(
        target=bound_workflow_plugin_target(target) if target is not None else None,
        input_bindings=[
            workflow_output_binding(item) for item in (data.get("input_bindings") or [])
        ],
        credential_mode=data.get("credential_mode", ""),
    )


def workflow_output_delivery_input_from_delivery(
    value: Any | None,
) -> WorkflowOutputDeliveryInput | None:
    """Return input copied from a workflow output delivery."""

    if value is None:
        return None
    return WorkflowOutputDeliveryInput(
        target=bound_workflow_plugin_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        input_bindings=[
            workflow_output_binding_input_from_binding(binding)
            for binding in value.input_bindings
        ],
        credential_mode=value.credential_mode,
    )


def bound_workflow_plugin_target(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a bound plugin workflow target ."""

    if isinstance(value, pb.BoundWorkflowPluginTarget):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.BoundWorkflowPluginTarget(
        plugin_name=data.get("plugin_name", ""),
        operation=data.get("operation", ""),
        input=_optional_struct(data.get("input")),
        connection=data.get("connection", ""),
        instance=data.get("instance", ""),
        credential_mode=data.get("credential_mode", ""),
    )


def bound_workflow_plugin_target_input_from_target(
    value: Any | None,
) -> BoundWorkflowPluginTargetInput | None:
    """Return input copied from a bound plugin workflow target."""

    if value is None:
        return None
    return BoundWorkflowPluginTargetInput(
        plugin_name=value.plugin_name,
        operation=value.operation,
        input=struct_to_dict(value.input) if has_field(value, "input") else None,
        connection=value.connection,
        instance=value.instance,
        credential_mode=value.credential_mode,
    )


def bound_workflow_agent_target(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a bound agent workflow target ."""

    if isinstance(value, pb.BoundWorkflowAgentTarget):
        return _copy(value)
    data = _data(value, kwargs)
    output_delivery = data.get("output_delivery")
    session_ready_delivery = data.get("session_ready_delivery")
    return pb.BoundWorkflowAgentTarget(
        provider_name=data.get("provider_name", ""),
        model=data.get("model", ""),
        prompt=data.get("prompt", ""),
        messages=_message_list(data.get("messages"), _agent_pb.AgentMessage),
        tool_refs=_message_list(data.get("tool_refs"), _agent_pb.AgentToolRef),
        response_schema=_optional_struct(data.get("response_schema")),
        metadata=_optional_struct(data.get("metadata")),
        timeout_seconds=data.get("timeout_seconds", 0),
        output_delivery=workflow_output_delivery(output_delivery)
        if output_delivery is not None
        else None,
        model_options=_optional_struct(data.get("model_options")),
        session_ready_delivery=workflow_output_delivery(session_ready_delivery)
        if session_ready_delivery is not None
        else None,
    )


def bound_workflow_agent_target_input_from_target(
    value: Any | None,
) -> BoundWorkflowAgentTargetInput | None:
    """Return input copied from a bound agent workflow target."""

    if value is None:
        return None
    return BoundWorkflowAgentTargetInput(
        provider_name=value.provider_name,
        model=value.model,
        prompt=value.prompt,
        messages=_message_list(value.messages, _agent_pb.AgentMessage),
        tool_refs=_message_list(value.tool_refs, _agent_pb.AgentToolRef),
        response_schema=struct_to_dict(value.response_schema)
        if has_field(value, "response_schema")
        else None,
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
        timeout_seconds=value.timeout_seconds,
        output_delivery=workflow_output_delivery_input_from_delivery(
            value.output_delivery
        )
        if has_field(value, "output_delivery")
        else None,
        model_options=struct_to_dict(value.model_options)
        if has_field(value, "model_options")
        else None,
        session_ready_delivery=workflow_output_delivery_input_from_delivery(
            value.session_ready_delivery
        )
        if has_field(value, "session_ready_delivery")
        else None,
    )


def bound_workflow_target(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a bound workflow target ."""

    if isinstance(value, pb.BoundWorkflowTarget):
        return _copy(value)
    data = _data(value, kwargs)
    plugin = data.get("plugin")
    agent = data.get("agent")
    if plugin is not None and agent is not None:
        raise ValueError("bound workflow target must set either plugin or agent")
    if plugin is not None:
        return pb.BoundWorkflowTarget(plugin=bound_workflow_plugin_target(plugin))
    if agent is not None:
        return pb.BoundWorkflowTarget(agent=bound_workflow_agent_target(agent))
    return pb.BoundWorkflowTarget()


def bound_workflow_target_input_from_target(
    value: Any | None,
) -> BoundWorkflowTargetInput | None:
    """Return input copied from a bound workflow target."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "plugin":
        return BoundWorkflowTargetInput(
            plugin=bound_workflow_plugin_target_input_from_target(value.plugin)
        )
    if kind == "agent":
        return BoundWorkflowTargetInput(
            agent=bound_workflow_agent_target_input_from_target(value.agent)
        )
    return BoundWorkflowTargetInput()


def bound_workflow_target_from_target(value: Any | None) -> Any | None:
    """Return a deep copy of a bound workflow target."""

    data = bound_workflow_target_input_from_target(value)
    return bound_workflow_target(data) if data is not None else None


def workflow_event(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow event ."""

    if isinstance(value, pb.WorkflowEvent):
        return _copy(value)
    data = _data(value, kwargs)
    event = pb.WorkflowEvent(
        id=data.get("id", ""),
        source=data.get("source", ""),
        spec_version=data.get("spec_version", ""),
        type=data.get("type", ""),
        subject=data.get("subject", ""),
        time=_optional_timestamp(data.get("time")),
        datacontenttype=data.get("datacontenttype", ""),
        data=_optional_struct(data.get("data")),
    )
    for key, item in (data.get("extensions") or {}).items():
        event.extensions[key].CopyFrom(_value(item))
    return event


def workflow_event_input_from_event(value: Any | None) -> WorkflowEventInput | None:
    """Return input copied from a workflow event."""

    if value is None:
        return None
    return WorkflowEventInput(
        id=value.id,
        source=value.source,
        spec_version=value.spec_version,
        type=value.type,
        subject=value.subject,
        time=_timestamp_to_datetime(value, "time"),
        datacontenttype=value.datacontenttype,
        data=struct_to_dict(value.data) if has_field(value, "data") else None,
        extensions={key: value_to_json(item) for key, item in value.extensions.items()},
    )


def workflow_event_from_event(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow event."""

    data = workflow_event_input_from_event(value)
    return workflow_event(data) if data is not None else None


def workflow_signal(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow signal ."""

    if isinstance(value, pb.WorkflowSignal):
        return _copy(value)
    data = _data(value, kwargs)
    created_by = data.get("created_by")
    return pb.WorkflowSignal(
        id=data.get("id", ""),
        name=data.get("name", ""),
        payload=_optional_struct(data.get("payload")),
        metadata=_optional_struct(data.get("metadata")),
        created_by=workflow_actor(created_by) if created_by is not None else None,
        created_at=_optional_timestamp(data.get("created_at")),
        idempotency_key=data.get("idempotency_key", ""),
        sequence=data.get("sequence", 0),
    )


def workflow_signal_input_from_signal(value: Any | None) -> WorkflowSignalInput | None:
    """Return input copied from a workflow signal."""

    if value is None:
        return None
    return WorkflowSignalInput(
        id=value.id,
        name=value.name,
        payload=struct_to_dict(value.payload) if has_field(value, "payload") else None,
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
        idempotency_key=value.idempotency_key,
        sequence=value.sequence,
    )


def workflow_signal_from_signal(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow signal."""

    data = workflow_signal_input_from_signal(value)
    return workflow_signal(data) if data is not None else None


def workflow_schedule_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow schedule trigger ."""

    if isinstance(value, pb.WorkflowScheduleTrigger):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowScheduleTrigger(
        schedule_id=data.get("schedule_id", ""),
        scheduled_for=_optional_timestamp(data.get("scheduled_for")),
    )


def workflow_event_trigger_invocation(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow event-trigger invocation ."""

    if isinstance(value, pb.WorkflowEventTriggerInvocation):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.WorkflowEventTriggerInvocation(
        trigger_id=data.get("trigger_id", ""),
        event=workflow_event(event) if event is not None else None,
    )


def workflow_run_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow run trigger ."""

    if isinstance(value, pb.WorkflowRunTrigger):
        return workflow_run_trigger_from_trigger(value)
    data = _data(value, kwargs)
    selected: list[str] = []
    if bool(data.get("manual")):
        selected.append("manual")
    if data.get("schedule") is not None:
        selected.append("schedule")
    if data.get("event") is not None:
        selected.append("event")
    if len(selected) > 1:
        raise ValueError("workflow run trigger must set exactly one trigger kind")
    if not selected:
        return pb.WorkflowRunTrigger()
    kind = selected[0]
    if kind == "manual":
        return pb.WorkflowRunTrigger(manual=pb.WorkflowManualTrigger())
    if kind == "schedule":
        return pb.WorkflowRunTrigger(
            schedule=workflow_schedule_trigger(data["schedule"])
        )
    return pb.WorkflowRunTrigger(event=workflow_event_trigger_invocation(data["event"]))


def workflow_run_trigger_input_from_trigger(
    value: Any | None,
) -> WorkflowRunTriggerInput | None:
    """Return input copied from a workflow run trigger."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "manual":
        return WorkflowRunTriggerInput(manual=True)
    if kind == "schedule":
        return WorkflowRunTriggerInput(
            schedule=WorkflowScheduleTriggerInput(
                schedule_id=value.schedule.schedule_id,
                scheduled_for=_timestamp_to_datetime(value.schedule, "scheduled_for"),
            )
        )
    if kind == "event":
        return WorkflowRunTriggerInput(
            event=WorkflowEventTriggerInvocationInput(
                trigger_id=value.event.trigger_id,
                event=workflow_event_input_from_event(value.event.event)
                if has_field(value.event, "event")
                else None,
            )
        )
    return WorkflowRunTriggerInput()


def workflow_run_trigger_from_trigger(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow run trigger."""

    data = workflow_run_trigger_input_from_trigger(value)
    return workflow_run_trigger(data) if data is not None else None


def _invoke_workflow_operation_request(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow host InvokeOperation request ."""

    if isinstance(value, pb.InvokeWorkflowOperationRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    trigger = data.get("trigger")
    created_by = data.get("created_by")
    return pb.InvokeWorkflowOperationRequest(
        target=bound_workflow_target(target) if target is not None else None,
        run_id=data.get("run_id", ""),
        trigger=workflow_run_trigger(trigger) if trigger is not None else None,
        input=_optional_struct(data.get("input")),
        metadata=_optional_struct(data.get("metadata")),
        created_by=workflow_actor(created_by) if created_by is not None else None,
        execution_ref=data.get("execution_ref", ""),
        signals=[workflow_signal(item) for item in (data.get("signals") or [])],
    )


def _workflow_manager_start_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowManagerStartRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowManagerStartRunRequest(
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
        workflow_key=data.get("workflow_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_signal_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSignalRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    signal = data.get("signal")
    return pb.WorkflowManagerSignalRunRequest(
        run_id=data.get("run_id", ""),
        signal=workflow_signal(signal) if signal is not None else None,
    )


def _workflow_manager_signal_or_start_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSignalOrStartRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    signal = data.get("signal")
    return pb.WorkflowManagerSignalOrStartRunRequest(
        provider_name=data.get("provider_name", ""),
        workflow_key=data.get("workflow_key", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
        signal=workflow_signal(signal) if signal is not None else None,
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_create_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerCreateDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowManagerCreateDefinitionRequest(
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
    )


def _workflow_manager_get_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerGetDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerGetDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_manager_update_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerUpdateDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowManagerUpdateDefinitionRequest(
        definition_id=data.get("definition_id", ""),
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
    )


def _workflow_manager_delete_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerDeleteDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerDeleteDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_manager_create_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerCreateScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowManagerCreateScheduleRequest(
        provider_name=data.get("provider_name", ""),
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        idempotency_key=data.get("idempotency_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_get_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerGetScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerGetScheduleRequest(schedule_id=data.get("schedule_id", ""))


def _workflow_manager_update_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerUpdateScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowManagerUpdateScheduleRequest(
        schedule_id=data.get("schedule_id", ""),
        provider_name=data.get("provider_name", ""),
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_id_request(
    message_type: type[Any], id_field: str, value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, message_type):
        return _copy(value)
    data = _data(value, kwargs)
    return message_type(**{id_field: data.get(id_field, "")})


def _workflow_manager_create_event_trigger_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerCreateEventTriggerRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    event_match = data.get("match")
    return pb.WorkflowManagerCreateEventTriggerRequest(
        provider_name=data.get("provider_name", ""),
        match=workflow_event_match(event_match) if event_match is not None else None,
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        idempotency_key=data.get("idempotency_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_update_event_trigger_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerUpdateEventTriggerRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    event_match = data.get("match")
    return pb.WorkflowManagerUpdateEventTriggerRequest(
        trigger_id=data.get("trigger_id", ""),
        provider_name=data.get("provider_name", ""),
        match=workflow_event_match(event_match) if event_match is not None else None,
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_manager_publish_event_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerPublishEventRequest):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.WorkflowManagerPublishEventRequest(
        provider_name=data.get("provider_name", ""),
        event=workflow_event(event) if event is not None else None,
    )


def bound_workflow_run(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider run ."""

    if isinstance(value, pb.BoundWorkflowRun):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    trigger = data.get("trigger")
    created_by = data.get("created_by")
    return pb.BoundWorkflowRun(
        id=data.get("id", ""),
        status=data.get("status", WORKFLOW_RUN_STATUS_UNSPECIFIED),
        target=bound_workflow_target(target) if target is not None else None,
        trigger=workflow_run_trigger(trigger) if trigger is not None else None,
        created_at=_optional_timestamp(data.get("created_at")),
        started_at=_optional_timestamp(data.get("started_at")),
        completed_at=_optional_timestamp(data.get("completed_at")),
        status_message=data.get("status_message", ""),
        result_body=data.get("result_body", ""),
        created_by=workflow_actor(created_by) if created_by is not None else None,
        execution_ref=data.get("execution_ref", ""),
        workflow_key=data.get("workflow_key", ""),
    )


def bound_workflow_run_input_from_run(
    value: Any | None,
) -> BoundWorkflowRunInput | None:
    """Return input copied from a workflow-provider run."""

    if value is None:
        return None
    return BoundWorkflowRunInput(
        id=value.id,
        status=value.status,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        trigger=workflow_run_trigger_input_from_trigger(value.trigger)
        if has_field(value, "trigger")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
        started_at=_timestamp_to_datetime(value, "started_at"),
        completed_at=_timestamp_to_datetime(value, "completed_at"),
        status_message=value.status_message,
        result_body=value.result_body,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
        workflow_key=value.workflow_key,
    )


def bound_workflow_run_from_run(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider run."""

    data = bound_workflow_run_input_from_run(value)
    return bound_workflow_run(data) if data is not None else None


def bound_workflow_definition_input_from_definition(
    value: Any | None,
) -> BoundWorkflowDefinitionInput | None:
    """Return input copied from a workflow-provider definition."""

    if value is None:
        return None
    return BoundWorkflowDefinitionInput(
        id=value.id,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
    )


def workflow_manager_bound_run_from_run(
    value: Any | None,
) -> WorkflowManagerBoundRun | None:
    if value is None:
        return None
    return WorkflowManagerBoundRun(
        id=value.id,
        status=value.status,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        trigger=workflow_run_trigger_input_from_trigger(value.trigger)
        if has_field(value, "trigger")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
        started_at=_timestamp_to_datetime(value, "started_at"),
        completed_at=_timestamp_to_datetime(value, "completed_at"),
        status_message=value.status_message,
        result_body=value.result_body,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
        workflow_key=value.workflow_key,
    )


def workflow_manager_bound_definition_from_definition(
    value: Any | None,
) -> WorkflowManagerBoundDefinition | None:
    if value is None:
        return None
    return WorkflowManagerBoundDefinition(
        id=value.id,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
    )


def bound_workflow_schedule(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider schedule ."""

    if isinstance(value, pb.BoundWorkflowSchedule):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    created_by = data.get("created_by")
    return pb.BoundWorkflowSchedule(
        id=data.get("id", ""),
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        created_at=_optional_timestamp(data.get("created_at")),
        updated_at=_optional_timestamp(data.get("updated_at")),
        next_run_at=_optional_timestamp(data.get("next_run_at")),
        created_by=workflow_actor(created_by) if created_by is not None else None,
        execution_ref=data.get("execution_ref", ""),
    )


def bound_workflow_schedule_input_from_schedule(
    value: Any | None,
) -> BoundWorkflowScheduleInput | None:
    """Return input copied from a workflow-provider schedule."""

    if value is None:
        return None
    return BoundWorkflowScheduleInput(
        id=value.id,
        cron=value.cron,
        timezone=value.timezone,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        created_at=_timestamp_to_datetime(value, "created_at"),
        updated_at=_timestamp_to_datetime(value, "updated_at"),
        next_run_at=_timestamp_to_datetime(value, "next_run_at"),
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
    )


def bound_workflow_schedule_from_schedule(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider schedule."""

    data = bound_workflow_schedule_input_from_schedule(value)
    return bound_workflow_schedule(data) if data is not None else None


def workflow_manager_bound_schedule_from_schedule(
    value: Any | None,
) -> WorkflowManagerBoundSchedule | None:
    if value is None:
        return None
    return WorkflowManagerBoundSchedule(
        id=value.id,
        cron=value.cron,
        timezone=value.timezone,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        created_at=_timestamp_to_datetime(value, "created_at"),
        updated_at=_timestamp_to_datetime(value, "updated_at"),
        next_run_at=_timestamp_to_datetime(value, "next_run_at"),
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
    )


def bound_workflow_event_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider event trigger ."""

    if isinstance(value, pb.BoundWorkflowEventTrigger):
        return _copy(value)
    data = _data(value, kwargs)
    match = data.get("match")
    target = data.get("target")
    created_by = data.get("created_by")
    return pb.BoundWorkflowEventTrigger(
        id=data.get("id", ""),
        match=workflow_event_match(match) if match is not None else None,
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        created_at=_optional_timestamp(data.get("created_at")),
        updated_at=_optional_timestamp(data.get("updated_at")),
        created_by=workflow_actor(created_by) if created_by is not None else None,
        execution_ref=data.get("execution_ref", ""),
    )


def bound_workflow_event_trigger_input_from_trigger(
    value: Any | None,
) -> BoundWorkflowEventTriggerInput | None:
    """Return input copied from a workflow-provider event trigger."""

    if value is None:
        return None
    return BoundWorkflowEventTriggerInput(
        id=value.id,
        match=workflow_event_match_input_from_match(value.match)
        if has_field(value, "match")
        else None,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        created_at=_timestamp_to_datetime(value, "created_at"),
        updated_at=_timestamp_to_datetime(value, "updated_at"),
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
    )


def bound_workflow_event_trigger_from_trigger(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider event trigger."""

    data = bound_workflow_event_trigger_input_from_trigger(value)
    return bound_workflow_event_trigger(data) if data is not None else None


def workflow_manager_bound_event_trigger_from_trigger(
    value: Any | None,
) -> WorkflowManagerBoundEventTrigger | None:
    if value is None:
        return None
    return WorkflowManagerBoundEventTrigger(
        id=value.id,
        match=workflow_event_match_input_from_match(value.match)
        if has_field(value, "match")
        else None,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        created_at=_timestamp_to_datetime(value, "created_at"),
        updated_at=_timestamp_to_datetime(value, "updated_at"),
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
    )


def workflow_manager_run_from_proto(value: Any) -> WorkflowManagerRun:
    return WorkflowManagerRun(
        provider_name=value.provider_name,
        run=workflow_manager_bound_run_from_run(value.run)
        if has_field(value, "run")
        else None,
    )


def workflow_manager_run_signal_from_proto(value: Any) -> WorkflowManagerRunSignal:
    return WorkflowManagerRunSignal(
        provider_name=value.provider_name,
        run=workflow_manager_bound_run_from_run(value.run)
        if has_field(value, "run")
        else None,
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        started_run=value.started_run,
        workflow_key=value.workflow_key,
    )


def workflow_manager_definition_from_proto(value: Any) -> WorkflowManagerDefinition:
    return WorkflowManagerDefinition(
        provider_name=value.provider_name,
        definition=workflow_manager_bound_definition_from_definition(
            value.definition
        )
        if has_field(value, "definition")
        else None,
    )


def workflow_manager_schedule_from_proto(value: Any) -> WorkflowManagerSchedule:
    return WorkflowManagerSchedule(
        provider_name=value.provider_name,
        schedule=workflow_manager_bound_schedule_from_schedule(value.schedule)
        if has_field(value, "schedule")
        else None,
    )


def workflow_manager_event_trigger_from_proto(
    value: Any,
) -> WorkflowManagerEventTrigger:
    return WorkflowManagerEventTrigger(
        provider_name=value.provider_name,
        trigger=workflow_manager_bound_event_trigger_from_trigger(value.trigger)
        if has_field(value, "trigger")
        else None,
    )


def workflow_execution_reference(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow execution reference ."""

    if isinstance(value, pb.WorkflowExecutionReference):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    run_as = data.get("run_as")
    return pb.WorkflowExecutionReference(
        id=data.get("id", ""),
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
        subject_id=data.get("subject_id", ""),
        credential_subject_id=data.get("credential_subject_id", ""),
        permissions=[
            workflow_access_permission(item) for item in (data.get("permissions") or [])
        ],
        created_at=_optional_timestamp(data.get("created_at")),
        revoked_at=_optional_timestamp(data.get("revoked_at")),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
        caller_plugin_name=data.get("caller_plugin_name", ""),
        run_as=workflow_run_as_subject(run_as) if run_as is not None else None,
        source_definition_id=data.get("source_definition_id", ""),
    )


def workflow_execution_reference_input_from_reference(
    value: Any | None,
) -> WorkflowExecutionReferenceInput | None:
    """Return input copied from a workflow execution reference."""

    if value is None:
        return None
    return WorkflowExecutionReferenceInput(
        id=value.id,
        provider_name=value.provider_name,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        subject_id=value.subject_id,
        credential_subject_id=value.credential_subject_id,
        permissions=[
            workflow_access_permission_input_from_permission(item)
            for item in value.permissions
        ],
        created_at=_timestamp_to_datetime(value, "created_at"),
        revoked_at=_timestamp_to_datetime(value, "revoked_at"),
        subject_kind=value.subject_kind,
        display_name=value.display_name,
        auth_source=value.auth_source,
        caller_plugin_name=value.caller_plugin_name,
        run_as=workflow_run_as_subject_input_from_subject(value.run_as)
        if has_field(value, "run_as")
        else None,
        source_definition_id=value.source_definition_id,
    )


def workflow_execution_reference_from_reference(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow execution reference."""

    data = workflow_execution_reference_input_from_reference(value)
    return workflow_execution_reference(data) if data is not None else None


def BoundWorkflowTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound workflow target value."""

    return pb.BoundWorkflowTarget(*args, **kwargs)


def BoundWorkflowPluginTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound plugin workflow target value."""

    return pb.BoundWorkflowPluginTarget(*args, **kwargs)


def BoundWorkflowAgentTarget(*args: Any, **kwargs: Any) -> Any:
    """Create a bound agent workflow target value."""

    return pb.BoundWorkflowAgentTarget(*args, **kwargs)


def WorkflowOutputDelivery(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output delivery value."""

    return pb.WorkflowOutputDelivery(*args, **kwargs)


def WorkflowOutputBinding(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output binding value."""

    return pb.WorkflowOutputBinding(*args, **kwargs)


def WorkflowOutputValueSource(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow output value source."""

    return pb.WorkflowOutputValueSource(*args, **kwargs)


def WorkflowActor(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow actor value."""

    return pb.WorkflowActor(*args, **kwargs)


def WorkflowSignal(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow signal value."""

    return pb.WorkflowSignal(*args, **kwargs)


def WorkflowEvent(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow event value."""

    return pb.WorkflowEvent(*args, **kwargs)


def WorkflowEventMatch(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow event-match value."""

    return pb.WorkflowEventMatch(*args, **kwargs)


def WorkflowRunTrigger(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow run-trigger value."""

    return pb.WorkflowRunTrigger(*args, **kwargs)


def BoundWorkflowRun(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider run value."""

    return pb.BoundWorkflowRun(*args, **kwargs)


def BoundWorkflowSchedule(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider schedule value."""

    return pb.BoundWorkflowSchedule(*args, **kwargs)


def BoundWorkflowEventTrigger(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider event trigger value."""

    return pb.BoundWorkflowEventTrigger(*args, **kwargs)


def BoundWorkflowDefinition(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow definition value."""

    return pb.BoundWorkflowDefinition(*args, **kwargs)


def WorkflowAccessPermission(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow execution-reference access permission."""

    return pb.WorkflowAccessPermission(*args, **kwargs)


def WorkflowExecutionReference(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow execution reference."""

    return pb.WorkflowExecutionReference(*args, **kwargs)


def StartWorkflowProviderRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider start-run request."""

    return pb.StartWorkflowProviderRunRequest(*args, **kwargs)


def GetWorkflowProviderRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider get-run request."""

    return pb.GetWorkflowProviderRunRequest(*args, **kwargs)


def ListWorkflowProviderRunsRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-runs request."""

    return pb.ListWorkflowProviderRunsRequest(*args, **kwargs)


def ListWorkflowProviderRunsResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-runs response."""

    return pb.ListWorkflowProviderRunsResponse(*args, **kwargs)


def CancelWorkflowProviderRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider cancel-run request."""

    return pb.CancelWorkflowProviderRunRequest(*args, **kwargs)


def SignalWorkflowProviderRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider signal-run request."""

    return pb.SignalWorkflowProviderRunRequest(*args, **kwargs)


def SignalOrStartWorkflowProviderRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider signal-or-start-run request."""

    return pb.SignalOrStartWorkflowProviderRunRequest(*args, **kwargs)


def SignalWorkflowRunResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider signal-run response."""

    return pb.SignalWorkflowRunResponse(*args, **kwargs)


def UpsertWorkflowProviderScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider upsert-schedule request."""

    return pb.UpsertWorkflowProviderScheduleRequest(*args, **kwargs)


def GetWorkflowProviderScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider get-schedule request."""

    return pb.GetWorkflowProviderScheduleRequest(*args, **kwargs)


def ListWorkflowProviderSchedulesRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-schedules request."""

    return pb.ListWorkflowProviderSchedulesRequest(*args, **kwargs)


def ListWorkflowProviderSchedulesResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-schedules response."""

    return pb.ListWorkflowProviderSchedulesResponse(*args, **kwargs)


def DeleteWorkflowProviderScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider delete-schedule request."""

    return pb.DeleteWorkflowProviderScheduleRequest(*args, **kwargs)


def PauseWorkflowProviderScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider pause-schedule request."""

    return pb.PauseWorkflowProviderScheduleRequest(*args, **kwargs)


def ResumeWorkflowProviderScheduleRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider resume-schedule request."""

    return pb.ResumeWorkflowProviderScheduleRequest(*args, **kwargs)


def UpsertWorkflowProviderEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider upsert-event-trigger request."""

    return pb.UpsertWorkflowProviderEventTriggerRequest(*args, **kwargs)


def GetWorkflowProviderEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider get-event-trigger request."""

    return pb.GetWorkflowProviderEventTriggerRequest(*args, **kwargs)


def ListWorkflowProviderEventTriggersRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-event-triggers request."""

    return pb.ListWorkflowProviderEventTriggersRequest(*args, **kwargs)


def ListWorkflowProviderEventTriggersResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-event-triggers response."""

    return pb.ListWorkflowProviderEventTriggersResponse(*args, **kwargs)


def DeleteWorkflowProviderEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider delete-event-trigger request."""

    return pb.DeleteWorkflowProviderEventTriggerRequest(*args, **kwargs)


def PauseWorkflowProviderEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider pause-event-trigger request."""

    return pb.PauseWorkflowProviderEventTriggerRequest(*args, **kwargs)


def ResumeWorkflowProviderEventTriggerRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider resume-event-trigger request."""

    return pb.ResumeWorkflowProviderEventTriggerRequest(*args, **kwargs)


def PutWorkflowExecutionReferenceRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider put-execution-reference request."""

    return pb.PutWorkflowExecutionReferenceRequest(*args, **kwargs)


def GetWorkflowExecutionReferenceRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider get-execution-reference request."""

    return pb.GetWorkflowExecutionReferenceRequest(*args, **kwargs)


def ListWorkflowExecutionReferencesRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-execution-references request."""

    return pb.ListWorkflowExecutionReferencesRequest(*args, **kwargs)


def ListWorkflowExecutionReferencesResponse(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider list-execution-references response."""

    return pb.ListWorkflowExecutionReferencesResponse(*args, **kwargs)


def PublishWorkflowProviderEventRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-provider publish-event request."""

    return pb.PublishWorkflowProviderEventRequest(*args, **kwargs)


def WorkflowManagerStartRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager start-run request."""

    return pb.WorkflowManagerStartRunRequest(*args, **kwargs)


def WorkflowManagerSignalRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager signal-run request."""

    return pb.WorkflowManagerSignalRunRequest(*args, **kwargs)


def WorkflowManagerSignalOrStartRunRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager signal-or-start-run request."""

    return pb.WorkflowManagerSignalOrStartRunRequest(*args, **kwargs)


def WorkflowManagerCreateDefinitionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager create-definition request."""

    return pb.WorkflowManagerCreateDefinitionRequest(*args, **kwargs)


def WorkflowManagerGetDefinitionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager get-definition request."""

    return pb.WorkflowManagerGetDefinitionRequest(*args, **kwargs)


def WorkflowManagerUpdateDefinitionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager update-definition request."""

    return pb.WorkflowManagerUpdateDefinitionRequest(*args, **kwargs)


def WorkflowManagerDeleteDefinitionRequest(*args: Any, **kwargs: Any) -> Any:
    """Create a workflow-manager delete-definition request."""

    return pb.WorkflowManagerDeleteDefinitionRequest(*args, **kwargs)


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


def workflow_run_status_name(status: int) -> str:
    """Return the enum name for a workflow run status value."""

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

    def invoke_operation(
        self, request: Any | None = None, **kwargs: Any
    ) -> InvokeWorkflowOperationResponse:
        """Invoke an operation through the workflow host."""

        response = _grpc_call(
            self._stub.InvokeOperation,
            _invoke_workflow_operation_request(request, **kwargs),
        )
        return InvokeWorkflowOperationResponse(
            status=response.status,
            body=response.body,
        )

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

    def start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerRun:
        """Start a workflow run."""

        request = _workflow_manager_start_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_manager_run_from_proto(_grpc_call(self._stub.StartRun, request))

    def signal_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerRunSignal:
        """Signal an existing workflow run."""

        request = _workflow_manager_signal_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_run_signal_from_proto(
            _grpc_call(self._stub.SignalRun, request)
        )

    def signal_or_start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerRunSignal:
        """Signal a run, or start it when no matching run exists."""

        request = _workflow_manager_signal_or_start_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_manager_run_signal_from_proto(
            _grpc_call(self._stub.SignalOrStartRun, request)
        )

    def create_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerDefinition:
        """Create a reusable workflow definition."""

        request = _workflow_manager_create_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_manager_definition_from_proto(
            _grpc_call(self._stub.CreateDefinition, request)
        )

    def get_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerDefinition:
        """Fetch one workflow definition."""

        request = _workflow_manager_get_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_definition_from_proto(
            _grpc_call(self._stub.GetDefinition, request)
        )

    def update_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerDefinition:
        """Update a workflow definition."""

        request = _workflow_manager_update_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_definition_from_proto(
            _grpc_call(self._stub.UpdateDefinition, request)
        )

    def delete_definition(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete a workflow definition."""

        request = _workflow_manager_delete_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteDefinition, request)
        return None

    def create_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerSchedule:
        """Create a workflow schedule."""

        request = _workflow_manager_create_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_manager_schedule_from_proto(
            _grpc_call(self._stub.CreateSchedule, request)
        )

    def get_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerSchedule:
        """Fetch one workflow schedule."""

        request = _workflow_manager_get_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_schedule_from_proto(
            _grpc_call(self._stub.GetSchedule, request)
        )

    def update_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerSchedule:
        """Update a workflow schedule."""

        request = _workflow_manager_update_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_schedule_from_proto(
            _grpc_call(self._stub.UpdateSchedule, request)
        )

    def delete_schedule(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete a workflow schedule."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerDeleteScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteSchedule, request)
        return None

    def pause_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerSchedule:
        """Pause a workflow schedule."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerPauseScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_manager_schedule_from_proto(
            _grpc_call(self._stub.PauseSchedule, request)
        )

    def resume_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerSchedule:
        """Resume a workflow schedule."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerResumeScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_manager_schedule_from_proto(
            _grpc_call(self._stub.ResumeSchedule, request)
        )

    def create_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerEventTrigger:
        """Create an event trigger."""

        request = _workflow_manager_create_event_trigger_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_manager_event_trigger_from_proto(
            _grpc_call(self._stub.CreateEventTrigger, request)
        )

    def get_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerEventTrigger:
        """Fetch one event trigger."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerGetEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_manager_event_trigger_from_proto(
            _grpc_call(self._stub.GetEventTrigger, request)
        )

    def update_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerEventTrigger:
        """Update an event trigger."""

        request = _workflow_manager_update_event_trigger_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_manager_event_trigger_from_proto(
            _grpc_call(self._stub.UpdateEventTrigger, request)
        )

    def delete_trigger(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete an event trigger."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerDeleteEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteEventTrigger, request)
        return None

    def pause_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerEventTrigger:
        """Pause an event trigger."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerPauseEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_manager_event_trigger_from_proto(
            _grpc_call(self._stub.PauseEventTrigger, request)
        )

    def resume_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerEventTrigger:
        """Resume an event trigger."""

        request = _workflow_manager_id_request(
            pb.WorkflowManagerResumeEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_manager_event_trigger_from_proto(
            _grpc_call(self._stub.ResumeEventTrigger, request)
        )

    def publish_event(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerPublishedEvent | None:
        """Publish an event into the workflow host."""

        request = _workflow_manager_publish_event_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_event_input_from_event(
            _grpc_call(self._stub.PublishEvent, request)
        )

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
