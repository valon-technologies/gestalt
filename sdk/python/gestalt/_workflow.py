from __future__ import annotations

import ast
import dataclasses as _dataclasses
import datetime as _dt
import json
import math
import os
from collections.abc import Mapping, Sequence
from typing import Any, Protocol, TypeAlias, cast

import grpc
from google.protobuf import message as _message

from ._agent import (
    AgentOutput,
    AgentToolRef,
    _agent_output_from_proto,
    _agent_output_to_proto,
    agent_message_from_dict,
    agent_message_from_proto,
    agent_message_to_proto,
    agent_tool_ref_from_dict,
    agent_tool_ref_from_proto,
    agent_tool_ref_to_proto,
    subject_from_proto,
    subject_to_proto,
)
from ._api import Request, Subject
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

WORKFLOW_STEP_STATUS_UNSPECIFIED = pb.WORKFLOW_STEP_STATUS_UNSPECIFIED
WORKFLOW_STEP_STATUS_PENDING = pb.WORKFLOW_STEP_STATUS_PENDING
WORKFLOW_STEP_STATUS_RUNNING = pb.WORKFLOW_STEP_STATUS_RUNNING
WORKFLOW_STEP_STATUS_SKIPPED = pb.WORKFLOW_STEP_STATUS_SKIPPED
WORKFLOW_STEP_STATUS_SUCCEEDED = pb.WORKFLOW_STEP_STATUS_SUCCEEDED
WORKFLOW_STEP_STATUS_FAILED = pb.WORKFLOW_STEP_STATUS_FAILED
WORKFLOW_STEP_STATUS_UNKNOWN = pb.WORKFLOW_STEP_STATUS_UNKNOWN

WorkflowJsonScalar: TypeAlias = None | bool | int | float | str
WorkflowJsonNonNullScalar: TypeAlias = bool | int | float | str
WorkflowJsonValue: TypeAlias = (
    WorkflowJsonScalar | list["WorkflowJsonValue"] | dict[str, "WorkflowJsonValue"]
)
WorkflowJsonObject: TypeAlias = dict[str, WorkflowJsonValue]
WorkflowRequestMapping: TypeAlias = Mapping[str, object]


class _WorkflowUnset:
    __slots__ = ()


_UNSET = _WorkflowUnset()


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowText:
    """Native data for templated workflow text."""

    __hash__ = None

    template: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepOutputSource:
    """Native data for a workflow step output value source."""

    __hash__ = None

    step_id: str = ""
    path: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowValue:
    """Native data for a workflow value expression."""

    __hash__ = None

    literal: WorkflowJsonValue | _WorkflowUnset = _UNSET
    object: Mapping[str, "WorkflowValuePayload"] | None = None
    array: list["WorkflowValuePayload"] | tuple["WorkflowValuePayload", ...] | None = (
        None
    )
    template: WorkflowText | str | None = None
    input: str = ""
    signal: str = ""
    step_output: WorkflowStepOutputSource | None = None
    step_input: WorkflowStepOutputSource | None = None


WorkflowValuePayload: TypeAlias = (
    WorkflowValue
    | WorkflowJsonScalar
    | Mapping[str, "WorkflowValuePayload"]
    | list["WorkflowValuePayload"]
    | tuple["WorkflowValuePayload", ...]
)
WorkflowValueInput: TypeAlias = (
    WorkflowValue
    | WorkflowJsonNonNullScalar
    | Mapping[str, WorkflowValuePayload]
    | list[WorkflowValuePayload]
    | tuple[WorkflowValuePayload, ...]
)


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepAppCall:
    """Native data for a workflow app step call."""

    __hash__ = None

    name: str = ""
    operation: str = ""
    input: WorkflowValueInput | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepAgentTurn:
    """Native data for a workflow agent step turn."""

    __hash__ = None

    provider: str = ""
    model: str = ""
    session_key: str = ""
    prompt: WorkflowText | str | None = None
    messages: Sequence["WorkflowAgentMessage"] = _dataclasses.field(
        default_factory=list
    )
    tools: Sequence[AgentToolRef] = _dataclasses.field(default_factory=list)
    output: AgentOutput | Mapping[str, Any] | None = None
    model_options: WorkflowJsonObject | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowAgentMessage:
    """Native data for a workflow agent message."""

    __hash__ = None

    role: str = ""
    text: WorkflowText | str | None = None
    metadata: WorkflowJsonObject | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepWhen:
    """Native data for a workflow step condition."""

    __hash__ = None

    value: WorkflowValueInput | None = None
    equals: WorkflowJsonValue | _WorkflowUnset = _UNSET


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStep:
    """Native data for one workflow step."""

    __hash__ = None

    id: str = ""
    inputs: Mapping[str, WorkflowValuePayload] = _dataclasses.field(
        default_factory=dict
    )
    app: WorkflowStepAppCall | None = None
    agent: WorkflowStepAgentTurn | None = None
    when: WorkflowStepWhen | None = None
    timeout_seconds: int = 0
    metadata: WorkflowJsonObject | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class BoundWorkflowTarget:
    """Native data for a bound workflow target."""

    __hash__ = None

    steps: Sequence[WorkflowStep] = _dataclasses.field(default_factory=list)


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowEvent:
    """Native data for a workflow event."""

    __hash__ = None

    id: str = ""
    source: str = ""
    spec_version: str = ""
    type: str = ""
    subject: str = ""
    time: _dt.datetime | Any | None = None
    datacontenttype: str = ""
    data: WorkflowJsonObject | None = None
    extensions: WorkflowJsonObject | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowEventMatch:
    """Native data for workflow event matching fields."""

    __hash__ = None

    type: str = ""
    source: str = ""
    subject: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowActivation:
    """Native data for a compiled workflow activation."""

    __hash__ = None

    id: str = ""
    input: WorkflowValueInput | None = None
    paused: bool = False
    schedule: Mapping[str, Any] | None = None
    event: Mapping[str, Any] | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowDefinitionSpec:
    """Native data for applying a workflow definition."""

    __hash__ = None

    id: str = ""
    target: BoundWorkflowTarget | None = None
    activations: Sequence[WorkflowActivation | Mapping[str, Any]] = _dataclasses.field(
        default_factory=list
    )
    paused: bool = False
    run_as: Subject | Mapping[str, Any] | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowSignal:
    """Native data for a workflow signal."""

    __hash__ = None

    id: str = ""
    name: str = ""
    payload: WorkflowJsonObject | None = None
    metadata: WorkflowJsonObject | None = None
    created_by_subject_id: str = ""
    created_at: _dt.datetime | Any | None = None
    idempotency_key: str = ""
    sequence: int = 0


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowScheduleTrigger:
    """Native data for a schedule-triggered workflow run."""

    __hash__ = None

    activation_id: str = ""
    scheduled_for: _dt.datetime | Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowEventTriggerInvocation:
    """Native data for an event-triggered workflow run."""

    __hash__ = None

    activation_id: str = ""
    event: WorkflowEvent | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunTrigger:
    """Native data for a workflow run trigger."""

    __hash__ = None

    manual: bool = False
    schedule: WorkflowScheduleTrigger | None = None
    event: WorkflowEventTriggerInvocation | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepAttempt:
    """Native data for one durable workflow step attempt."""

    __hash__ = None

    id: str = ""
    status: int = WORKFLOW_STEP_STATUS_UNSPECIFIED
    idempotency_key: str = ""
    input: WorkflowJsonValue | None = None
    output: WorkflowJsonValue | None = None
    status_message: str = ""
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStepExecution:
    """Native data for one durable workflow step execution."""

    __hash__ = None

    step_id: str = ""
    status: int = WORKFLOW_STEP_STATUS_UNSPECIFIED
    attempts: Sequence[WorkflowStepAttempt] = _dataclasses.field(default_factory=list)
    input: WorkflowJsonValue | None = None
    output: WorkflowJsonValue | None = None
    status_message: str = ""
    skip_reason: str = ""
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRun:
    """Native data for a workflow-provider run."""

    __hash__ = None

    id: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    target: BoundWorkflowTarget | None = None
    trigger: WorkflowRunTrigger | None = None
    created_at: _dt.datetime | Any | None = None
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None
    status_message: str = ""
    output: WorkflowJsonValue | None = None
    created_by_subject_id: str = ""
    workflow_key: str = ""
    provider_name: str = ""
    definition_id: str = ""
    run_as: Subject | Mapping[str, Any] | None = None
    input: WorkflowJsonObject | None = None
    definition_generation: int = 0
    current_step_id: str = ""
    steps: Sequence[WorkflowStepExecution] = _dataclasses.field(default_factory=list)


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowDefinition:
    """Native data copied from a workflow-provider definition."""

    __hash__ = None

    id: str = ""
    generation: int = 0
    target: BoundWorkflowTarget | None = None
    activations: Sequence[WorkflowActivation] = _dataclasses.field(default_factory=list)
    paused: bool = False
    created_by_subject_id: str = ""
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    provider_name: str = ""
    run_as: Subject | Mapping[str, Any] | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowStartRun:
    __hash__ = None
    provider_name: str = ""
    idempotency_key: str = ""
    workflow_key: str = ""
    definition_id: str = ""
    input: WorkflowJsonObject | None = None
    run_as: Subject | Mapping[str, Any] | None = None
    expected_definition_generation: int = 0


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowSignalRun:
    __hash__ = None
    run_id: str = ""
    signal: WorkflowSignal | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowSignalOrStartRun:
    __hash__ = None
    provider_name: str = ""
    workflow_key: str = ""
    idempotency_key: str = ""
    signal: WorkflowSignal | None = None
    definition_id: str = ""
    input: WorkflowJsonObject | None = None
    run_as: Subject | Mapping[str, Any] | None = None
    expected_definition_generation: int = 0


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowApplyDefinition:
    __hash__ = None
    provider_name: str = ""
    spec: WorkflowDefinitionSpec | None = None
    idempotency_key: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowGetDefinition:
    __hash__ = None
    definition_id: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowDeleteDefinition:
    __hash__ = None
    definition_id: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowSetDefinitionPaused:
    __hash__ = None
    definition_id: str = ""
    paused: bool = False


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowSetActivationPaused:
    __hash__ = None
    definition_id: str = ""
    activation_id: str = ""
    paused: bool = False


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowGetRun:
    __hash__ = None
    run_id: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowGetRunEvents:
    __hash__ = None
    run_id: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowGetRunOutput:
    __hash__ = None
    run_id: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowDeliverEvent:
    __hash__ = None
    app_name: str = ""
    provider_name: str = ""
    event: WorkflowEvent | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunSignal:
    __hash__ = None
    provider_name: str = ""
    run: WorkflowRun | None = None
    signal: WorkflowSignal | None = None
    started_run: bool = False
    workflow_key: str = ""


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


