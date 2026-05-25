from __future__ import annotations

import ast
import dataclasses as _dataclasses
import datetime as _dt
import json
import os
from collections.abc import Mapping, Sequence
from typing import Any, Protocol, cast

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
from ._gen.v1 import app_pb2 as _app_pb
from ._gen.v1 import workflow_pb2 as _pb
from ._gen.v1 import workflow_pb2_grpc as _pb_grpc
from ._grpc_transport import (
    ENV_HOST_SERVICE_SOCKET,
    ENV_HOST_SERVICE_TOKEN,
    host_service_channel,
)
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
	json_from_native,
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
    literal_set: bool = False
    object: Mapping[str, Any] | None = None
    array: Sequence[Any] | None = None
    template: Any | None = None
    run_input: str = ""
    signal_payload: str = ""
    step_output: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepAppCall:
    """Native data for a workflow app step call."""

    name: str = ""
    operation: str = ""
    input: Any | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


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
    app: Any | None = None
    agent: Any | None = None
    when: Any | None = None
    timeout_seconds: int = 0
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
    workflow_key: str = ""
    provider_name: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class BoundWorkflowDefinition:
    """Native data copied from a workflow-provider definition."""

    id: str = ""
    target: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None
    provider_name: str = ""


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
    provider_name: str = ""
    definition_id: str = ""


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
    provider_name: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStartRun:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    workflow_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowSignalRun:
    run_id: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowSignalOrStartRun:
    provider_name: str = ""
    workflow_key: str = ""
    target: Any | None = None
    idempotency_key: str = ""
    signal: Any | None = None
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowCreateDefinition:
    provider_name: str = ""
    target: Any | None = None
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowGetDefinition:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowUpdateDefinition:
    definition_id: str = ""
    provider_name: str = ""
    target: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowDeleteDefinition:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowCreateSchedule:
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowGetSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowUpdateSchedule:
    schedule_id: str = ""
    provider_name: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowDeleteSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowPauseSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowResumeSchedule:
    schedule_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowCreateEventTrigger:
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    idempotency_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowGetEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowUpdateEventTrigger:
    trigger_id: str = ""
    provider_name: str = ""
    match: Any | None = None
    target: Any | None = None
    paused: bool = False
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowDeleteEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowPauseEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowResumeEventTrigger:
    trigger_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowPublishEvent:
    provider_name: str = ""
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRun:
    provider_name: str = ""
    run: BoundWorkflowRun | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunSignal:
    provider_name: str = ""
    run: BoundWorkflowRun | None = None
    signal: WorkflowSignal | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowDefinition:
    provider_name: str = ""
    definition: BoundWorkflowDefinition | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowSchedule:
    provider_name: str = ""
    schedule: BoundWorkflowSchedule | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowEventTrigger:
    provider_name: str = ""
    trigger: BoundWorkflowEventTrigger | None = None


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
        elif message_type is _app_pb.AgentToolRef:
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
        if isinstance(item, _app_pb.AgentToolRef):
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
    if name == "step_output":
        return pb.WorkflowValue(step_output=workflow_step_output_source(item))
    raise AssertionError(f"unknown workflow value kind {name}")


def workflow_value_input_from_value(value: Any | None) -> WorkflowValue | None:
    """Return input copied from a workflow value expression."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "literal":
        return WorkflowValue(literal=value_to_json(value.literal), literal_set=True)
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
    if kind == "step_output":
        return WorkflowValue(
            step_output=workflow_step_output_source_input_from_source(value.step_output)
        )
    return WorkflowValue()


def workflow_step_app_call(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow app step call."""

    if isinstance(value, pb.WorkflowStepAppCall):
        return _copy(value)
    data = _data(value, kwargs)
    input_value = data.get("input")
    return pb.WorkflowStepAppCall(
        name=data.get("name", ""),
        operation=data.get("operation", ""),
        input=workflow_value(input_value) if input_value is not None else None,
        connection=data.get("connection", ""),
        instance=data.get("instance", ""),
        credential_mode=data.get("credential_mode", ""),
    )


