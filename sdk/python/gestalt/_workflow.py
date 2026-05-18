from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import os
from collections.abc import Mapping, Sequence
from typing import Any

import grpc
from google.protobuf import message as _message

from ._agent import (
    agent_message_from_dict,
    agent_message_from_proto,
    agent_message_to_proto,
    agent_tool_ref_from_dict,
    agent_tool_ref_from_proto,
    agent_tool_ref_to_proto,
)
from ._gen.v1 import agent_pb2 as _agent_pb
from ._gen.v1 import plugin_pb2 as _plugin_pb
from ._gen.v1 import workflow_pb2 as _pb
from ._gen.v1 import workflow_pb2_grpc as _pb_grpc
from ._grpc_transport import host_service_channel
from ._protocol import (
    coerce_model as _coerce,
)
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
class WorkflowText:
    """Native data for templated workflow text."""

    template: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStepOutputSource:
    """Native data for a workflow step output value source."""

    step_id: str = ""
    path: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowValue:
    """Native data for a workflow value expression."""

    literal: Any = _MISSING
    object: Mapping[str, Any] | None = None
    array: Sequence[Any] | None = None
    template: Any | None = None
    run_input: str = ""
    signal_payload: str = ""
    signal_metadata: str = ""
    workflow_context: str = ""
    step_output: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepPluginCall:
    """Native data for a workflow plugin step call."""

    name: str = ""
    operation: str = ""
    input: Any | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStepDelivery:
    """Native data for a workflow step output delivery."""

    plugin: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepAgentTurn:
    """Native data for a workflow agent step turn."""

    provider: str = ""
    model: str = ""
    session_key: str = ""
    prompt: Any | None = None
    messages: Sequence[Any] | None = None
    tools: Sequence[Any] | None = None
    response_schema: Any | None = None
    model_options: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowAgentMessage:
    """Native data for a workflow agent message."""

    role: str = ""
    text: Any | None = None
    metadata: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepWhen:
    """Native data for a workflow step condition."""

    value: Any | None = None
    equals: Any = _MISSING


@_dataclasses.dataclass(slots=True)
class WorkflowStep:
    """Native data for one workflow step."""

    id: str = ""
    inputs: Mapping[str, Any] | None = None
    plugin: Any | None = None
    agent: Any | None = None
    when: Any | None = None
    timeout_seconds: int = 0
    output_delivery: Any | None = None
    metadata: Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowTarget:
    """Native data for a bound workflow target."""

    steps: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActor:
    """Native data for a workflow actor."""

    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowEvent:
    """Native data for a workflow event."""

    id: str = ""
    source: str = ""
    spec_version: str = ""
    type: str = ""
    subject: str = ""
    time: _dt.datetime | Any | None = None
    datacontenttype: str = ""
    data: Any | None = None
    extensions: Mapping[str, Any] | None = None


WorkflowManagerPublishedEvent = WorkflowEvent


@_dataclasses.dataclass(slots=True)
class WorkflowEventMatch:
    """Native data for workflow event matching fields."""

    type: str = ""
    source: str = ""
    subject: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowSignal:
    """Native data for a workflow signal."""

    id: str = ""
    name: str = ""
    payload: Any | None = None
    metadata: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None
    idempotency_key: str = ""
    sequence: int = 0