def _agent_tool_ref_input_list(values: Sequence[Any] | None) -> list[AgentToolRef]:
    if values is None:
        return []
    output: list[AgentToolRef] = []
    for item in values:
        if isinstance(item, _app_pb.AgentToolRef):
            output.append(agent_tool_ref_from_proto(item))
        else:
            output.append(agent_tool_ref_from_dict(item))
    return output


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


def workflow_activation(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow activation."""

    if isinstance(value, pb.WorkflowActivation):
        return _copy(value)
    data = _data(value, kwargs)
    activation = pb.WorkflowActivation(
        id=data.get("id", ""),
        paused=data.get("paused", False),
    )
    if data.get("input") is not None:
        activation.input.CopyFrom(_workflow_value_nested(data["input"]))
    if data.get("schedule") is not None:
        schedule_data = _message_mapping(data["schedule"])
        activation.schedule.CopyFrom(
            pb.WorkflowScheduleActivation(
                cron=schedule_data.get("cron", ""),
                timezone=schedule_data.get("timezone", ""),
            )
        )
    if data.get("event") is not None:
        event_data = _message_mapping(data["event"])
        match = event_data.get("match")
        activation.event.CopyFrom(
            pb.WorkflowEventActivation(
                match=workflow_event_match(match) if match is not None else None
            )
        )
    return activation


def workflow_activation_input_from_activation(
    value: Any | None,
) -> WorkflowActivation | None:
    """Return input copied from a workflow activation."""

    if value is None:
        return None
    trigger = which_oneof(value, "trigger")
    return WorkflowActivation(
        id=value.id,
        input=workflow_value_input_from_value(value.input)
        if has_field(value, "input")
        else None,
        paused=value.paused,
        schedule={"cron": value.schedule.cron, "timezone": value.schedule.timezone}
        if trigger == "schedule"
        else None,
        event={"match": workflow_event_match_input_from_match(value.event.match)}
        if trigger == "event" and has_field(value.event, "match")
        else None,
    )


def workflow_definition_spec(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow definition spec."""

    if isinstance(value, pb.WorkflowDefinitionSpec):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowDefinitionSpec(
        id=data.get("id", ""),
        target=bound_workflow_target(target) if target is not None else None,
        activations=[workflow_activation(item) for item in data.get("activations", [])],
        paused=data.get("paused", False),
        run_as=subject_to_proto(data.get("run_as")),
    )


def workflow_definition_spec_input_from_spec(
    value: Any | None,
) -> WorkflowDefinitionSpec | None:
    """Return input copied from a workflow definition spec."""

    if value is None:
        return None
    return WorkflowDefinitionSpec(
        id=value.id,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        activations=[
            cast(WorkflowActivation, workflow_activation_input_from_activation(item))
            for item in value.activations
        ],
        paused=value.paused,
        run_as=subject_from_proto(value.run_as) if has_field(value, "run_as") else None,
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


def _workflow_value_nested(value: WorkflowValuePayload | Any) -> Any:
    if isinstance(value, pb.WorkflowValue):
        return _copy(value)
    if isinstance(value, WorkflowValue):
        return workflow_value(value)
    if isinstance(value, Mapping):
        return pb.WorkflowValue(
            object=pb.WorkflowObject(
                fields={
                    key: _workflow_value_nested(nested) for key, nested in value.items()
                }
            )
        )
    if isinstance(value, Sequence) and not isinstance(
        value, str | bytes | bytearray | memoryview
    ):
        return pb.WorkflowValue(
            array=pb.WorkflowArray(
                values=[_workflow_value_nested(nested) for nested in value]
            )
        )
    return pb.WorkflowValue(literal=_value(value))


def workflow_value(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow value expression."""

    if isinstance(value, pb.WorkflowValue):
        return _copy(value)
    if (
        value is not None
        and _dataclass_mapping(value) is None
        and not isinstance(value, Mapping)
    ):
        data = {"literal": value}
        data.update(kwargs)
    else:
        data = _data(value, kwargs)

    literal = data.get("literal", _UNSET)
    choices: list[tuple[str, Any]] = []
    if literal is not _UNSET:
        choices.append(("literal", literal))
    for name in (
        "object",
        "array",
        "template",
        "input",
        "signal",
        "step_output",
        "step_input",
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
                fields={
                    key: _workflow_value_nested(nested) for key, nested in item.items()
                }
            )
        )
    if name == "array":
        return pb.WorkflowValue(
            array=pb.WorkflowArray(
                values=[_workflow_value_nested(nested) for nested in item]
            )
        )
    if name == "template":
        return pb.WorkflowValue(template=workflow_text(item))
    if name == "input":
        return pb.WorkflowValue(input=_workflow_path_source(item))
    if name == "signal":
        return pb.WorkflowValue(signal=_workflow_path_source(item))
    if name == "step_output":
        return pb.WorkflowValue(step_output=workflow_step_output_source(item))
    if name == "step_input":
        return pb.WorkflowValue(step_input=workflow_step_output_source(item))
    raise AssertionError(f"unknown workflow value kind {name}")


def workflow_value_input_from_value(value: Any | None) -> WorkflowValue | None:
    """Return input copied from a workflow value expression."""

    if value is None:
        return None
    kind = which_oneof(value, "kind")
    if kind == "literal":
        return WorkflowValue(
            literal=cast(WorkflowJsonValue, value_to_json(value.literal))
        )
    if kind == "object":
        return WorkflowValue(
            object={
                key: cast(WorkflowValue, workflow_value_input_from_value(item))
                for key, item in value.object.fields.items()
            }
        )
    if kind == "array":
        return WorkflowValue(
            array=[
                cast(WorkflowValue, workflow_value_input_from_value(item))
                for item in value.array.values
            ]
        )
    if kind == "template":
        return WorkflowValue(template=workflow_text_input_from_text(value.template))
    if kind == "input":
        return WorkflowValue(input=value.input.path)
    if kind == "signal":
        return WorkflowValue(signal=value.signal.path)
    if kind == "step_output":
        return WorkflowValue(
            step_output=workflow_step_output_source_input_from_source(value.step_output)
        )
    if kind == "step_input":
        return WorkflowValue(
            step_input=workflow_step_output_source_input_from_source(value.step_input)
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
        input=_workflow_value_nested(input_value) if input_value is not None else None,
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
        metadata=cast(WorkflowJsonObject, struct_to_dict(value.metadata))
        if has_field(value, "metadata")
        else None,
    )


def _workflow_agent_message_proto_list(values: Sequence[Any] | None) -> list[Any]:
    if values is None:
        return []
    return [workflow_agent_message(item) for item in values]


def _workflow_agent_message_input_list(
    values: Sequence[Any] | None,
) -> list[WorkflowAgentMessage]:
    if values is None:
        return []
    return [
        cast(WorkflowAgentMessage, workflow_agent_message_input_from_message(item))
        for item in values
    ]


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
        output=_agent_output_to_proto(data.get("output")),
        model_options=_optional_struct(data.get("model_options")),
    )


def workflow_step_agent_turn_input_from_turn(
    value: Any | None,
) -> WorkflowStepAgentTurn | None:
    """Return input copied from a workflow agent step turn."""

    if value is None:
        return None
    output = (
        _agent_output_from_proto(value.output) if has_field(value, "output") else None
    )
    if output is None:
        raise ValueError("workflow agent output is required")
    return WorkflowStepAgentTurn(
        provider=value.provider,
        model=value.model,
        session_key=value.session_key,
        prompt=workflow_text_input_from_text(value.prompt)
        if has_field(value, "prompt")
        else None,
        messages=_workflow_agent_message_input_list(value.messages),
        tools=_agent_tool_ref_input_list(value.tools),
        output=output,
        model_options=cast(WorkflowJsonObject, struct_to_dict(value.model_options))
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
        condition.value.CopyFrom(_workflow_value_nested(data["value"]))
    equals = data.get("equals", _UNSET)
    if equals is not _UNSET:
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
        equals=cast(WorkflowJsonValue, value_to_json(value.equals))
        if has_field(value, "equals")
        else _UNSET,
    )


def workflow_step(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step."""

    if isinstance(value, pb.WorkflowStep):
        _validate_workflow_step_timeout(value)
        return _copy(value)
    data = _data(value, kwargs)
    app = data.get("app")
    agent = data.get("agent")
    if app is not None and agent is not None:
        raise ValueError("workflow step must set either app or agent")
    timeout_seconds = data.get("timeout_seconds", 0)
    if timeout_seconds < 0:
        raise ValueError("workflow step timeout_seconds must not be negative")
    step = pb.WorkflowStep(
        id=data.get("id", ""),
        inputs={
            key: _workflow_value_nested(item)
            for key, item in (data.get("inputs") or {}).items()
        },
        timeout_seconds=timeout_seconds,
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


def _validate_workflow_step_timeout(value: Any) -> None:
    if value.timeout_seconds < 0:
        raise ValueError("workflow step timeout_seconds must not be negative")


def workflow_step_input_from_step(value: Any | None) -> WorkflowStep | None:
    """Return input copied from a workflow step."""

    if value is None:
        return None
    return WorkflowStep(
        id=value.id,
        inputs={
            key: cast(WorkflowValue, workflow_value_input_from_value(item))
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
        for step in value.steps:
            _validate_workflow_step_timeout(step)
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
        steps=[
            cast(WorkflowStep, workflow_step_input_from_step(step))
            for step in value.steps
        ],
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
        data=cast(WorkflowJsonObject, struct_to_dict(value.data))
        if has_field(value, "data")
        else None,
        extensions={
            key: cast(WorkflowJsonValue, value_to_json(item))
            for key, item in value.extensions.items()
        },
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
    return pb.WorkflowSignal(
        id=data.get("id", ""),
        name=data.get("name", ""),
        payload=_optional_struct(data.get("payload")),
        metadata=_optional_struct(data.get("metadata")),
        created_by_subject_id=data.get("created_by_subject_id", ""),
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
        payload=cast(WorkflowJsonObject, struct_to_dict(value.payload))
        if has_field(value, "payload")
        else None,
        metadata=cast(WorkflowJsonObject, struct_to_dict(value.metadata))
        if has_field(value, "metadata")
        else None,
        created_by_subject_id=value.created_by_subject_id,
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
        activation_id=data.get("activation_id", ""),
        scheduled_for=_optional_timestamp(data.get("scheduled_for")),
    )


def workflow_event_trigger_invocation(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow event-trigger invocation ."""

    if isinstance(value, pb.WorkflowEventTriggerInvocation):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.WorkflowEventTriggerInvocation(
        activation_id=data.get("activation_id", ""),
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
                activation_id=value.schedule.activation_id,
                scheduled_for=_timestamp_to_datetime(value.schedule, "scheduled_for"),
            )
        )
    if kind == "event":
        return WorkflowRunTrigger(
            event=WorkflowEventTriggerInvocation(
                activation_id=value.event.activation_id,
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
    return pb.StartWorkflowProviderRunRequest(
        provider_name=data.get("provider_name", ""),
        idempotency_key=data.get("idempotency_key", ""),
        workflow_key=data.get("workflow_key", ""),
        definition_id=data.get("definition_id", ""),
        input=_optional_struct(data.get("input")),
        expected_definition_generation=data.get("expected_definition_generation", 0),
    )


def _workflow_signal_run_request(value: Any | None = None, **kwargs: Any) -> Any:
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
    signal = data.get("signal")
    return pb.SignalOrStartWorkflowProviderRunRequest(
        provider_name=data.get("provider_name", ""),
        workflow_key=data.get("workflow_key", ""),
        idempotency_key=data.get("idempotency_key", ""),
        signal=workflow_signal(signal) if signal is not None else None,
        definition_id=data.get("definition_id", ""),
        input=_optional_struct(data.get("input")),
        expected_definition_generation=data.get("expected_definition_generation", 0),
    )


def _workflow_apply_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ApplyWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    spec = data.get("spec")
    return pb.ApplyWorkflowProviderDefinitionRequest(
        provider_name=data.get("provider_name", ""),
        spec=workflow_definition_spec(spec) if spec is not None else None,
        idempotency_key=data.get("idempotency_key", ""),
    )


def _workflow_get_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowProviderDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_delete_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.DeleteWorkflowProviderDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.DeleteWorkflowProviderDefinitionRequest(
        definition_id=data.get("definition_id", "")
    )


def _workflow_set_definition_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SetWorkflowProviderDefinitionPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SetWorkflowProviderDefinitionPausedRequest(
        definition_id=data.get("definition_id", ""),
        paused=data.get("paused", False),
    )


def _workflow_set_activation_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SetWorkflowProviderActivationPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SetWorkflowProviderActivationPausedRequest(
        definition_id=data.get("definition_id", ""),
        activation_id=data.get("activation_id", ""),
        paused=data.get("paused", False),
    )


def _workflow_get_run_events_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowProviderRunEventsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowProviderRunEventsRequest(run_id=data.get("run_id", ""))


def _workflow_get_run_output_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowProviderRunOutputRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowProviderRunOutputRequest(run_id=data.get("run_id", ""))


def _workflow_deliver_event_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.DeliverWorkflowProviderEventRequest):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.DeliverWorkflowProviderEventRequest(
        app_name=data.get("app_name", ""),
        provider_name=data.get("provider_name", ""),
        event=workflow_event(event) if event is not None else None,
    )


def workflow_step_attempt(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step attempt."""

    if isinstance(value, pb.WorkflowStepAttempt):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepAttempt(
        id=data.get("id", ""),
        status=data.get("status", WORKFLOW_STEP_STATUS_UNSPECIFIED),
        idempotency_key=data.get("idempotency_key", ""),
        input=_optional_value(data.get("input")),
        output=_optional_value(data.get("output")),
        status_message=data.get("status_message", ""),
        started_at=_optional_timestamp(data.get("started_at")),
        completed_at=_optional_timestamp(data.get("completed_at")),
    )


def workflow_step_attempt_input_from_attempt(
    value: Any | None,
) -> WorkflowStepAttempt | None:
    """Return input copied from a workflow step attempt."""

    if value is None:
        return None
    return WorkflowStepAttempt(
        id=value.id,
        status=value.status,
        idempotency_key=value.idempotency_key,
        input=cast(WorkflowJsonValue, value_to_json(value.input))
        if has_field(value, "input")
        else None,
        output=cast(WorkflowJsonValue, value_to_json(value.output))
        if has_field(value, "output")
        else None,
        status_message=value.status_message,
        started_at=_timestamp_to_datetime(value, "started_at"),
        completed_at=_timestamp_to_datetime(value, "completed_at"),
    )


def workflow_step_execution(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow step execution."""

    if isinstance(value, pb.WorkflowStepExecution):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepExecution(
        step_id=data.get("step_id", ""),
        status=data.get("status", WORKFLOW_STEP_STATUS_UNSPECIFIED),
        attempts=[workflow_step_attempt(item) for item in data.get("attempts", [])],
        input=_optional_value(data.get("input")),
        output=_optional_value(data.get("output")),
        status_message=data.get("status_message", ""),
        skip_reason=data.get("skip_reason", ""),
        started_at=_optional_timestamp(data.get("started_at")),
        completed_at=_optional_timestamp(data.get("completed_at")),
    )


def workflow_step_execution_input_from_execution(
    value: Any | None,
) -> WorkflowStepExecution | None:
    """Return input copied from a workflow step execution."""

    if value is None:
        return None
    return WorkflowStepExecution(
        step_id=value.step_id,
        status=value.status,
        attempts=[
            cast(WorkflowStepAttempt, workflow_step_attempt_input_from_attempt(item))
            for item in value.attempts
        ],
        input=cast(WorkflowJsonValue, value_to_json(value.input))
        if has_field(value, "input")
        else None,
        output=cast(WorkflowJsonValue, value_to_json(value.output))
        if has_field(value, "output")
        else None,
        status_message=value.status_message,
        skip_reason=value.skip_reason,
        started_at=_timestamp_to_datetime(value, "started_at"),
        completed_at=_timestamp_to_datetime(value, "completed_at"),
    )


def workflow_run(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider run."""

    if isinstance(value, pb.WorkflowRun):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    trigger = data.get("trigger")
    return pb.WorkflowRun(
        id=data.get("id", ""),
        status=data.get("status", WORKFLOW_RUN_STATUS_UNSPECIFIED),
        target=bound_workflow_target(target) if target is not None else None,
        trigger=workflow_run_trigger(trigger) if trigger is not None else None,
        created_at=_optional_timestamp(data.get("created_at")),
        started_at=_optional_timestamp(data.get("started_at")),
        completed_at=_optional_timestamp(data.get("completed_at")),
        status_message=data.get("status_message", ""),
        output=_optional_value(data.get("output")),
        created_by_subject_id=data.get("created_by_subject_id", ""),
        workflow_key=data.get("workflow_key", ""),
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
        run_as=subject_to_proto(data.get("run_as")),
        input=_optional_struct(data.get("input")),
        definition_generation=data.get("definition_generation", 0),
        current_step_id=data.get("current_step_id", ""),
        steps=[workflow_step_execution(item) for item in data.get("steps", [])],
    )


def workflow_run_input_from_run(
    value: Any | None,
) -> WorkflowRun | None:
    """Return input copied from a workflow-provider run."""

    if value is None:
        return None
    return WorkflowRun(
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
        output=cast(WorkflowJsonValue, value_to_json(value.output))
        if has_field(value, "output")
        else None,
        created_by_subject_id=value.created_by_subject_id,
        workflow_key=value.workflow_key,
        provider_name=value.provider_name,
        definition_id=value.definition_id,
        run_as=subject_from_proto(value.run_as) if has_field(value, "run_as") else None,
        input=cast(WorkflowJsonObject, struct_to_dict(value.input))
        if has_field(value, "input")
        else None,
        definition_generation=value.definition_generation,
        current_step_id=value.current_step_id,
        steps=[
            cast(
                WorkflowStepExecution,
                workflow_step_execution_input_from_execution(item),
            )
            for item in value.steps
        ],
    )


def workflow_run_from_run(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider run."""

    data = workflow_run_input_from_run(value)
    return workflow_run(data) if data is not None else None


def workflow_definition(value: Any | None = None, **kwargs: Any) -> Any:
    """Create a workflow-provider definition."""

    if isinstance(value, pb.WorkflowDefinition):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    return pb.WorkflowDefinition(
        id=data.get("id", ""),
        generation=data.get("generation", 0),
        target=bound_workflow_target(target) if target is not None else None,
        activations=[workflow_activation(item) for item in data.get("activations", [])],
        paused=data.get("paused", False),
        created_by_subject_id=data.get("created_by_subject_id", ""),
        created_at=_optional_timestamp(data.get("created_at")),
        updated_at=_optional_timestamp(data.get("updated_at")),
        provider_name=data.get("provider_name", ""),
        run_as=subject_to_proto(data.get("run_as")),
    )


def workflow_definition_input_from_definition(
    value: Any | None,
) -> WorkflowDefinition | None:
    """Return input copied from a workflow-provider definition."""

    if value is None:
        return None
    return WorkflowDefinition(
        id=value.id,
        generation=value.generation,
        target=bound_workflow_target_input_from_target(value.target)
        if has_field(value, "target")
        else None,
        activations=[
            cast(WorkflowActivation, workflow_activation_input_from_activation(item))
            for item in value.activations
        ],
        paused=value.paused,
        created_by_subject_id=value.created_by_subject_id,
        created_at=_timestamp_to_datetime(value, "created_at"),
        updated_at=_timestamp_to_datetime(value, "updated_at"),
        provider_name=value.provider_name,
        run_as=subject_from_proto(value.run_as) if has_field(value, "run_as") else None,
    )


def workflow_definition_from_definition(value: Any | None) -> Any | None:
    """Return a deep copy of a workflow-provider definition."""

    data = workflow_definition_input_from_definition(value)
    return workflow_definition(data) if data is not None else None


def workflow_run_from_proto(value: Any) -> WorkflowRun:
    return workflow_run_input_from_run(value) or WorkflowRun()


def workflow_run_signal_from_proto(value: Any) -> WorkflowRunSignal:
    run = value.run if has_field(value, "run") else None
    return WorkflowRunSignal(
        provider_name=run.provider_name if run is not None else "",
        run=workflow_run_input_from_run(run),
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        started_run=value.started_run,
        workflow_key=value.workflow_key,
    )


def workflow_definition_from_proto(value: Any) -> WorkflowDefinition:
    return workflow_definition_input_from_definition(value) or WorkflowDefinition()


@_dataclasses.dataclass(frozen=True, slots=True)
class StartWorkflowProviderRunRequest:
    """Start-run request passed to workflow providers."""

    __hash__ = None

    idempotency_key: str = ""
    workflow_key: str = ""
    definition_id: str = ""
    input: WorkflowJsonObject | None = None
    expected_definition_generation: int = 0
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderRunRequest:
    """Get-run request passed to workflow providers."""

    __hash__ = None

    run_id: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class ListWorkflowProviderRunsRequest:
    """List-runs request passed to workflow providers."""

    __hash__ = None

    page_size: int = 0
    page_token: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    target_app: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class ListWorkflowProviderRunsResponse:
    """Runs returned by workflow providers."""

    __hash__ = None

    runs: Sequence[WorkflowRun] = _dataclasses.field(default_factory=list)
    next_page_token: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunEvent:
    """Run event returned by workflow providers."""

    __hash__ = None

    id: str = ""
    run_id: str = ""
    step_id: str = ""
    type: str = ""
    data: WorkflowJsonObject | None = None
    created_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderRunEventsRequest:
    """Get-run-events request passed to workflow providers."""

    __hash__ = None

    run_id: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderRunEventsResponse:
    """Run events returned by workflow providers."""

    __hash__ = None

    events: Sequence[WorkflowRunEvent] = _dataclasses.field(default_factory=list)


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderRunOutputRequest:
    """Get-run-output request passed to workflow providers."""

    __hash__ = None

    run_id: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderRunOutputResponse:
    """Run output returned by workflow providers."""

    __hash__ = None

    output: WorkflowJsonValue | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class CancelWorkflowProviderRunRequest:
    """Cancel-run request passed to workflow providers."""

    __hash__ = None

    run_id: str = ""
    reason: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class SignalWorkflowProviderRunRequest:
    """Signal-run request passed to workflow providers."""

    __hash__ = None

    run_id: str = ""
    signal: WorkflowSignal | None = None
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class SignalOrStartWorkflowProviderRunRequest:
    """Signal-or-start request passed to workflow providers."""

    __hash__ = None

    workflow_key: str = ""
    idempotency_key: str = ""
    signal: WorkflowSignal | None = None
    definition_id: str = ""
    input: WorkflowJsonObject | None = None
    expected_definition_generation: int = 0
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class SignalWorkflowRunResponse:
    """Signal-run response returned by workflow providers."""

    __hash__ = None

    run: WorkflowRun | None = None
    signal: WorkflowSignal | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(frozen=True, slots=True)
class ApplyWorkflowProviderDefinitionRequest:
    """Apply-definition request passed to workflow providers."""

    __hash__ = None

    spec: WorkflowDefinitionSpec | None = None
    idempotency_key: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class GetWorkflowProviderDefinitionRequest:
    """Get-definition request passed to workflow providers."""

    __hash__ = None

    definition_id: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class ListWorkflowProviderDefinitionsRequest:
    """List-definitions request passed to workflow providers."""

    __hash__ = None

    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class ListWorkflowProviderDefinitionsResponse:
    """Definitions returned by workflow providers."""

    __hash__ = None

    definitions: Sequence[WorkflowDefinition] = _dataclasses.field(default_factory=list)


@_dataclasses.dataclass(frozen=True, slots=True)
class SetWorkflowProviderDefinitionPausedRequest:
    """Set-definition-paused request passed to workflow providers."""

    __hash__ = None

    definition_id: str = ""
    paused: bool = False
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class SetWorkflowProviderActivationPausedRequest:
    """Set-activation-paused request passed to workflow providers."""

    __hash__ = None

    definition_id: str = ""
    activation_id: str = ""
    paused: bool = False
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class DeleteWorkflowProviderDefinitionRequest:
    """Delete-definition request passed to workflow providers."""

    __hash__ = None

    definition_id: str = ""
    context: Any | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class DeliverWorkflowProviderEventRequest:
    """Deliver-event request passed to workflow providers."""

    __hash__ = None

    app_name: str = ""
    event: WorkflowEvent | None = None
    context: Any | None = None


def start_workflow_provider_run_request_from_proto(
    value: Any,
) -> StartWorkflowProviderRunRequest:
    return StartWorkflowProviderRunRequest(
        idempotency_key=value.idempotency_key,
        workflow_key=value.workflow_key,
        definition_id=value.definition_id,
        input=cast(WorkflowJsonObject, struct_to_dict(value.input))
        if has_field(value, "input")
        else None,
        expected_definition_generation=value.expected_definition_generation,
        context=getattr(value, "context", None),
    )


def get_workflow_provider_run_request_from_proto(
    value: Any,
) -> GetWorkflowProviderRunRequest:
    return GetWorkflowProviderRunRequest(
        run_id=value.run_id,
        context=getattr(value, "context", None),
    )


def list_workflow_provider_runs_request_from_proto(
    value: Any,
) -> ListWorkflowProviderRunsRequest:
    return ListWorkflowProviderRunsRequest(
        page_size=int(value.page_size),
        page_token=value.page_token,
        status=value.status,
        target_app=value.target_app,
        context=getattr(value, "context", None),
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
        runs=[workflow_run(item) for item in response.runs],
        next_page_token=response.next_page_token,
    )


def cancel_workflow_provider_run_request_from_proto(
    value: Any,
) -> CancelWorkflowProviderRunRequest:
    return CancelWorkflowProviderRunRequest(
        run_id=value.run_id,
        reason=value.reason,
        context=getattr(value, "context", None),
    )


def signal_workflow_provider_run_request_from_proto(
    value: Any,
) -> SignalWorkflowProviderRunRequest:
    return SignalWorkflowProviderRunRequest(
        run_id=value.run_id,
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        context=getattr(value, "context", None),
    )


def signal_or_start_workflow_provider_run_request_from_proto(
    value: Any,
) -> SignalOrStartWorkflowProviderRunRequest:
    return SignalOrStartWorkflowProviderRunRequest(
        workflow_key=value.workflow_key,
        idempotency_key=value.idempotency_key,
        signal=workflow_signal_input_from_signal(value.signal)
        if has_field(value, "signal")
        else None,
        definition_id=value.definition_id,
        input=cast(WorkflowJsonObject, struct_to_dict(value.input))
        if has_field(value, "input")
        else None,
        expected_definition_generation=value.expected_definition_generation,
        context=getattr(value, "context", None),
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
        out.run.CopyFrom(workflow_run(response.run))
    if response.signal is not None:
        out.signal.CopyFrom(workflow_signal(response.signal))
    return out


def apply_workflow_provider_definition_request_from_proto(
    value: Any,
) -> ApplyWorkflowProviderDefinitionRequest:
    return ApplyWorkflowProviderDefinitionRequest(
        spec=workflow_definition_spec_input_from_spec(value.spec)
        if has_field(value, "spec")
        else None,
        idempotency_key=value.idempotency_key,
        context=getattr(value, "context", None),
    )


def get_workflow_provider_definition_request_from_proto(
    value: Any,
) -> GetWorkflowProviderDefinitionRequest:
    return GetWorkflowProviderDefinitionRequest(
        definition_id=value.definition_id,
        context=getattr(value, "context", None),
    )


def list_workflow_provider_definitions_request_from_proto(
    value: Any,
) -> ListWorkflowProviderDefinitionsRequest:
    return ListWorkflowProviderDefinitionsRequest(
        context=getattr(value, "context", None),
    )


def list_workflow_provider_definitions_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.ListWorkflowProviderDefinitionsResponse):
        return _copy(value)
    response = _coerce(
        value,
        ListWorkflowProviderDefinitionsResponse,
        "ListWorkflowProviderDefinitionsResponse",
    )
    return pb.ListWorkflowProviderDefinitionsResponse(
        definitions=[workflow_definition(item) for item in response.definitions]
    )


def set_workflow_provider_definition_paused_request_from_proto(
    value: Any,
) -> SetWorkflowProviderDefinitionPausedRequest:
    return SetWorkflowProviderDefinitionPausedRequest(
        definition_id=value.definition_id,
        paused=value.paused,
        context=getattr(value, "context", None),
    )


def set_workflow_provider_activation_paused_request_from_proto(
    value: Any,
) -> SetWorkflowProviderActivationPausedRequest:
    return SetWorkflowProviderActivationPausedRequest(
        definition_id=value.definition_id,
        activation_id=value.activation_id,
        paused=value.paused,
        context=getattr(value, "context", None),
    )


def delete_workflow_provider_definition_request_from_proto(
    value: Any,
) -> DeleteWorkflowProviderDefinitionRequest:
    return DeleteWorkflowProviderDefinitionRequest(
        definition_id=value.definition_id,
        context=getattr(value, "context", None),
    )


def workflow_run_event(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunEvent):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunEvent(
        id=data.get("id", ""),
        run_id=data.get("run_id", ""),
        step_id=data.get("step_id", ""),
        type=data.get("type", ""),
        data=_optional_struct(data.get("data")),
        created_at=_optional_timestamp(data.get("created_at")),
    )


def workflow_run_event_input_from_event(value: Any | None) -> WorkflowRunEvent | None:
    if value is None:
        return None
    return WorkflowRunEvent(
        id=value.id,
        run_id=value.run_id,
        step_id=value.step_id,
        type=value.type,
        data=cast(WorkflowJsonObject, struct_to_dict(value.data))
        if has_field(value, "data")
        else None,
        created_at=_timestamp_to_datetime(value, "created_at"),
    )


def get_workflow_provider_run_events_request_from_proto(
    value: Any,
) -> GetWorkflowProviderRunEventsRequest:
    return GetWorkflowProviderRunEventsRequest(
        run_id=value.run_id,
        context=getattr(value, "context", None),
    )


def get_workflow_provider_run_events_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.GetWorkflowProviderRunEventsResponse):
        return _copy(value)
    response = _coerce(
        value,
        GetWorkflowProviderRunEventsResponse,
        "GetWorkflowProviderRunEventsResponse",
    )
    return pb.GetWorkflowProviderRunEventsResponse(
        events=[workflow_run_event(item) for item in response.events]
    )


def get_workflow_provider_run_output_request_from_proto(
    value: Any,
) -> GetWorkflowProviderRunOutputRequest:
    return GetWorkflowProviderRunOutputRequest(
        run_id=value.run_id,
        context=getattr(value, "context", None),
    )


def get_workflow_provider_run_output_response_to_proto(value: Any) -> Any:
    if isinstance(value, pb.GetWorkflowProviderRunOutputResponse):
        return _copy(value)
    if isinstance(value, GetWorkflowProviderRunOutputResponse):
        output = value.output
    else:
        output = value
    response = pb.GetWorkflowProviderRunOutputResponse()
    if output is not None:
        response.output.CopyFrom(_value(output))
    return response


def deliver_workflow_provider_event_request_from_proto(
    value: Any,
) -> DeliverWorkflowProviderEventRequest:
    return DeliverWorkflowProviderEventRequest(
        app_name=value.app_name,
        event=workflow_event_input_from_event(value.event)
        if has_field(value, "event")
        else None,
        context=getattr(value, "context", None),
    )


def workflow_run_status_name(status: int) -> str:
    """Return the enum name for a workflow run status value."""

    if not status:
        return ""
    try:
        return pb.WorkflowRunStatus.Name(status)
    except ValueError:
        return str(status)


WorkflowStartRunInput: TypeAlias = WorkflowStartRun | WorkflowRequestMapping | None
WorkflowSignalRunInput: TypeAlias = WorkflowSignalRun | WorkflowRequestMapping | None
WorkflowSignalOrStartRunInput: TypeAlias = (
    WorkflowSignalOrStartRun | WorkflowRequestMapping | None
)
WorkflowApplyDefinitionInput: TypeAlias = (
    WorkflowApplyDefinition | WorkflowRequestMapping | None
)
WorkflowGetDefinitionInput: TypeAlias = (
    WorkflowGetDefinition | WorkflowRequestMapping | None
)
WorkflowDeleteDefinitionInput: TypeAlias = (
    WorkflowDeleteDefinition | WorkflowRequestMapping | None
)
WorkflowSetDefinitionPausedInput: TypeAlias = (
    WorkflowSetDefinitionPaused | WorkflowRequestMapping | None
)
WorkflowSetActivationPausedInput: TypeAlias = (
    WorkflowSetActivationPaused | WorkflowRequestMapping | None
)
WorkflowGetRunInput: TypeAlias = WorkflowGetRun | WorkflowRequestMapping | None
WorkflowGetRunEventsInput: TypeAlias = (
    WorkflowGetRunEvents | WorkflowRequestMapping | None
)
WorkflowGetRunOutputInput: TypeAlias = (
    WorkflowGetRunOutput | WorkflowRequestMapping | None
)
WorkflowDeliverEventInput: TypeAlias = (
    WorkflowDeliverEvent | WorkflowRequestMapping | None
)


class WorkflowProtocol(Protocol):
    """Fakeable contract for workflow calls."""

    def close(self) -> None:
        """Close the client."""

    def start_run(
        self, request: WorkflowStartRunInput = None, **kwargs: object
    ) -> WorkflowRun:
        """Start a workflow run."""

    def signal_run(
        self, request: WorkflowSignalRunInput = None, **kwargs: object
    ) -> WorkflowRunSignal:
        """Signal an existing workflow run."""

    def signal_or_start_run(
        self, request: WorkflowSignalOrStartRunInput = None, **kwargs: object
    ) -> WorkflowRunSignal:
        """Signal a run, or start it when no matching run exists."""

    def apply_definition(
        self, request: WorkflowApplyDefinitionInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Apply a reusable workflow definition."""

    def get_definition(
        self, request: WorkflowGetDefinitionInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Fetch one workflow definition."""

    def list_definitions(
        self,
    ) -> WorkflowDefinition:
        """List workflow definitions."""

    def delete_definition(
        self, request: WorkflowDeleteDefinitionInput = None, **kwargs: object
    ) -> None:
        """Delete a workflow definition."""

    def set_definition_paused(
        self, request: WorkflowSetDefinitionPausedInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Pause or resume a workflow definition."""

    def set_activation_paused(
        self, request: WorkflowSetActivationPausedInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Pause or resume a workflow activation."""

    def get_run_events(
        self, request: WorkflowGetRunEventsInput = None, **kwargs: object
    ) -> GetWorkflowProviderRunEventsResponse:
        """Fetch run events."""

    def get_run_output(
        self, request: WorkflowGetRunOutputInput = None, **kwargs: object
    ) -> GetWorkflowProviderRunOutputResponse:
        """Fetch run output."""

    def deliver_event(
        self, request: WorkflowDeliverEventInput = None, **kwargs: object
    ) -> WorkflowEvent | None:
        """Deliver an event into workflow activation matching."""


class Workflow:
    """Client for applying definitions, starting runs, signaling, and delivering events.

    This capability is for provider code that receives a Gestalt request. The
    request idempotency key is used for create requests that do not already
    include one.
    """

    def __init__(
        self,
        request: Request,
        *,
        idempotency_key: str = "",
        timeout: float | None = None,
    ) -> None:
        target = os.environ.get(ENV_HOST_SERVICE_SOCKET, "")
        if not target:
            raise RuntimeError(f"workflow: {ENV_HOST_SERVICE_SOCKET} is not set")
        relay_token = os.environ.get(ENV_HOST_SERVICE_TOKEN, "")

        self._channel = host_service_channel("workflow", target, token=relay_token)
        self._stub = pb_grpc.WorkflowStub(self._channel)
        self._timeout = timeout
        self._context = request.context
        if not idempotency_key.strip():
            idempotency_key = request.idempotency_key
        self._idempotency_key = idempotency_key.strip()

    def close(self) -> None:
        """Close the underlying gRPC channel."""

        self._channel.close()

    def start_run(
        self, request: WorkflowStartRunInput = None, **kwargs: object
    ) -> WorkflowRun:
        """Start a workflow run."""

        request = _workflow_start_run_request(request, **kwargs)
        self._attach_context(request)
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_run_from_proto(_grpc_call(self._stub.StartRun, request, timeout=self._timeout))

    def signal_run(
        self, request: WorkflowSignalRunInput = None, **kwargs: object
    ) -> WorkflowRunSignal:
        """Signal an existing workflow run."""

        request = _workflow_signal_run_request(request, **kwargs)
        self._attach_context(request)
        return workflow_run_signal_from_proto(_grpc_call(self._stub.SignalRun, request, timeout=self._timeout))

    def signal_or_start_run(
        self, request: WorkflowSignalOrStartRunInput = None, **kwargs: object
    ) -> WorkflowRunSignal:
        """Signal a run, or start it when no matching run exists."""

        request = _workflow_signal_or_start_run_request(request, **kwargs)
        self._attach_context(request)
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_run_signal_from_proto(
            _grpc_call(self._stub.SignalOrStartRun, request, timeout=self._timeout)
        )

    def apply_definition(
        self, request: WorkflowApplyDefinitionInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Apply a reusable workflow definition."""

        request = _workflow_apply_definition_request(request, **kwargs)
        self._attach_context(request)
        if not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return workflow_definition_from_proto(
            _grpc_call(self._stub.ApplyDefinition, request, timeout=self._timeout)
        )

    def get_definition(
        self, request: WorkflowGetDefinitionInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Fetch one workflow definition."""

        request = _workflow_get_definition_request(request, **kwargs)
        self._attach_context(request)
        return workflow_definition_from_proto(
            _grpc_call(self._stub.GetDefinition, request, timeout=self._timeout)
        )

    def list_definitions(self) -> list[WorkflowDefinition]:
        """List workflow definitions."""

        request = pb.ListWorkflowProviderDefinitionsRequest()
        self._attach_context(request)
        response = _grpc_call(self._stub.ListDefinitions, request, timeout=self._timeout)
        return [workflow_definition_from_proto(item) for item in response.definitions]

    def set_definition_paused(
        self, request: WorkflowSetDefinitionPausedInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Pause or resume a workflow definition."""

        request = _workflow_set_definition_paused_request(request, **kwargs)
        self._attach_context(request)
        return workflow_definition_from_proto(
            _grpc_call(self._stub.SetDefinitionPaused, request, timeout=self._timeout)
        )

    def set_activation_paused(
        self, request: WorkflowSetActivationPausedInput = None, **kwargs: object
    ) -> WorkflowDefinition:
        """Pause or resume a workflow activation."""

        request = _workflow_set_activation_paused_request(request, **kwargs)
        self._attach_context(request)
        return workflow_definition_from_proto(
            _grpc_call(self._stub.SetActivationPaused, request, timeout=self._timeout)
        )

    def delete_definition(
        self, request: WorkflowDeleteDefinitionInput = None, **kwargs: object
    ) -> None:
        """Delete a workflow definition."""

        request = _workflow_delete_definition_request(request, **kwargs)
        self._attach_context(request)
        _grpc_call(self._stub.DeleteDefinition, request, timeout=self._timeout)
        return None

    def get_run_events(
        self, request: WorkflowGetRunEventsInput = None, **kwargs: object
    ) -> GetWorkflowProviderRunEventsResponse:
        """Fetch run events."""

        request = _workflow_get_run_events_request(request, **kwargs)
        self._attach_context(request)
        response = _grpc_call(self._stub.GetRunEvents, request, timeout=self._timeout)
        return GetWorkflowProviderRunEventsResponse(
            events=[
                cast(WorkflowRunEvent, workflow_run_event_input_from_event(item))
                for item in response.events
            ]
        )

    def get_run_output(
        self, request: WorkflowGetRunOutputInput = None, **kwargs: object
    ) -> GetWorkflowProviderRunOutputResponse:
        """Fetch run output."""

        request = _workflow_get_run_output_request(request, **kwargs)
        self._attach_context(request)
        response = _grpc_call(self._stub.GetRunOutput, request, timeout=self._timeout)
        return GetWorkflowProviderRunOutputResponse(
            output=cast(WorkflowJsonValue, value_to_json(response.output))
            if has_field(response, "output")
            else None
        )

    def deliver_event(
        self, request: WorkflowDeliverEventInput = None, **kwargs: object
    ) -> WorkflowEvent | None:
        """Deliver an event into workflow activation matching."""

        request = _workflow_deliver_event_request(request, **kwargs)
        self._attach_context(request)
        return workflow_event_input_from_event(
            _grpc_call(self._stub.DeliverEvent, request, timeout=self._timeout)
        )

    def _attach_context(self, request: Any) -> None:
        if self._context is not None and hasattr(request, "context"):
            request.context.CopyFrom(self._context)

    def __enter__(self) -> Workflow:
        """Return the client for ``with`` statements."""

        return self

    def __exit__(self, *args: Any) -> None:
        """Close the client at the end of a context manager block."""

        self.close()


def _grpc_call(method: Any, request: Any, timeout: float | None = None) -> Any:
    try:
        return method(request, timeout=timeout)
    except grpc.RpcError:
        raise


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowExecutionRequest:
    __hash__ = None
    provider_name: str = ""
    run_id: str = ""
    target: BoundWorkflowTarget | None = None
    trigger: WorkflowRunTrigger | None = None
    input: WorkflowJsonObject | None = None
    metadata: WorkflowJsonObject | None = None
    created_by_subject_id: str = ""
    signals: Sequence[WorkflowSignal] = _dataclasses.field(default_factory=list)
    steps: Mapping[str, Any] | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunContextTrigger:
    __hash__ = None
    kind: str = ""
    activation_id: str = ""
    scheduled_for: str = ""
    event: WorkflowJsonObject | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunContextSignal:
    __hash__ = None
    id: str = ""
    name: str = ""
    payload: WorkflowJsonObject = _dataclasses.field(default_factory=dict)
    metadata: WorkflowJsonObject = _dataclasses.field(default_factory=dict)
    created_by_subject_id: str = ""
    created_at: str = ""
    idempotency_key: str = ""
    sequence: int | None = None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowRunContext:
    __hash__ = None
    provider: str = ""
    run_id: str = ""
    target: WorkflowJsonObject | None = None
    trigger: WorkflowRunContextTrigger = _dataclasses.field(
        default_factory=WorkflowRunContextTrigger
    )
    input: WorkflowJsonObject = _dataclasses.field(default_factory=dict)
    metadata: WorkflowJsonObject = _dataclasses.field(default_factory=dict)
    signals: Sequence[WorkflowRunContextSignal] = _dataclasses.field(
        default_factory=list
    )
    created_by_subject_id: str = ""

    @property
    def latest_signal(self) -> WorkflowRunContextSignal | None:
        return self.signals[-1] if self.signals else None


@_dataclasses.dataclass(frozen=True, slots=True)
class WorkflowEvalContext:
    __hash__ = None
    request: WorkflowExecutionRequest
    outputs: Mapping[str, Any] | None = None
    inputs: Mapping[str, Any] | None = None
    allow_inputs: bool = False


class WorkflowValueError(ValueError):
    pass


def evaluate_workflow_step_inputs(
    ctx: WorkflowEvalContext, values: Mapping[str, WorkflowValuePayload] | None
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


def evaluate_workflow_value(
    ctx: WorkflowEvalContext, value: WorkflowValuePayload
) -> tuple[Any, bool]:
    if not isinstance(value, WorkflowValue):
        if isinstance(value, Mapping):
            out: dict[str, Any] = {}
            for key, child in value.items():
                if not isinstance(key, str):
                    raise WorkflowValueError(
                        f"workflow value object keys must be strings, got {type(key).__name__}"
                    )
                resolved, ok = evaluate_workflow_value(
                    ctx, cast(WorkflowValuePayload, child)
                )
                if not ok:
                    return None, False
                out[key] = resolved
            return out, True
        if isinstance(value, Sequence) and not isinstance(
            value, str | bytes | bytearray | memoryview
        ):
            out = []
            for child in value:
                resolved, ok = evaluate_workflow_value(
                    ctx, cast(WorkflowValuePayload, child)
                )
                if not ok:
                    return None, False
                out.append(resolved)
            return out, True
        return value, True
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
        template = (
            value.template.template
            if isinstance(value.template, WorkflowText)
            else value.template
        )
        return render_workflow_template(ctx, template), True
    if value.input.strip():
        return map_path_value(ctx.request.input, value.input)
    if value.signal.strip():
        signal = latest_workflow_signal(ctx.request.signals)
        if signal is None:
            return None, False
        return path_value(signal.payload, value.signal)
    if value.step_output is not None:
        step_id = value.step_output.step_id.strip()
        outputs = ctx.outputs or {}
        if step_id not in outputs:
            raise WorkflowValueError(
                f'workflow step output references missing step "{step_id}"'
            )
        return path_value(outputs[step_id], value.step_output.path)
    if value.step_input is not None:
        step_id = value.step_input.step_id.strip()
        steps = getattr(ctx.request, "steps", None) or {}
        step = steps.get(step_id, {})
        return path_value(step.get("inputs", {}), value.step_input.path)
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


def workflow_run_context(req: WorkflowExecutionRequest) -> dict[str, Any]:
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
    if req.created_by_subject_id.strip():
        out["createdBySubjectId"] = req.created_by_subject_id.strip()
    return out


def parse_workflow_run_context(
    workflow: Mapping[str, Any] | None = None,
) -> WorkflowRunContext:
    data = workflow if isinstance(workflow, Mapping) else {}
    return WorkflowRunContext(
        provider=_workflow_context_str(data.get("provider")),
        run_id=_workflow_context_str(data.get("runId")),
        target=_workflow_context_optional_object(data.get("target")),
        trigger=_workflow_run_trigger(data.get("trigger")),
        input=_workflow_context_object(data.get("input")),
        metadata=_workflow_context_object(data.get("metadata")),
        signals=_workflow_run_signals(data.get("signals")),
        created_by_subject_id=_workflow_context_str(data.get("createdBySubjectId")),
    )


def _workflow_run_trigger(value: Any) -> WorkflowRunContextTrigger:
    data = value if isinstance(value, Mapping) else {}
    return WorkflowRunContextTrigger(
        kind=_workflow_context_str(data.get("kind")),
        activation_id=_workflow_context_str(data.get("activationId")),
        scheduled_for=_workflow_context_str(data.get("scheduledFor")),
        event=_workflow_context_optional_object(data.get("event")),
    )


def _workflow_run_signals(value: Any) -> list[WorkflowRunContextSignal]:
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes, bytearray)):
        return []
    signals: list[WorkflowRunContextSignal] = []
    for item in value:
        signal = _workflow_run_signal(item)
        if signal is not None:
            signals.append(signal)
    return signals


def _workflow_run_signal(value: Any) -> WorkflowRunContextSignal | None:
    if not isinstance(value, Mapping):
        return None
    return WorkflowRunContextSignal(
        id=_workflow_context_str(value.get("id")),
        name=_workflow_context_str(value.get("name")),
        payload=_workflow_context_object(value.get("payload")),
        metadata=_workflow_context_object(value.get("metadata")),
        created_by_subject_id=_workflow_context_str(value.get("createdBySubjectId")),
        created_at=_workflow_context_str(value.get("createdAt")),
        idempotency_key=_workflow_context_str(value.get("idempotencyKey")),
        sequence=_workflow_context_int(value.get("sequence")),
    )


def _workflow_context_str(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def _workflow_context_int(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, float) and math.isfinite(value) and value.is_integer():
        return int(value)
    return value if isinstance(value, int) else None


def _workflow_context_object(value: Any) -> WorkflowJsonObject:
    if not isinstance(value, Mapping):
        return {}
    return cast(WorkflowJsonObject, dict(value))


def _workflow_context_optional_object(value: Any) -> WorkflowJsonObject | None:
    if not isinstance(value, Mapping):
        return None
    return cast(WorkflowJsonObject, dict(value))


def _workflow_target_context(
    target: BoundWorkflowTarget | Any | None,
) -> dict[str, Any]:
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


def _workflow_trigger_context(
    trigger: WorkflowRunTrigger | Any | None,
) -> dict[str, Any]:
    if trigger is None:
        return {}
    if getattr(trigger, "schedule", None) is not None:
        schedule = trigger.schedule
        out: dict[str, Any] = {
            "kind": "schedule",
            "activationId": getattr(schedule, "activation_id", ""),
        }
        scheduled_for = _format_workflow_time(getattr(schedule, "scheduled_for", None))
        if scheduled_for:
            out["scheduledFor"] = scheduled_for
        return out
    if getattr(trigger, "event", None) is not None:
        event_trigger = trigger.event
        out = {
            "kind": "event",
            "activationId": getattr(event_trigger, "activation_id", ""),
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


def workflow_signals_context(
    signals: Sequence[WorkflowSignal] | None,
) -> list[dict[str, Any]]:
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
        if signal.created_by_subject_id.strip():
            item["createdBySubjectId"] = signal.created_by_subject_id.strip()
        created_at = _format_workflow_time(signal.created_at)
        if created_at:
            item["createdAt"] = created_at
        if signal.idempotency_key.strip():
            item["idempotencyKey"] = signal.idempotency_key.strip()
        if signal.sequence:
            item["sequence"] = signal.sequence
        out.append(item)
    return out


def latest_workflow_signal(
    signals: Sequence[WorkflowSignal] | None,
) -> WorkflowSignal | None:
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
        elif (
            isinstance(current, Sequence)
            and not isinstance(current, (str, bytes))
            and isinstance(segment, int)
        ):
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
    if expr.startswith("input."):
        return map_path_value(ctx.request.input, expr.removeprefix("input."))
    if expr.startswith("signal."):
        signal = latest_workflow_signal(ctx.request.signals)
        if signal is None:
            return None, False
        return path_value(signal.payload, expr.removeprefix("signal."))
    if expr.startswith("steps."):
        return path_value(
            getattr(ctx.request, "steps", {}) or {}, expr.removeprefix("steps.")
        )
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
    return value.literal is not _UNSET