def workflow_step_app_call_input_from_call(
    value: Any | None,
) -> WorkflowStepAppCall | None:
    """Return input copied from a workflow app step call."""

    if value is None:
        return None
    return WorkflowStepAppCall(
        name=value.name,
        operation=value.operation,
        input=workflow_value_input_from_value(value.input)
        if has_field(value, "input")
        else None,
        connection=value.connection,
        instance=value.instance,
        credential_mode=value.credential_mode,
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
        tools=_message_proto_list(data.get("tools"), _app_pb.AgentToolRef),
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
    app = data.get("app")
    agent = data.get("agent")
    if app is not None and agent is not None:
        raise ValueError("workflow step must set either app or agent")
    step = pb.WorkflowStep(
        id=data.get("id", ""),
        inputs={
            key: workflow_value(item)
            for key, item in (data.get("inputs") or {}).items()
        },
        timeout_seconds=data.get("timeout_seconds", 0),
        metadata=_optional_struct(data.get("metadata")),
    )
    if app is not None:
        step.app.CopyFrom(workflow_step_app_call(app))
    if agent is not None:
        step.agent.CopyFrom(workflow_step_agent_turn(agent))
    when = data.get("when")
    if when is not None:
        step.when.CopyFrom(workflow_step_when(when))
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
        app=workflow_step_app_call_input_from_call(value.app)
        if has_field(value, "app")
        else None,
        agent=workflow_step_agent_turn_input_from_turn(value.agent)
        if has_field(value, "agent")
        else None,
        when=workflow_step_when_input_from_when(value.when)
        if has_field(value, "when")
        else None,
        timeout_seconds=value.timeout_seconds,
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


def _workflow_start_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.StartWorkflowProviderRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.StartWorkflowProviderRunRequest(
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
        workflow_key=data.get("workflow_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_signal_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SignalWorkflowProviderRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    signal = data.get("signal")
    return pb.SignalWorkflowProviderRunRequest(
        run_id=data.get("run_id", ""),
        signal=workflow_signal(signal) if signal is not None else None,
    )


def _workflow_signal_or_start_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SignalOrStartWorkflowProviderRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    signal = data.get("signal")
    return pb.SignalOrStartWorkflowProviderRunRequest(
        provider_name=data.get("provider_name", ""),
        workflow_key=data.get("workflow_key", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
        signal=workflow_signal(signal) if signal is not None else None,
        definition_id=data.get("definition_id", ""),
    )


def _workflow_create_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.CreateWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.CreateWorkflowProviderDefinitionRequest(
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
    )


def _workflow_get_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.GetWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowProviderDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_update_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.UpdateWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.UpdateWorkflowProviderDefinitionRequest(
        definition_id=data.get("definition_id", ""),
        provider_name=data.get("provider_name", ""),
        target=bound_workflow_target(target) if target is not None else None,
    )


def _workflow_delete_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.DeleteWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.DeleteWorkflowProviderDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_create_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.UpsertWorkflowProviderScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.UpsertWorkflowProviderScheduleRequest(
        provider_name=data.get("provider_name", ""),
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        idempotency_key=data.get("idempotency_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_get_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.GetWorkflowProviderScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowProviderScheduleRequest(schedule_id=data.get("schedule_id", ""))


def _workflow_update_schedule_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.UpsertWorkflowProviderScheduleRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.UpsertWorkflowProviderScheduleRequest(
        schedule_id=data.get("schedule_id", ""),
        provider_name=data.get("provider_name", ""),
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_id_request(
    message_type: type[Any], id_field: str, value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, message_type):
        return _copy(value)
    data = _data(value, kwargs)
    return message_type(**{id_field: data.get(id_field, "")})


def _workflow_create_event_trigger_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.UpsertWorkflowProviderEventTriggerRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    event_match = data.get("match")
    return pb.UpsertWorkflowProviderEventTriggerRequest(
        provider_name=data.get("provider_name", ""),
        match=workflow_event_match(event_match) if event_match is not None else None,
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        idempotency_key=data.get("idempotency_key", ""),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_update_event_trigger_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.UpsertWorkflowProviderEventTriggerRequest):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    event_match = data.get("match")
    return pb.UpsertWorkflowProviderEventTriggerRequest(
        trigger_id=data.get("trigger_id", ""),
        provider_name=data.get("provider_name", ""),
        match=workflow_event_match(event_match) if event_match is not None else None,
        target=bound_workflow_target(target) if target is not None else None,
        paused=data.get("paused", False),
        definition_id=data.get("definition_id", ""),
    )


def _workflow_publish_event_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.PublishWorkflowProviderEventRequest):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.PublishWorkflowProviderEventRequest(
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
        workflow_key=data.get("workflow_key", ""),
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
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
        workflow_key=value.workflow_key,
        provider_name=value.provider_name,
        definition_id=value.definition_id,
    )


def bound_workflow_run_from_run(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider run."""

    data = bound_workflow_run_input_from_run(value)
    return bound_workflow_run(data) if data is not None else None


def bound_workflow_definition(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider definition."""

    if isinstance(value, pb.BoundWorkflowDefinition):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    created_by = data.get("created_by")
    return pb.BoundWorkflowDefinition(
        id=data.get("id", ""),
        target=bound_workflow_target(target) if target is not None else None,
        created_by=workflow_actor(created_by) if created_by is not None else None,
        created_at=_optional_timestamp(data.get("created_at")),
        provider_name=data.get("provider_name", ""),
    )


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
        provider_name=value.provider_name,
    )


def bound_workflow_definition_from_definition(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider definition."""

    data = bound_workflow_definition_input_from_definition(value)
    return bound_workflow_definition(data) if data is not None else None


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
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
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
        provider_name=value.provider_name,
        definition_id=value.definition_id,
    )


def bound_workflow_schedule_from_schedule(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider schedule."""

    data = bound_workflow_schedule_input_from_schedule(value)
    return bound_workflow_schedule(data) if data is not None else None


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
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
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
        provider_name=value.provider_name,
        definition_id=value.definition_id,
    )


def bound_workflow_event_trigger_from_trigger(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider event trigger."""

    data = bound_workflow_event_trigger_input_from_trigger(value)
    return bound_workflow_event_trigger(data) if data is not None else None


def workflow_run_from_proto(value: Any) -> WorkflowRun:
    return WorkflowRun(
        provider_name=value.provider_name,
        run=bound_workflow_run_input_from_run(value),
    )


def workflow_run_signal_from_proto(value: Any) -> WorkflowRunSignal:
    run = value.run if has_field(value, "run") else None
    return WorkflowRunSignal(
        provider_name=run.provider_name if run is not None else "",
        run=bound_workflow_run_input_from_run(run),
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        started_run=value.started_run,
        workflow_key=value.workflow_key,
    )


def workflow_definition_from_proto(value: Any) -> WorkflowDefinition:
    return WorkflowDefinition(
        provider_name=value.provider_name,
        definition=bound_workflow_definition_input_from_definition(value),
    )


def workflow_schedule_from_proto(value: Any) -> WorkflowSchedule:
    return WorkflowSchedule(
        provider_name=value.provider_name,
        schedule=bound_workflow_schedule_input_from_schedule(value),
    )


def workflow_event_trigger_from_proto(
    value: Any,
) -> WorkflowEventTrigger:
    return WorkflowEventTrigger(
        provider_name=value.provider_name,
        trigger=bound_workflow_event_trigger_input_from_trigger(value),
    )


@_dataclasses.dataclass(slots=True)
class StartWorkflowProviderRunRequest:
    """Start-run request passed to workflow providers."""

    target: Any | None = None
    idempotency_key: str = ""
    created_by: Any | None = None
    workflow_key: str = ""
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowProviderRunRequest:
    """Get-run request passed to workflow providers."""

    run_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderRunsRequest:
    """List-runs request passed to workflow providers."""

    page_size: int = 0
    page_token: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    target_app: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowProviderRunsResponse:
    """Runs returned by workflow providers."""

    runs: Sequence[Any] | None = None
    next_page_token: str = ""


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
    signal: Any | None = None
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class SignalWorkflowRunResponse:
    """Signal-run response returned by workflow providers."""

    run: Any | None = None
    signal: Any | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class CreateWorkflowProviderDefinitionRequest:
    """Create-definition request passed to workflow providers."""

    target: Any | None = None
    idempotency_key: str = ""
    created_by: Any | None = None


@_dataclasses.dataclass(slots=True)
class GetWorkflowProviderDefinitionRequest:
    """Get-definition request passed to workflow providers."""

    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class UpdateWorkflowProviderDefinitionRequest:
    """Update-definition request passed to workflow providers."""

    definition_id: str = ""
    target: Any | None = None
    requested_by: Any | None = None


@_dataclasses.dataclass(slots=True)
class DeleteWorkflowProviderDefinitionRequest:
    """Delete-definition request passed to workflow providers."""

    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class UpsertWorkflowProviderScheduleRequest:
    """Upsert-schedule request passed to workflow providers."""

    schedule_id: str = ""
    cron: str = ""
    timezone: str = ""
    target: Any | None = None
    paused: bool = False
    requested_by: Any | None = None
    idempotency_key: str = ""
    definition_id: str = ""


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
    idempotency_key: str = ""
    definition_id: str = ""


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
class PublishWorkflowProviderEventRequest:
    """Publish-event request passed to workflow providers."""

    app_name: str = ""
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
        workflow_key=value.workflow_key,
        definition_id=value.definition_id,
    )


def get_workflow_provider_run_request_from_proto(
    value: Any,
) -> GetWorkflowProviderRunRequest:
    return GetWorkflowProviderRunRequest(run_id=value.run_id)


def list_workflow_provider_runs_request_from_proto(
    value: Any,
) -> ListWorkflowProviderRunsRequest:
    return ListWorkflowProviderRunsRequest(
        page_size=int(value.page_size),
        page_token=value.page_token,
        status=value.status,
        target_app=value.target_app,
    )


def list_workflow_provider_runs_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowProviderRunsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowProviderRunsResponse,
        "ListWorkflowProviderRunsResponse",
    )
    return pb.ListWorkflowProviderRunsResponse(
        runs=[bound_workflow_run(item) for item in (response.runs or [])],
        next_page_token=response.next_page_token,
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
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        definition_id=value.definition_id,
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


def create_workflow_provider_definition_request_from_proto(
    value: Any,
) -> CreateWorkflowProviderDefinitionRequest:
    return CreateWorkflowProviderDefinitionRequest(
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        idempotency_key=value.idempotency_key,
        created_by=workflow_actor_input_from_actor(value.created_by)
        if has_field(value, "created_by")
        else None,
    )


def get_workflow_provider_definition_request_from_proto(
    value: Any,
) -> GetWorkflowProviderDefinitionRequest:
    return GetWorkflowProviderDefinitionRequest(definition_id=value.definition_id)


def update_workflow_provider_definition_request_from_proto(
    value: Any,
) -> UpdateWorkflowProviderDefinitionRequest:
    return UpdateWorkflowProviderDefinitionRequest(
        definition_id=value.definition_id,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        requested_by=workflow_actor_input_from_actor(value.requested_by)
        if has_field(value, "requested_by")
        else None,
    )


def delete_workflow_provider_definition_request_from_proto(
    value: Any,
) -> DeleteWorkflowProviderDefinitionRequest:
    return DeleteWorkflowProviderDefinitionRequest(definition_id=value.definition_id)


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
        idempotency_key=value.idempotency_key,
        definition_id=value.definition_id,
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
        schedules=[bound_workflow_schedule(item) for item in (response.schedules or [])]
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
        idempotency_key=value.idempotency_key,
        definition_id=value.definition_id,
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


def publish_workflow_provider_event_request_from_proto(
    value: Any,
) -> PublishWorkflowProviderEventRequest:
    return PublishWorkflowProviderEventRequest(
        app_name=value.app_name,
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


class WorkflowProtocol(Protocol):
    """Fakeable contract for workflow calls."""

    def close(self) -> None:
        """Close the client."""

    def start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRun:
        """Start a workflow run."""

    def signal_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRunSignal:
        """Signal an existing workflow run."""

    def signal_or_start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRunSignal:
        """Signal a run, or start it when no matching run exists."""

    def create_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Create a reusable workflow definition."""

    def get_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Fetch one workflow definition."""

    def update_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Update a workflow definition."""

    def delete_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> None:
        """Delete a workflow definition."""

    def create_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Create a workflow schedule."""

    def get_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Fetch one workflow schedule."""

    def update_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Update a workflow schedule."""

    def delete_schedule(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete a workflow schedule."""

    def pause_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Pause a workflow schedule."""

    def resume_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Resume a workflow schedule."""

    def create_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Create an event trigger."""

    def get_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Fetch one event trigger."""

    def update_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Update an event trigger."""

    def delete_trigger(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete an event trigger."""

    def pause_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Pause an event trigger."""

    def resume_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Resume an event trigger."""

    def publish_event(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEvent | None:
        """Publish an event into the workflow."""


class Workflow:
    """Client for starting runs and managing workflow schedules or triggers.

    This capability is for provider code that receives an invocation token. Methods
    attach that token to each request before calling the host service. The
    optional ``idempotency_key`` is used for create requests that do not already
    include one.
    """

    def __init__(self, invocation_token: str, *, idempotency_key: str = "") -> None:
        trimmed_token = invocation_token.strip()
        if not trimmed_token:
            raise RuntimeError("workflow: invocation token is not available")

        target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
        if not target:
            raise RuntimeError(
                f"workflow: {ENV_HOST_SERVICE_SOCKET} is not set"
            )
        relay_token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "")

        self._channel = host_service_channel(
            "workflow", target, token=relay_token
        )
        self._stub = pb_grpc.WorkflowProviderStub(self._channel)
        self._invocation_token = trimmed_token
        self._idempotency_key = idempotency_key.strip()

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRun:
        """Start a workflow run."""

        request = _workflow_start_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_run_from_proto(_grpc_call(self._stub.StartRun, request))

    def signal_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRunSignal:
        """Signal an existing workflow run."""

        request = _workflow_signal_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_run_signal_from_proto(
            _grpc_call(self._stub.SignalRun, request)
        )

    def signal_or_start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowRunSignal:
        """Signal a run, or start it when no matching run exists."""

        request = _workflow_signal_or_start_run_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_run_signal_from_proto(
            _grpc_call(self._stub.SignalOrStartRun, request)
        )

    def create_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Create a reusable workflow definition."""

        request = _workflow_create_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_definition_from_proto(
            _grpc_call(self._stub.CreateDefinition, request)
        )

    def get_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Fetch one workflow definition."""

        request = _workflow_get_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_definition_from_proto(
            _grpc_call(self._stub.GetDefinition, request)
        )

    def update_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowDefinition:
        """Update a workflow definition."""

        request = _workflow_update_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_definition_from_proto(
            _grpc_call(self._stub.UpdateDefinition, request)
        )

    def delete_definition(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete a workflow definition."""

        request = _workflow_delete_definition_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteDefinition, request)
        return None

    def create_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Create a workflow schedule."""

        request = _workflow_create_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_schedule_from_proto(
            _grpc_call(self._stub.UpsertSchedule, request)
        )

    def get_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Fetch one workflow schedule."""

        request = _workflow_get_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_schedule_from_proto(
            _grpc_call(self._stub.GetSchedule, request)
        )

    def update_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Update a workflow schedule."""

        request = _workflow_update_schedule_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_schedule_from_proto(
            _grpc_call(self._stub.UpsertSchedule, request)
        )

    def delete_schedule(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete a workflow schedule."""

        request = _workflow_id_request(
            pb.DeleteWorkflowProviderScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteSchedule, request)
        return None

    def pause_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Pause a workflow schedule."""

        request = _workflow_id_request(
            pb.PauseWorkflowProviderScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_schedule_from_proto(
            _grpc_call(self._stub.PauseSchedule, request)
        )

    def resume_schedule(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowSchedule:
        """Resume a workflow schedule."""

        request = _workflow_id_request(
            pb.ResumeWorkflowProviderScheduleRequest, "schedule_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_schedule_from_proto(
            _grpc_call(self._stub.ResumeSchedule, request)
        )

    def create_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Create an event trigger."""

        request = _workflow_create_event_trigger_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_event_trigger_from_proto(
            _grpc_call(self._stub.UpsertEventTrigger, request)
        )

    def get_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Fetch one event trigger."""

        request = _workflow_id_request(
            pb.GetWorkflowProviderEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_event_trigger_from_proto(
            _grpc_call(self._stub.GetEventTrigger, request)
        )

    def update_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Update an event trigger."""

        request = _workflow_update_event_trigger_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_event_trigger_from_proto(
            _grpc_call(self._stub.UpsertEventTrigger, request)
        )

    def delete_trigger(self, request: Any | None = None, **kwargs: Any) -> None:
        """Delete an event trigger."""

        request = _workflow_id_request(
            pb.DeleteWorkflowProviderEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        _grpc_call(self._stub.DeleteEventTrigger, request)
        return None

    def pause_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Pause an event trigger."""

        request = _workflow_id_request(
            pb.PauseWorkflowProviderEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_event_trigger_from_proto(
            _grpc_call(self._stub.PauseEventTrigger, request)
        )

    def resume_trigger(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEventTrigger:
        """Resume an event trigger."""

        request = _workflow_id_request(
            pb.ResumeWorkflowProviderEventTriggerRequest, "trigger_id", request, **kwargs
        )
        request.invocation_token = self._invocation_token
        return workflow_event_trigger_from_proto(
            _grpc_call(self._stub.ResumeEventTrigger, request)
        )

    def publish_event(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowEvent | None:
        """Publish an event into the workflow host."""

        request = _workflow_publish_event_request(request, **kwargs)
        request.invocation_token = self._invocation_token
        return workflow_event_input_from_event(
            _grpc_call(self._stub.PublishEvent, request)
        )

    def __enter__(self) -> Workflow:
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

@_dataclasses.dataclass(slots=True)
class WorkflowExecutionRequest:
    provider_name: str = ""
    run_id: str = ""
    target: BoundWorkflowTarget | None = None
    trigger: WorkflowRunTrigger | None = None
    input: Mapping[str, Any] | None = None
    metadata: Mapping[str, Any] | None = None
    created_by: Any | None = None
    invocation_token: str = ""
    signals: Sequence[WorkflowSignal] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowEvalContext:
    request: WorkflowExecutionRequest
    outputs: Mapping[str, Any] | None = None
    inputs: Mapping[str, Any] | None = None
    allow_inputs: bool = False


class WorkflowValueError(ValueError):
    pass


def evaluate_workflow_step_inputs(
    ctx: WorkflowEvalContext, values: Mapping[str, WorkflowValue] | None
) -> dict[str, Any] | None:
    if not values:
        return None
    out: dict[str, Any] = {}
    for key, value in values.items():
        resolved, ok = evaluate_workflow_value(ctx, value)
        if not ok:
            raise WorkflowValueError(f"inputs.{key} did not resolve")
        out[key] = resolved
    return out


def evaluate_workflow_value(ctx: WorkflowEvalContext, value: WorkflowValue) -> tuple[Any, bool]:
    if _literal_is_set(value):
        return value.literal, True
    if value.object is not None:
        out: dict[str, Any] = {}
        for key, child in value.object.items():
            resolved, ok = evaluate_workflow_value(ctx, child)
            if not ok:
                return None, False
            out[key] = resolved
        return out, True
    if value.array is not None:
        out = []
        for child in value.array:
            resolved, ok = evaluate_workflow_value(ctx, child)
            if not ok:
                return None, False
            out.append(resolved)
        return out, True
    if value.template is not None:
        template = value.template.template if hasattr(value.template, "template") else str(value.template)
        return render_workflow_template(ctx, template), True
    if value.run_input.strip():
        return map_path_value(ctx.request.input, value.run_input)
    if value.signal_payload.strip():
        signal = latest_workflow_signal(ctx.request.signals)
        if signal is None:
            return None, False
        return path_value(signal.payload, value.signal_payload)
    if value.step_output is not None:
        step_id = value.step_output.step_id.strip()
        outputs = ctx.outputs or {}
        if step_id not in outputs:
            raise WorkflowValueError(f'workflow step output references missing step "{step_id}"')
        return path_value(outputs[step_id], value.step_output.path)
    return None, True


def render_workflow_template(ctx: WorkflowEvalContext, template: str) -> str:
    out: list[str] = []
    i = 0
    while i < len(template):
        if template.startswith("$${", i):
            out.append("${")
            i += 3
            continue
        if not template.startswith("${", i):
            out.append(template[i])
            i += 1
            continue
        end = template.find("}", i + 2)
        if end < 0:
            raise WorkflowValueError("unterminated template expression")
        expr = template[i + 2 : end].strip()
        value, ok = _template_expression_value(ctx, expr)
        if not ok:
            raise WorkflowValueError(f'template expression "{expr}" did not resolve')
        out.append(_render_template_value(value))
        i = end + 1
    return "".join(out)


def workflow_invocation_context(req: WorkflowExecutionRequest) -> dict[str, Any]:
    out: dict[str, Any] = {}
    if req.run_id.strip():
        out["runId"] = req.run_id.strip()
    if req.provider_name.strip():
        out["provider"] = req.provider_name.strip()
    target = _workflow_target_context(req.target)
    if target:
        out["target"] = target
    trigger = _workflow_trigger_context(req.trigger)
    if trigger:
        out["trigger"] = trigger
    if req.input is not None:
        out["input"] = dict(req.input)
    if req.metadata is not None:
        out["metadata"] = dict(req.metadata)
    signal_context = workflow_signals_context(req.signals)
    if signal_context:
        out["signals"] = signal_context
    created_by = _workflow_actor_context(req.created_by)
    if created_by:
        out["createdBy"] = created_by
    return out


def _workflow_target_context(target: BoundWorkflowTarget | Any | None) -> dict[str, Any]:
    if target is None:
        return {}
    raw_steps = getattr(target, "steps", None)
    if not raw_steps:
        return {}
    target_steps = cast(Sequence[Any], raw_steps)
    steps: list[dict[str, Any]] = []
    for step in target_steps:
        item: dict[str, Any] = {"id": getattr(step, "id", "").strip()}
        app = getattr(step, "app", None)
        agent = getattr(step, "agent", None)
        if app is not None:
            item["kind"] = "app"
            item["app"] = getattr(app, "name", "").strip()
            item["operation"] = getattr(app, "operation", "").strip()
            if getattr(app, "connection", "").strip():
                item["connection"] = app.connection.strip()
            if getattr(app, "instance", "").strip():
                item["instance"] = app.instance.strip()
            if getattr(app, "credential_mode", "").strip():
                item["credentialMode"] = app.credential_mode.strip()
        elif agent is not None:
            item["kind"] = "agent"
            item["agentProvider"] = getattr(agent, "provider", "").strip()
            item["model"] = getattr(agent, "model", "").strip()
        else:
            item["kind"] = "unknown"
        steps.append(item)
    return {"kind": "steps", "steps": steps}


def _workflow_trigger_context(trigger: WorkflowRunTrigger | Any | None) -> dict[str, Any]:
    if trigger is None:
        return {}
    if getattr(trigger, "schedule", None) is not None:
        schedule = trigger.schedule
        out: dict[str, Any] = {
            "kind": "schedule",
            "scheduleId": getattr(schedule, "schedule_id", ""),
        }
        scheduled_for = _format_workflow_time(getattr(schedule, "scheduled_for", None))
        if scheduled_for:
            out["scheduledFor"] = scheduled_for
        return out
    if getattr(trigger, "event", None) is not None:
        event_trigger = trigger.event
        out = {
            "kind": "event",
            "triggerId": getattr(event_trigger, "trigger_id", ""),
        }
        event = _workflow_event_context(getattr(event_trigger, "event", None))
        if event:
            out["event"] = event
        return out
    if getattr(trigger, "manual", False):
        return {"kind": "manual"}
    return {}


def _workflow_event_context(event: WorkflowEvent | Any | None) -> dict[str, Any]:
    if event is None:
        return {}
    out: dict[str, Any] = {}
    if getattr(event, "id", "").strip():
        out["id"] = event.id.strip()
    if getattr(event, "source", "").strip():
        out["source"] = event.source.strip()
    if getattr(event, "spec_version", "").strip():
        out["specVersion"] = event.spec_version.strip()
    if getattr(event, "type", "").strip():
        out["type"] = event.type.strip()
    if getattr(event, "subject", "").strip():
        out["subject"] = event.subject.strip()
    event_time = _format_workflow_time(getattr(event, "time", None))
    if event_time:
        out["time"] = event_time
    if getattr(event, "datacontenttype", "").strip():
        out["dataContentType"] = event.datacontenttype.strip()
    if getattr(event, "data", None) is not None:
        out["data"] = json_from_native(event.data)
    extensions = getattr(event, "extensions", None)
    if extensions is not None:
        out["extensions"] = dict(cast(Mapping[str, Any], extensions))
    return out


def _workflow_actor_context(actor: WorkflowActor | Any | None) -> dict[str, Any]:
    if actor is None:
        return {}
    out: dict[str, Any] = {}
    if getattr(actor, "subject_id", "").strip():
        out["subjectId"] = actor.subject_id.strip()
    if getattr(actor, "subject_kind", "").strip():
        out["subjectKind"] = actor.subject_kind.strip()
    if getattr(actor, "display_name", "").strip():
        out["displayName"] = actor.display_name.strip()
    if getattr(actor, "auth_source", "").strip():
        out["authSource"] = actor.auth_source.strip()
    return out


def workflow_signals_context(signals: Sequence[WorkflowSignal] | None) -> list[dict[str, Any]]:
    if not signals:
        return []
    out: list[dict[str, Any]] = []
    for signal in list(signals)[:10]:
        item: dict[str, Any] = {}
        if signal.id.strip():
            item["id"] = signal.id.strip()
        if signal.name.strip():
            item["name"] = signal.name.strip()
        if signal.payload is not None:
            payload = _compact_workflow_signal_payload(signal.payload)
            if payload:
                item["payload"] = payload
        if signal.metadata is not None:
            item["metadata"] = _compact_json_value(signal.metadata, 4)
        created_by = _workflow_actor_context(signal.created_by)
        if created_by:
            item["createdBy"] = created_by
        created_at = _format_workflow_time(signal.created_at)
        if created_at:
            item["createdAt"] = created_at
        if signal.idempotency_key.strip():
            item["idempotencyKey"] = signal.idempotency_key.strip()
        if signal.sequence:
            item["sequence"] = signal.sequence
        out.append(item)
    return out


def latest_workflow_signal(signals: Sequence[WorkflowSignal] | None) -> WorkflowSignal | None:
    if not signals:
        return None
    return signals[-1]


def map_path_value(values: Mapping[str, Any] | None, path: str) -> tuple[Any, bool]:
    if not values:
        return None, False
    return path_value(values, path)


def path_value(root: Any, path: str) -> tuple[Any, bool]:
    path = path.strip()
    if not path:
        return root, True
    current = root
    for segment in _path_segments(path):
        if isinstance(current, Mapping) and isinstance(segment, str):
            if segment not in current:
                return None, False
            current = current[segment]
        elif isinstance(current, Sequence) and not isinstance(current, (str, bytes)) and isinstance(segment, int):
            if segment < 0 or segment >= len(current):
                return None, False
            current = current[segment]
        else:
            return None, False
    return current, True


def _template_expression_value(ctx: WorkflowEvalContext, expr: str) -> tuple[Any, bool]:
    if expr.startswith("inputs."):
        if not ctx.allow_inputs:
            raise WorkflowValueError("inputs references are not allowed here")
        return map_path_value(ctx.inputs, expr.removeprefix("inputs."))
    if expr.startswith("runInput."):
        return map_path_value(ctx.request.input, expr.removeprefix("runInput."))
    if expr.startswith("signalPayload."):
        signal = latest_workflow_signal(ctx.request.signals)
        if signal is None:
            return None, False
        return path_value(signal.payload, expr.removeprefix("signalPayload."))
    raise WorkflowValueError(f'unsupported template expression "{expr}"')


def _render_template_value(value: Any) -> str:
    if isinstance(value, str):
        return value
    return json.dumps(value, separators=(",", ":"))


def _path_segments(path: str) -> list[str | int]:
    out: list[str | int] = []
    i = 0
    while i < len(path):
        if path[i] == ".":
            i += 1
            continue
        if path[i] == "[":
            end = path.find("]", i)
            if end < 0:
                raise WorkflowValueError(f'invalid workflow path "{path}"')
            token = path[i + 1 : end].strip()
            if token.startswith(("'", '"')):
                out.append(ast.literal_eval(token))
            else:
                out.append(int(token))
            i = end + 1
            continue
        start = i
        while i < len(path) and path[i] not in ".[":
            i += 1
        key = path[start:i].strip()
        if not key:
            raise WorkflowValueError(f'invalid workflow path "{path}"')
        out.append(key)
    return out


def _format_workflow_time(value: Any | None) -> str:
    if value is None:
        return ""
    if isinstance(value, _timestamp_pb2.Timestamp):
        value = datetime_from_timestamp(value)
    if not isinstance(value, _dt.datetime):
        return ""
    if value.tzinfo is None:
        value = value.replace(tzinfo=_dt.timezone.utc)
    return value.astimezone(_dt.timezone.utc).isoformat().replace("+00:00", "Z")


def _compact_workflow_signal_payload(payload: Any) -> dict[str, Any]:
    source = _workflow_map_value(payload)
    if not source:
        return {}
    out: dict[str, Any] = {}
    for key in (
        "delivery_id",
        "deliveryId",
        "github_event",
        "githubEvent",
        "github_action",
        "githubAction",
        "event",
        "action",
        "summary",
        "user_prompt",
        "userPrompt",
        "payload_sha256",
        "payloadSha256",
        "payload_omitted",
        "payloadOmitted",
    ):
        _copy_compact_payload_field(out, source, key)
    for key in (
        "agent_request",
        "agentRequest",
        "installation",
        "repository",
        "sender",
        "webhook_policy",
        "webhookPolicy",
        "pull_request",
        "pullRequest",
        "issue",
        "comment",
        "review",
        "ref",
        "check_run",
        "checkRun",
        "check_suite",
        "checkSuite",
        "workflow_run",
        "workflowRun",
        "review_check_run",
        "reviewCheckRun",
    ):
        if key in source:
            out[key] = _compact_json_value(source[key], 4)
    fields: dict[str, Any] = {}
    for key in sorted(source):
        if len(fields) >= 20:
            break
        if key in out or _workflow_signal_payload_key_excluded(key):
            continue
        compact, ok = _compact_json_scalar(source[key])
        if ok:
            fields[key] = compact
    if fields:
        out["fields"] = fields
    out["payloadOmitted"] = True
    return out


def _workflow_map_value(value: Any) -> dict[str, Any]:
    if isinstance(value, Mapping):
        return dict(value)
    return {"value": value}


def _copy_compact_payload_field(
    out: dict[str, Any], payload: Mapping[str, Any], key: str
) -> None:
    if key not in payload or _workflow_signal_payload_key_excluded(key):
        return
    compact, ok = _compact_json_scalar(payload[key])
    out[key] = compact if ok else _compact_json_value(payload[key], 4)


def _workflow_signal_payload_key_excluded(key: str) -> bool:
    return key.strip() in ("", "payload", "_gestalt_payload_preview_json")


def _compact_json_scalar(value: Any) -> tuple[Any, bool]:
    if value is None or isinstance(value, (str, bool, int, float)):
        if isinstance(value, str):
            return _truncate_workflow_string(value, 4096), True
        return value, True
    return None, False


def _compact_json_value(value: Any, depth: int) -> Any:
    scalar, ok = _compact_json_scalar(value)
    if ok:
        return scalar
    if depth <= 0:
        return {"omitted": True}
    if isinstance(value, Mapping):
        out: dict[str, Any] = {}
        items = sorted((str(k), item) for k, item in value.items())
        items = [
            (key, item)
            for key, item in items
            if not _workflow_signal_payload_key_excluded(key)
        ]
        for key, item in items[:20]:
            out[key] = _compact_json_value(item, depth - 1)
        if len(items) > len(out):
            out["omittedFields"] = len(items) - len(out)
        return out
    if isinstance(value, Sequence) and not isinstance(value, (str, bytes)):
        return [_compact_json_value(item, depth - 1) for item in list(value)[:20]]
    return str(value)


def _truncate_workflow_string(value: str, max_bytes: int) -> str:
    if len(value.encode()) <= max_bytes:
        return value
    suffix = "..."
    body = value.encode()[: max_bytes - len(suffix)].decode(errors="ignore")
    while len((body + suffix).encode()) > max_bytes and body:
        body = body[:-1]
    return body + suffix


def _literal_is_set(value: WorkflowValue) -> bool:
    return bool(getattr(value, "literal_set", False)) or value.literal is not _MISSING