@_dataclasses.dataclass(slots=True)
class WorkflowScheduleTrigger:
    """Native data for a schedule-triggered workflow run."""

    schedule_id: str = ""
    scheduled_for: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowEventTriggerInvocation:
    """Native data for an event-triggered workflow run."""

    trigger_id: str = ""
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunTrigger:
    """Native data for a workflow run trigger."""

    manual: bool = False
    schedule: Any | None = None
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowRun:
    """Native data for a workflow-provider run."""

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
class BoundWorkflowDefinition:
    """Native data copied from a workflow-provider definition."""

    id: str = ""
    target: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowSchedule:
    """Native data for a workflow-provider schedule."""

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
class BoundWorkflowEventTrigger:
    """Native data for a workflow-provider event trigger."""

    id: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    created_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowAccessPermission:
    """Native data for an execution-reference permission."""

    plugin: str = ""
    operations: Sequence[str] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunAsSubject:
    """Native data for a workflow run-as subject."""

    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowExecutionReference:
    """Native data for a workflow execution reference."""

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
class WorkflowManagerStartRun:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    workflow_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalRun:
    run_id: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalOrStartRun:
    provider_name: str = ""
    workflow_key: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    signal: Any | None = None
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateDefinition:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetDefinition:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateDefinition:
    definition_id: str = ""
    provider_name: str = ""
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteDefinition:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateSchedule:
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateSchedule:
    schedule_id: str = ""
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPauseSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerResumeSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCreateEventTrigger:
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerUpdateEventTrigger:
    trigger_id: str = ""
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPauseEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerResumeEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerPublishEvent:
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
    signal: WorkflowSignal | None = None
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
    """Native data for invoking a workflow operation through the host."""

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


def _message_mapping(item: Any) -> dict[str, Any]:
    mapping = _dataclass_mapping(item)
    if mapping is not None:
        return dict(mapping)
    if not isinstance(item, Mapping):
        raise TypeError(
            f"expected protobuf message, mapping, or dataclass, got {type(item).__name__}"
        )
    return dict(item)


def _message_proto_list(
    values: Sequence[Any] | None, message_type: type[Any]
) -> list[Any]:
    if values is None:
        return []
    output = []
    for item in values:
        if isinstance(item, _message.Message):
            output.append(_copy(item))
        elif message_type is _agent_pb.AgentMessage:
            output.append(agent_message_to_proto(item))
        elif message_type is _plugin_pb.AgentToolRef:
            converted = agent_tool_ref_to_proto(item)
            if converted is None:
                raise TypeError("AgentToolRef item cannot be None")
            output.append(converted)
        else:
            output.append(message_type(**_message_mapping(item)))
    return output


def _agent_message_input_list(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []
    output = []
    for item in values:
        if isinstance(item, _agent_pb.AgentMessage):
            output.append(agent_message_from_proto(item))
        else:
            output.append(agent_message_from_dict(item))
    return output


def _agent_tool_ref_input_list(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []
    output = []
    for item in values:
        if isinstance(item, _plugin_pb.AgentToolRef):
            output.append(agent_tool_ref_from_proto(item))
        else:
            output.append(agent_tool_ref_from_dict(item))
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


def workflow_actor_input_from_actor(value: Any | None) -> WorkflowActor | None:
    """Return input copied from a workflow actor."""

    if value is None:
        return None
    return WorkflowActor(
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
) -> WorkflowRunAsSubject | None:
    """Return input copied from a workflow run-as subject."""

    if value is None:
        return None
    return WorkflowRunAsSubject(
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
) -> WorkflowAccessPermission:
    """Return input copied from an execution-reference permission."""

    return WorkflowAccessPermission(
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
) -> WorkflowEventMatch | None:
    """Return input copied from workflow event-match fields."""

    if value is None:
        return None
    return WorkflowEventMatch(
        type=value.type, source=value.source, subject=value.subject
    )


def workflow_text(value: Any | None = None, **kwargs: Any) -> Any:
    """Create workflow text."""

    if isinstance(value, pb.WorkflowText):
        return _copy(value)
    if isinstance(value, str):
        data = {"template": value}
        data.update(kwargs)
    else:
        data = _data(value, kwargs)
    return pb.WorkflowText(template=data.get("template", ""))


def workflow_text_input_from_text(value: Any | None) -> WorkflowText | None:
    """Return input copied from workflow text."""

    if value is None:
        return None
    return WorkflowText(template=value.template)


def workflow_step_output_source(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step output source."""

    if isinstance(value, pb.WorkflowStepOutputSource):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepOutputSource(
        step_id=data.get("step_id", ""),
        path=data.get("path", ""),
    )


def workflow_step_output_source_input_from_source(
    value: Any | None,
) -> WorkflowStepOutputSource | None:
    """Return input copied from a workflow step output source."""

    if value is None:
        return None
    return WorkflowStepOutputSource(step_id=value.step_id, path=value.path)


def _workflow_path_source(path: str) -> Any:
    return pb.WorkflowPathSource(path=path)


def workflow_value(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow value expression."""

    if isinstance(value, pb.WorkflowValue):
        return _copy(value)
    if value is not None and _dataclass_mapping(value) is None and not isinstance(value, Mapping):
        data = {"literal": value}
        data.update(kwargs)
    else:
        data = _data(value, kwargs)

    literal = data.get("literal", _MISSING)
    choices: list[tuple[str, Any]] = []
    if literal is not _MISSING:
        choices.append(("literal", literal))
    for name in (
        "object",
        "array",
        "template",
        "run_input",
        "signal_payload",
        "signal_metadata",
        "workflow_context",
        "step_output",
    ):
        item = data.get(name)
        if item is not None and item != "":
            choices.append((name, item))
    if not choices and value is not None:
        choices.append(("object", data))
    if not choices:
        return pb.WorkflowValue()
    if len(choices) > 1:
        raise ValueError("workflow value must set exactly one value kind")

    name, item = choices[0]
    if name == "literal":
        return pb.WorkflowValue(literal=_value(item))
    if name == "object":
        return pb.WorkflowValue(
            object=pb.WorkflowObject(
                fields={key: workflow_value(nested) for key, nested in item.items()}
            )
        )
    if name == "array":
        return pb.WorkflowValue(
            array=pb.WorkflowArray(values=[workflow_value(nested) for nested in item])
        )
    if name == "template":
        return pb.WorkflowValue(template=workflow_text(item))
    if name == "run_input":
        return pb.WorkflowValue(run_input=_workflow_path_source(item))
    if name == "signal_payload":
        return pb.WorkflowValue(signal_payload=_workflow_path_source(item))
    if name == "signal_metadata":
        return pb.WorkflowValue(signal_metadata=_workflow_path_source(item))
    if name == "workflow_context":
        return pb.WorkflowValue(workflow_context=_workflow_path_source(item))
    if name == "step_output":
        return pb.WorkflowValue(step_output=workflow_step_output_source(item))
    raise AssertionError(f"unknown workflow value kind {name}")


def workflow_value_input_from_value(value: Any | None) -> WorkflowValue | None:
    """Return input copied from a workflow value expression."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "literal":
        return WorkflowValue(literal=value_to_json(value.literal))
    if kind == "object":
        return WorkflowValue(
            object={
                key: workflow_value_input_from_value(item)
                for key, item in value.object.fields.items()
            }
        )
    if kind == "array":
        return WorkflowValue(
            array=[
                workflow_value_input_from_value(item)
                for item in value.array.values
            ]
        )
    if kind == "template":
        return WorkflowValue(template=workflow_text_input_from_text(value.template))
    if kind == "run_input":
        return WorkflowValue(run_input=value.run_input.path)
    if kind == "signal_payload":
        return WorkflowValue(signal_payload=value.signal_payload.path)
    if kind == "signal_metadata":
        return WorkflowValue(signal_metadata=value.signal_metadata.path)
    if kind == "workflow_context":
        return WorkflowValue(workflow_context=value.workflow_context.path)
    if kind == "step_output":
        return WorkflowValue(
            step_output=workflow_step_output_source_input_from_source(value.step_output)
        )
    return WorkflowValue()


def workflow_step_plugin_call(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow plugin step call."""

    if isinstance(value, pb.WorkflowStepPluginCall):
        return _copy(value)
    data = _data(value, kwargs)
    input_value = data.get("input")
    return pb.WorkflowStepPluginCall(
        name=data.get("name", ""),
        operation=data.get("operation", ""),
        input=workflow_value(input_value) if input_value is not None else None,
        connection=data.get("connection", ""),
        instance=data.get("instance", ""),
        credential_mode=data.get("credential_mode", ""),
    )


def workflow_step_plugin_call_input_from_call(
    value: Any | None,
) -> WorkflowStepPluginCall | None:
    """Return input copied from a workflow plugin step call."""

    if value is None:
        return None
    return WorkflowStepPluginCall(
        name=value.name,
        operation=value.operation,
        input=workflow_value_input_from_value(value.input)
        if has_field(value, "input")
        else None,
        connection=value.connection,
        instance=value.instance,
        credential_mode=value.credential_mode,
    )


def workflow_step_delivery(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step output delivery."""

    if isinstance(value, pb.WorkflowStepDelivery):
        return _copy(value)
    data = _data(value, kwargs)
    plugin = data.get("plugin")
    return pb.WorkflowStepDelivery(
        plugin=workflow_step_plugin_call(plugin) if plugin is not None else None,
    )


def workflow_step_delivery_input_from_delivery(
    value: Any | None,
) -> WorkflowStepDelivery | None:
    """Return input copied from a workflow step output delivery."""

    if value is None:
        return None
    return WorkflowStepDelivery(
        plugin=workflow_step_plugin_call_input_from_call(value.plugin)
        if has_field(value, "plugin")
        else None,
    )


def workflow_agent_message(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow agent message."""

    if isinstance(value, pb.WorkflowAgentMessage):
        return _copy(value)
    data = _data(value, kwargs)
    text = data.get("text")
    return pb.WorkflowAgentMessage(
        role=data.get("role", ""),
        text=workflow_text(text) if text is not None else None,
        metadata=_optional_struct(data.get("metadata")),
    )


def workflow_agent_message_input_from_message(
    value: Any | None,
) -> WorkflowAgentMessage | None:
    """Return input copied from a workflow agent message."""

    if value is None:
        return None
    return WorkflowAgentMessage(
        role=value.role,
        text=workflow_text_input_from_text(value.text)
        if has_field(value, "text")
        else None,
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
    )


def _workflow_agent_message_proto_list(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []
    return [workflow_agent_message(item) for item in values]


def _workflow_agent_message_input_list(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []
    return [workflow_agent_message_input_from_message(item) for item in values]


def workflow_step_agent_turn(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow agent step turn."""

    if isinstance(value, pb.WorkflowStepAgentTurn):
        return _copy(value)
    data = _data(value, kwargs)
    prompt = data.get("prompt")
    return pb.WorkflowStepAgentTurn(
        provider=data.get("provider", ""),
        model=data.get("model", ""),
        session_key=data.get("session_key", ""),
        prompt=workflow_text(prompt) if prompt is not None else None,
        messages=_workflow_agent_message_proto_list(data.get("messages")),
        tools=_message_proto_list(data.get("tools"), _plugin_pb.AgentToolRef),
        response_schema=_optional_struct(data.get("response_schema")),
        model_options=_optional_struct(data.get("model_options")),
    )


def workflow_step_agent_turn_input_from_turn(
    value: Any | None,
) -> WorkflowStepAgentTurn | None:
    """Return input copied from a workflow agent step turn."""

    if value is None:
        return None
    return WorkflowStepAgentTurn(
        provider=value.provider,
        model=value.model,
        session_key=value.session_key,
        prompt=workflow_text_input_from_text(value.prompt)
        if has_field(value, "prompt")
        else None,
        messages=_workflow_agent_message_input_list(value.messages),
        tools=_agent_tool_ref_input_list(value.tools),
        response_schema=struct_to_dict(value.response_schema)
        if has_field(value, "response_schema")
        else None,
        model_options=struct_to_dict(value.model_options)
        if has_field(value, "model_options")
        else None,
    )


def workflow_step_when(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step condition."""

    if isinstance(value, pb.WorkflowStepWhen):
        return _copy(value)
    data = _data(value, kwargs)
    condition = pb.WorkflowStepWhen()
    if data.get("value") is not None:
        condition.value.CopyFrom(workflow_value(data["value"]))
    equals = data.get("equals", _MISSING)
    if equals is not _MISSING:
        condition.equals.CopyFrom(_value(equals))
    return condition


def workflow_step_when_input_from_when(
    value: Any | None,
) -> WorkflowStepWhen | None:
    """Return input copied from a workflow step condition."""

    if value is None:
        return None
    return WorkflowStepWhen(
        value=workflow_value_input_from_value(value.value)
        if has_field(value, "value")
        else None,
        equals=value_to_json(value.equals) if has_field(value, "equals") else _MISSING,
    )


def workflow_step(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step."""

    if isinstance(value, pb.WorkflowStep):
        return _copy(value)
    data = _data(value, kwargs)
    plugin = data.get("plugin")
    agent = data.get("agent")
    if plugin is not None and agent is not None:
        raise ValueError("workflow step must set either plugin or agent")
    step = pb.WorkflowStep(
        id=data.get("id", ""),
        inputs={
            key: workflow_value(item)
            for key, item in (data.get("inputs") or {}).items()
        },
        timeout_seconds=data.get("timeout_seconds", 0),
        metadata=_optional_struct(data.get("metadata")),
    )
    if plugin is not None:
        step.plugin.CopyFrom(workflow_step_plugin_call(plugin))
    if agent is not None:
        step.agent.CopyFrom(workflow_step_agent_turn(agent))
    when = data.get("when")
    if when is not None:
        step.when.CopyFrom(workflow_step_when(when))
    output_delivery = data.get("output_delivery")
    if output_delivery is not None:
        step.output_delivery.CopyFrom(workflow_step_delivery(output_delivery))
    return step


def workflow_step_input_from_step(value: Any | None) -> WorkflowStep | None:
    """Return input copied from a workflow step."""

    if value is None:
        return None
    return WorkflowStep(
        id=value.id,
        inputs={
            key: workflow_value_input_from_value(item)
            for key, item in value.inputs.items()
        },
        plugin=workflow_step_plugin_call_input_from_call(value.plugin)
        if has_field(value, "plugin")
        else None,
        agent=workflow_step_agent_turn_input_from_turn(value.agent)
        if has_field(value, "agent")
        else None,
        when=workflow_step_when_input_from_when(value.when)
        if has_field(value, "when")
        else None,
        timeout_seconds=value.timeout_seconds,
        output_delivery=workflow_step_delivery_input_from_delivery(
            value.output_delivery
        )
        if has_field(value, "output_delivery")
        else None,
        metadata=struct_to_dict(value.metadata)
        if has_field(value, "metadata")
        else None,
    )


def bound_workflow_target(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a bound workflow target."""

    if isinstance(value, pb.BoundWorkflowTarget):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.BoundWorkflowTarget(
        steps=[workflow_step(step) for step in (data.get("steps") or [])],
    )


def bound_workflow_target_input_from_target(
    value: Any | None,
) -> BoundWorkflowTarget | None:
    """Return input copied from a bound workflow target."""

    if value is None:
        return None
    return BoundWorkflowTarget(
        steps=[workflow_step_input_from_step(step) for step in value.steps],
    )


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


def workflow_event_input_from_event(value: Any | None) -> WorkflowEvent | None:
    """Return input copied from a workflow event."""

    if value is None:
        return None
    return WorkflowEvent(
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


def workflow_signal_input_from_signal(value: Any | None) -> WorkflowSignal | None:
    """Return input copied from a workflow signal."""

    if value is None:
        return None
    return WorkflowSignal(
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
) -> WorkflowRunTrigger | None:
    """Return input copied from a workflow run trigger."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "manual":
        return WorkflowRunTrigger(manual=True)
    if kind == "schedule":
        return WorkflowRunTrigger(
            schedule=WorkflowScheduleTrigger(
                schedule_id=value.schedule.schedule_id,
                scheduled_for=_timestamp_to_datetime(value.schedule, "scheduled_for"),
            )
        )
    if kind == "event":
        return WorkflowRunTrigger(
            event=WorkflowEventTriggerInvocation(
                trigger_id=value.event.trigger_id,
                event=workflow_event_input_from_event(value.event.event)
                if has_field(value.event, "event")
                else None,
            )
        )
    return WorkflowRunTrigger()


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
) -> BoundWorkflowRun | None:
    """Return input copied from a workflow-provider run."""

    if value is None:
        return None
    return BoundWorkflowRun(
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
) -> BoundWorkflowDefinition | None:
    """Return input copied from a workflow-provider definition."""

    if value is None:
        return None
    return BoundWorkflowDefinition(
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
) -> BoundWorkflowSchedule | None:
    """Return input copied from a workflow-provider schedule."""

    if value is None:
        return None
    return BoundWorkflowSchedule(
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
) -> BoundWorkflowEventTrigger | None:
    """Return input copied from a workflow-provider event trigger."""

    if value is None:
        return None
    return BoundWorkflowEventTrigger(
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
) -> WorkflowExecutionReference | None:
    """Return input copied from a workflow execution reference."""

    if value is None:
        return None
    return WorkflowExecutionReference(
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


@_dataclasses.dataclass(slots=True)
class StartWorkflowProviderRunRequest:
    """Start-run request passed to workflow providers."""

    target: Any | None = None
    idempotency_key: str = ""
    created_by: Any | None = None
    execution_ref: str = ""
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowProviderRunRequest:
    """Get-run request passed to workflow providers."""

    run_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderRunsRequest:
    """List-runs request passed to workflow providers."""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderRunsResponse:
    """Runs returned by workflow providers."""

    runs: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class CancelWorkflowProviderRunRequest:
    """Cancel-run request passed to workflow providers."""

    run_id: str = ""
    reason: str = ""


@_dataclasses.dataclass(slots=True)
class SignalWorkflowProviderRunRequest:
    """Signal-run request passed to workflow providers."""

    run_id: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class SignalOrStartWorkflowProviderRunRequest:
    """Signal-or-start request passed to workflow providers."""

    workflow_key: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    created_by: Any | None = None
    execution_ref: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class SignalWorkflowRunResponse:
    """Signal-run response returned by workflow providers."""

    run: Any | None = None
    signal: Any | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class UpsertWorkflowProviderScheduleRequest:
    """Upsert-schedule request passed to workflow providers."""

    schedule_id: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    requested_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowProviderScheduleRequest:
    """Get-schedule request passed to workflow providers."""

    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderSchedulesRequest:
    """List-schedules request passed to workflow providers."""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderSchedulesResponse:
    """Schedules returned by workflow providers."""

    schedules: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class DeleteWorkflowProviderScheduleRequest:
    """Delete-schedule request passed to workflow providers."""

    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class PauseWorkflowProviderScheduleRequest:
    """Pause-schedule request passed to workflow providers."""

    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class ResumeWorkflowProviderScheduleRequest:
    """Resume-schedule request passed to workflow providers."""

    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class UpsertWorkflowProviderEventTriggerRequest:
    """Upsert-event-trigger request passed to workflow providers."""

    trigger_id: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    requested_by: Any | None = None
    execution_ref: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowProviderEventTriggerRequest:
    """Get-event-trigger request passed to workflow providers."""

    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderEventTriggersRequest:
    """List-event-triggers request passed to workflow providers."""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderEventTriggersResponse:
    """Event triggers returned by workflow providers."""

    triggers: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class DeleteWorkflowProviderEventTriggerRequest:
    """Delete-event-trigger request passed to workflow providers."""

    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class PauseWorkflowProviderEventTriggerRequest:
    """Pause-event-trigger request passed to workflow providers."""

    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class ResumeWorkflowProviderEventTriggerRequest:
    """Resume-event-trigger request passed to workflow providers."""

    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class PutWorkflowExecutionReferenceRequest:
    """Put-execution-reference request passed to workflow providers."""

    reference: Any | None = None


@_dataclasses.dataclass(slots=True)
class GetWorkflowExecutionReferenceRequest:
    """Get-execution-reference request passed to workflow providers."""

    id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowExecutionReferencesRequest:
    """List-execution-references request passed to workflow providers."""

    subject_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowExecutionReferencesResponse:
    """Execution references returned by workflow providers."""

    references: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class PublishWorkflowProviderEventRequest:
    """Publish-event request passed to workflow providers."""

    plugin_name: str = ""
    event: Any | None = None
    published_by: Any | None = None


def start_workflow_provider_run_request_from_proto(
    value: Any,
) -> StartWorkflowProviderRunRequest:
    return StartWorkflowProviderRunRequest(
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        idempotency_key=value.idempotency_key,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
        workflow_key=value.workflow_key,
    )


def get_workflow_provider_run_request_from_proto(
    value: Any,
) -> GetWorkflowProviderRunRequest:
    return GetWorkflowProviderRunRequest(run_id=value.run_id)


def list_workflow_provider_runs_request_from_proto(
    _value: Any,
) -> ListWorkflowProviderRunsRequest:
    return ListWorkflowProviderRunsRequest()


def list_workflow_provider_runs_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowProviderRunsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowProviderRunsResponse,
        "ListWorkflowProviderRunsResponse",
    )
    return pb.ListWorkflowProviderRunsResponse(
        runs=[bound_workflow_run(item) for item in (response.runs or [])]
    )


def cancel_workflow_provider_run_request_from_proto(
    value: Any,
) -> CancelWorkflowProviderRunRequest:
    return CancelWorkflowProviderRunRequest(run_id=value.run_id, reason=value.reason)


def signal_workflow_provider_run_request_from_proto(
    value: Any,
) -> SignalWorkflowProviderRunRequest:
    return SignalWorkflowProviderRunRequest(
        run_id=value.run_id,
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
    )


def signal_or_start_workflow_provider_run_request_from_proto(
    value: Any,
) -> SignalOrStartWorkflowProviderRunRequest:
    return SignalOrStartWorkflowProviderRunRequest(
        workflow_key=value.workflow_key,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        idempotency_key=value.idempotency_key,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
        execution_ref=value.execution_ref,
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
    )


def signal_workflow_run_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.SignalWorkflowRunResponse):
        return _copy(value)
    response = _coerce(value, SignalWorkflowRunResponse, "SignalWorkflowRunResponse")
    out = pb.SignalWorkflowRunResponse(
        started_run=response.started_run,
        workflow_key=response.workflow_key,
    )
    if response.run is not None:
        out.run.CopyFrom(bound_workflow_run(response.run))
    if response.signal is not None:
        out.signal.CopyFrom(workflow_signal(response.signal))
    return out


def upsert_workflow_provider_schedule_request_from_proto(
    value: Any,
) -> UpsertWorkflowProviderScheduleRequest:
    return UpsertWorkflowProviderScheduleRequest(
        schedule_id=value.schedule_id,
        cron=value.cron,
        timezone=value.timezone,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        requested_by=workflow_actor_input_from_actor(value.requested_by)
        if has_field(value, "requested_by")
        else None,
        execution_ref=value.execution_ref,
    )


def get_workflow_provider_schedule_request_from_proto(
    value: Any,
) -> GetWorkflowProviderScheduleRequest:
    return GetWorkflowProviderScheduleRequest(schedule_id=value.schedule_id)


def list_workflow_provider_schedules_request_from_proto(
    _value: Any,
) -> ListWorkflowProviderSchedulesRequest:
    return ListWorkflowProviderSchedulesRequest()


def list_workflow_provider_schedules_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowProviderSchedulesResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowProviderSchedulesResponse,
        "ListWorkflowProviderSchedulesResponse",
    )
    return pb.ListWorkflowProviderSchedulesResponse(
        schedules=[
            bound_workflow_schedule(item) for item in (response.schedules or [])
        ]
    )


def delete_workflow_provider_schedule_request_from_proto(
    value: Any,
) -> DeleteWorkflowProviderScheduleRequest:
    return DeleteWorkflowProviderScheduleRequest(schedule_id=value.schedule_id)


def pause_workflow_provider_schedule_request_from_proto(
    value: Any,
) -> PauseWorkflowProviderScheduleRequest:
    return PauseWorkflowProviderScheduleRequest(schedule_id=value.schedule_id)


def resume_workflow_provider_schedule_request_from_proto(
    value: Any,
) -> ResumeWorkflowProviderScheduleRequest:
    return ResumeWorkflowProviderScheduleRequest(schedule_id=value.schedule_id)


def upsert_workflow_provider_event_trigger_request_from_proto(
    value: Any,
) -> UpsertWorkflowProviderEventTriggerRequest:
    return UpsertWorkflowProviderEventTriggerRequest(
        trigger_id=value.trigger_id,
        match=workflow_event_match_input_from_match(value.match)
        if has_field(value, "match")
        else None,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        paused=value.paused,
        requested_by=workflow_actor_input_from_actor(value.requested_by)
        if has_field(value, "requested_by")
        else None,
        execution_ref=value.execution_ref,
    )


def get_workflow_provider_event_trigger_request_from_proto(
    value: Any,
) -> GetWorkflowProviderEventTriggerRequest:
    return GetWorkflowProviderEventTriggerRequest(trigger_id=value.trigger_id)


def list_workflow_provider_event_triggers_request_from_proto(
    _value: Any,
) -> ListWorkflowProviderEventTriggersRequest:
    return ListWorkflowProviderEventTriggersRequest()


def list_workflow_provider_event_triggers_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowProviderEventTriggersResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowProviderEventTriggersResponse,
        "ListWorkflowProviderEventTriggersResponse",
    )
    return pb.ListWorkflowProviderEventTriggersResponse(
        triggers=[
            bound_workflow_event_trigger(item) for item in (response.triggers or [])
        ]
    )


def delete_workflow_provider_event_trigger_request_from_proto(
    value: Any,
) -> DeleteWorkflowProviderEventTriggerRequest:
    return DeleteWorkflowProviderEventTriggerRequest(trigger_id=value.trigger_id)


def pause_workflow_provider_event_trigger_request_from_proto(
    value: Any,
) -> PauseWorkflowProviderEventTriggerRequest:
    return PauseWorkflowProviderEventTriggerRequest(trigger_id=value.trigger_id)


def resume_workflow_provider_event_trigger_request_from_proto(
    value: Any,
) -> ResumeWorkflowProviderEventTriggerRequest:
    return ResumeWorkflowProviderEventTriggerRequest(trigger_id=value.trigger_id)


def put_workflow_execution_reference_request_from_proto(
    value: Any,
) -> PutWorkflowExecutionReferenceRequest:
    return PutWorkflowExecutionReferenceRequest(
        reference=workflow_execution_reference_input_from_reference(value.reference)
        if has_field(value, "reference")
        else None,
    )


def get_workflow_execution_reference_request_from_proto(
    value: Any,
) -> GetWorkflowExecutionReferenceRequest:
    return GetWorkflowExecutionReferenceRequest(id=value.id)


def list_workflow_execution_references_request_from_proto(
    value: Any,
) -> ListWorkflowExecutionReferencesRequest:
    return ListWorkflowExecutionReferencesRequest(subject_id=value.subject_id)


def list_workflow_execution_references_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowExecutionReferencesResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowExecutionReferencesResponse,
        "ListWorkflowExecutionReferencesResponse",
    )
    return pb.ListWorkflowExecutionReferencesResponse(
        references=[
            workflow_execution_reference(item)
            for item in (response.references or [])
        ]
    )


def publish_workflow_provider_event_request_from_proto(
    value: Any,
) -> PublishWorkflowProviderEventRequest:
    return PublishWorkflowProviderEventRequest(
        plugin_name=value.plugin_name,
        event=workflow_event_input_from_event(value.event)
        if has_field(value, "event")
        else None,
        published_by=workflow_actor_input_from_actor(value.published_by)
        if has_field(value, "published_by")
        else None,
    )


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
