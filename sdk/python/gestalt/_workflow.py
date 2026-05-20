from __future__ import annotations

import dataclasses as _dataclasses
import datetime as _dt
import os
from collections.abc import Mapping, Sequence
from typing import Any

import grpc
from google.protobuf import message as _message

from ._agent import agent_tool_ref_from_proto, agent_tool_ref_to_proto
from ._gen.v1 import plugin_pb2 as _plugin_pb
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

WORKFLOW_ACTIVATION_MODE_UNSPECIFIED = pb.WORKFLOW_ACTIVATION_MODE_UNSPECIFIED
WORKFLOW_ACTIVATION_MODE_START = pb.WORKFLOW_ACTIVATION_MODE_START
WORKFLOW_ACTIVATION_MODE_SIGNAL = pb.WORKFLOW_ACTIVATION_MODE_SIGNAL
WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START = (
    pb.WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START
)

WORKFLOW_ACTION_KIND_UNSPECIFIED = pb.WORKFLOW_ACTION_KIND_UNSPECIFIED
WORKFLOW_ACTION_KIND_PLUGIN = pb.WORKFLOW_ACTION_KIND_PLUGIN
WORKFLOW_ACTION_KIND_AGENT_TURN = pb.WORKFLOW_ACTION_KIND_AGENT_TURN
WORKFLOW_ACTION_KIND_DELIVERY = pb.WORKFLOW_ACTION_KIND_DELIVERY

WORKFLOW_DEFINITION_STATUS_UNSPECIFIED = pb.WORKFLOW_DEFINITION_STATUS_UNSPECIFIED
WORKFLOW_DEFINITION_STATUS_PENDING = pb.WORKFLOW_DEFINITION_STATUS_PENDING
WORKFLOW_DEFINITION_STATUS_ACTIVE = pb.WORKFLOW_DEFINITION_STATUS_ACTIVE
WORKFLOW_DEFINITION_STATUS_PAUSED = pb.WORKFLOW_DEFINITION_STATUS_PAUSED
WORKFLOW_DEFINITION_STATUS_DELETED = pb.WORKFLOW_DEFINITION_STATUS_DELETED
WORKFLOW_DEFINITION_STATUS_FAILED = pb.WORKFLOW_DEFINITION_STATUS_FAILED

WORKFLOW_RUN_STATUS_UNSPECIFIED = pb.WORKFLOW_RUN_STATUS_UNSPECIFIED
WORKFLOW_RUN_STATUS_PENDING = pb.WORKFLOW_RUN_STATUS_PENDING
WORKFLOW_RUN_STATUS_RUNNING = pb.WORKFLOW_RUN_STATUS_RUNNING
WORKFLOW_RUN_STATUS_SUCCEEDED = pb.WORKFLOW_RUN_STATUS_SUCCEEDED
WORKFLOW_RUN_STATUS_FAILED = pb.WORKFLOW_RUN_STATUS_FAILED
WORKFLOW_RUN_STATUS_CANCELED = pb.WORKFLOW_RUN_STATUS_CANCELED

WORKFLOW_STEP_STATUS_UNSPECIFIED = pb.WORKFLOW_STEP_STATUS_UNSPECIFIED
WORKFLOW_STEP_STATUS_PENDING = pb.WORKFLOW_STEP_STATUS_PENDING
WORKFLOW_STEP_STATUS_RUNNING = pb.WORKFLOW_STEP_STATUS_RUNNING
WORKFLOW_STEP_STATUS_SUCCEEDED = pb.WORKFLOW_STEP_STATUS_SUCCEEDED
WORKFLOW_STEP_STATUS_FAILED = pb.WORKFLOW_STEP_STATUS_FAILED
WORKFLOW_STEP_STATUS_SKIPPED = pb.WORKFLOW_STEP_STATUS_SKIPPED
WORKFLOW_STEP_STATUS_CANCELED = pb.WORKFLOW_STEP_STATUS_CANCELED

WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED = pb.WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED
WORKFLOW_RUN_EVENT_TYPE_RUN_STARTED = pb.WORKFLOW_RUN_EVENT_TYPE_RUN_STARTED
WORKFLOW_RUN_EVENT_TYPE_RUN_COMPLETED = pb.WORKFLOW_RUN_EVENT_TYPE_RUN_COMPLETED
WORKFLOW_RUN_EVENT_TYPE_RUN_FAILED = pb.WORKFLOW_RUN_EVENT_TYPE_RUN_FAILED
WORKFLOW_RUN_EVENT_TYPE_RUN_CANCELED = pb.WORKFLOW_RUN_EVENT_TYPE_RUN_CANCELED
WORKFLOW_RUN_EVENT_TYPE_SIGNAL_RECEIVED = pb.WORKFLOW_RUN_EVENT_TYPE_SIGNAL_RECEIVED
WORKFLOW_RUN_EVENT_TYPE_STEP_STARTED = pb.WORKFLOW_RUN_EVENT_TYPE_STEP_STARTED
WORKFLOW_RUN_EVENT_TYPE_STEP_SUCCEEDED = pb.WORKFLOW_RUN_EVENT_TYPE_STEP_SUCCEEDED
WORKFLOW_RUN_EVENT_TYPE_STEP_FAILED = pb.WORKFLOW_RUN_EVENT_TYPE_STEP_FAILED
WORKFLOW_RUN_EVENT_TYPE_STEP_SKIPPED = pb.WORKFLOW_RUN_EVENT_TYPE_STEP_SKIPPED
WORKFLOW_RUN_EVENT_TYPE_ACTION_INVOKED = pb.WORKFLOW_RUN_EVENT_TYPE_ACTION_INVOKED
WORKFLOW_RUN_EVENT_TYPE_ACTION_COMPLETED = pb.WORKFLOW_RUN_EVENT_TYPE_ACTION_COMPLETED
WORKFLOW_RUN_EVENT_TYPE_ACTION_FAILED = pb.WORKFLOW_RUN_EVENT_TYPE_ACTION_FAILED

_MISSING = object()


@_dataclasses.dataclass(slots=True)
class WorkflowText:
    template: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowPathSource:
    path: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStepOutputSource:
    step_id: str = ""
    path: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowValue:
    literal: Any = _MISSING
    object: Any | None = None
    array: Any | None = None
    template: Any | None = None
    run_input: Any | None = None
    signal_payload: Any | None = None
    step_output: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowObject:
    fields: Mapping[str, Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowArray:
    values: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepPluginCall:
    name: str = ""
    operation: str = ""
    input: Any | None = None
    connection: str = ""
    instance: str = ""
    credential_mode: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStepDelivery:
    plugin: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowAgentMessage:
    role: str = ""
    text: Any | None = None
    metadata: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepAgentTurn:
    provider: str = ""
    model: str = ""
    session_key: Any | None = None
    prompt: Any | None = None
    messages: Sequence[Any] | None = None
    tools: Sequence[Any] | None = None
    response_schema: Any | None = None
    model_options: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStepWhen:
    value: Any | None = None
    equals: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowStep:
    id: str = ""
    inputs: Mapping[str, Any] | None = None
    when: Any | None = None
    timeout_seconds: int = 0
    output_delivery: Any | None = None
    metadata: Any | None = None
    plugin: Any | None = None
    agent: Any | None = None


@_dataclasses.dataclass(slots=True)
class BoundWorkflowTarget:
    steps: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActor:
    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowRunAsSubject:
    subject_id: str = ""
    subject_kind: str = ""
    display_name: str = ""
    auth_source: str = ""
    credential_subject_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowEvent:
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
    type: str = ""
    source: str = ""
    subject: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManualActivation:
    pass


@_dataclasses.dataclass(slots=True)
class WorkflowScheduleActivation:
    cron: str = ""
    timezone: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowEventActivation:
    match: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActivation:
    id: str = ""
    paused: bool = False
    mode: int = WORKFLOW_ACTIVATION_MODE_UNSPECIFIED
    input: Any | None = None
    run_key: Any | None = None
    idempotency_key: Any | None = None
    manual: Any | None = None
    schedule: Any | None = None
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowAccessPermission:
    plugin: str = ""
    operations: Sequence[str] | None = None
    actions: Sequence[str] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowDefinitionSpec:
    id: str = ""
    generation: int = 0
    target: Any | None = None
    activations: Sequence[Any] | None = None
    paused: bool = False
    run_as: Any | None = None
    permissions: Sequence[Any] | None = None
    labels: Mapping[str, str] | None = None
    workflow_semantics_version: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowActionDescriptor:
    action_id: str = ""
    step_id: str = ""
    kind: int = WORKFLOW_ACTION_KIND_UNSPECIFIED
    plugin: Any | None = None
    agent: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActionTable:
    actions: Sequence[Any] | None = None
    digest: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowDefinitionBinding:
    id: str = ""
    execution_ref: str = ""
    execution_ref_generation: int = 0
    definition_id: str = ""
    definition_generation: int = 0
    spec_digest: str = ""
    target_digest: str = ""
    action_table_digest: str = ""
    permissions_digest: str = ""
    workflow_semantics_version: str = ""
    request_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowRunError:
    code: str = ""
    message: str = ""
    step_id: str = ""
    action_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowDefinition:
    spec: Any | None = None
    status: int = WORKFLOW_DEFINITION_STATUS_UNSPECIFIED
    created_at: _dt.datetime | Any | None = None
    updated_at: _dt.datetime | Any | None = None
    applied_generation: int = 0
    spec_digest: str = ""
    target_digest: str = ""
    action_table_digest: str = ""
    provider_plan_id: str = ""
    provider_plan_digest: str = ""
    binding: Any | None = None
    error: Any | None = None


@_dataclasses.dataclass(slots=True)
class ApplyWorkflowDefinitionRequest:
    spec: Any | None = None
    binding: Any | None = None
    execution_ref: Any | None = None
    request_id: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowDefinitionRequest:
    definition_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowDefinitionsRequest:
    page_size: int = 0
    page_token: str = ""
    labels: Mapping[str, str] | None = None


@_dataclasses.dataclass(slots=True)
class ListWorkflowDefinitionsResponse:
    definitions: Sequence[Any] | None = None
    next_page_token: str = ""


@_dataclasses.dataclass(slots=True)
class DeleteWorkflowDefinitionRequest:
    definition_id: str = ""
    generation: int = 0
    request_id: str = ""


@_dataclasses.dataclass(slots=True)
class SetWorkflowDefinitionPausedRequest:
    definition_id: str = ""
    paused: bool = False
    request_id: str = ""


@_dataclasses.dataclass(slots=True)
class SetWorkflowActivationPausedRequest:
    definition_id: str = ""
    activation_id: str = ""
    paused: bool = False
    request_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManualTrigger:
    pass


@_dataclasses.dataclass(slots=True)
class WorkflowScheduleTrigger:
    activation_id: str = ""
    scheduled_for: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowEventTrigger:
    activation_id: str = ""
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunTrigger:
    definition_id: str = ""
    definition_generation: int = 0
    activation_id: str = ""
    manual: Any | None = None
    schedule: Any | None = None
    event: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowSignal:
    id: str = ""
    name: str = ""
    payload: Any | None = None
    metadata: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None
    idempotency_key: str = ""
    sequence: int = 0


@_dataclasses.dataclass(slots=True)
class WorkflowOutputSummary:
    envelope_version: str = ""
    kind: str = ""
    size_bytes: int = 0
    sha256: str = ""
    truncated: bool = False
    redacted: bool = False
    media_type: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowStepState:
    step_id: str = ""
    step_index: int = 0
    status: int = WORKFLOW_STEP_STATUS_UNSPECIFIED
    skipped_reason: str = ""
    attempt_number: int = 0
    output_summary: Any | None = None
    output_ref: str = ""
    error: Any | None = None
    updated_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRun:
    id: str = ""
    definition_id: str = ""
    definition_generation: int = 0
    workflow_key: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED
    trigger: Any | None = None
    input: Any | None = None
    created_by: Any | None = None
    created_at: _dt.datetime | Any | None = None
    started_at: _dt.datetime | Any | None = None
    completed_at: _dt.datetime | Any | None = None
    status_message: str = ""
    execution_ref: str = ""
    execution_ref_generation: int = 0
    target_digest: str = ""
    spec_digest: str = ""
    action_table_digest: str = ""
    steps: Sequence[Any] | None = None
    error: Any | None = None


@_dataclasses.dataclass(slots=True)
class StartWorkflowRunRequest:
    definition_id: str = ""
    definition_generation: int = 0
    activation_id: str = ""
    workflow_key: str = ""
    input: Any | None = None
    idempotency_key: str = ""
    created_by: Any | None = None


@_dataclasses.dataclass(slots=True)
class SignalWorkflowRunRequest:
    run_id: str = ""
    signal: Any | None = None


@_dataclasses.dataclass(slots=True)
class SignalOrStartWorkflowRunRequest:
    definition_id: str = ""
    definition_generation: int = 0
    activation_id: str = ""
    workflow_key: str = ""
    input: Any | None = None
    idempotency_key: str = ""
    signal: Any | None = None
    created_by: Any | None = None


@_dataclasses.dataclass(slots=True)
class CancelWorkflowRunRequest:
    run_id: str = ""
    reason: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowRunRequest:
    run_id: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowRunsRequest:
    definition_id: str = ""
    page_size: int = 0
    page_token: str = ""
    status: int = WORKFLOW_RUN_STATUS_UNSPECIFIED


@_dataclasses.dataclass(slots=True)
class ListWorkflowRunsResponse:
    runs: Sequence[Any] | None = None
    next_page_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowRunSignal:
    run: Any | None = None
    signal: Any | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class DeliverWorkflowEventRequest:
    delivery_id: str = ""
    event: Any | None = None
    published_by: Any | None = None
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowEventDeliveryResult:
    definition_id: str = ""
    activation_id: str = ""
    run: Any | None = None
    signal: Any | None = None
    started_run: bool = False


@_dataclasses.dataclass(slots=True)
class DeliverWorkflowEventResponse:
    results: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowRunEvent:
    id: str = ""
    run_id: str = ""
    sequence: int = 0
    type: int = WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED
    step_id: str = ""
    action_id: str = ""
    attempt_number: int = 0
    message: str = ""
    output_summary: Any | None = None
    output_ref: str = ""
    error: Any | None = None
    observed_at: _dt.datetime | Any | None = None


@_dataclasses.dataclass(slots=True)
class GetWorkflowRunEventsRequest:
    run_id: str = ""
    page_size: int = 0
    page_token: str = ""


@_dataclasses.dataclass(slots=True)
class ListWorkflowRunEventsResponse:
    events: Sequence[Any] | None = None
    next_page_token: str = ""


@_dataclasses.dataclass(slots=True)
class GetWorkflowRunOutputRequest:
    run_id: str = ""
    output_ref: str = ""
    step_id: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowRunOutput:
    output_ref: str = ""
    summary: Any | None = None
    body: Any = _MISSING


@_dataclasses.dataclass(slots=True)
class WorkflowHostActionSelector:
    execution_ref: str = ""
    execution_ref_generation: int = 0
    run_id: str = ""
    definition_id: str = ""
    definition_generation: int = 0
    step_id: str = ""
    action_id: str = ""
    attempt_number: int = 0
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowPluginActionPayload:
    input: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowAgentTurnPayload:
    prompt: Any | None = None
    messages: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class InvokeWorkflowActionRequest:
    selector: Any | None = None
    metadata: Any | None = None
    trigger: Any | None = None
    signals: Sequence[Any] | None = None
    plugin: Any | None = None
    agent_turn: Any | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowActionResult:
    action_event_id: str = ""
    status: int = 0
    body: str = ""
    output_summary: Any | None = None
    output_ref: str = ""
    error: Any | None = None


@_dataclasses.dataclass(slots=True)
class ManagedWorkflowDefinition:
    provider_name: str = ""
    definition: Any | None = None


@_dataclasses.dataclass(slots=True)
class ManagedWorkflowRun:
    provider_name: str = ""
    run: Any | None = None


@_dataclasses.dataclass(slots=True)
class ManagedWorkflowRunSignal:
    provider_name: str = ""
    run: Any | None = None
    signal: Any | None = None
    started_run: bool = False
    workflow_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerApplyDefinitionRequest:
    provider_name: str = ""
    spec: Any | None = None
    invocation_token: str = ""
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerGetDefinitionRequest:
    definition_id: str = ""
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerListDefinitionsRequest:
    provider_name: str = ""
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerListDefinitionsResponse:
    definitions: Sequence[Any] | None = None


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeleteDefinitionRequest:
    definition_id: str = ""
    generation: int = 0
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSetDefinitionPausedRequest:
    definition_id: str = ""
    paused: bool = False
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSetActivationPausedRequest:
    definition_id: str = ""
    activation_id: str = ""
    paused: bool = False
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerStartRunRequest:
    provider_name: str = ""
    definition_id: str = ""
    definition_generation: int = 0
    activation_id: str = ""
    workflow_key: str = ""
    input: Any | None = None
    idempotency_key: str = ""
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalRunRequest:
    run_id: str = ""
    signal: Any | None = None
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerSignalOrStartRunRequest:
    provider_name: str = ""
    definition_id: str = ""
    definition_generation: int = 0
    activation_id: str = ""
    workflow_key: str = ""
    input: Any | None = None
    idempotency_key: str = ""
    signal: Any | None = None
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerCancelRunRequest:
    run_id: str = ""
    reason: str = ""
    invocation_token: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeliverEventRequest:
    provider_name: str = ""
    event: Any | None = None
    invocation_token: str = ""
    idempotency_key: str = ""


@_dataclasses.dataclass(slots=True)
class WorkflowManagerDeliverEventResponse:
    results: Sequence[Any] | None = None


WorkflowManagerDefinition = ManagedWorkflowDefinition
WorkflowManagerRun = ManagedWorkflowRun
WorkflowManagerRunSignal = ManagedWorkflowRunSignal


def _optional_struct(value: Any | None) -> Any | None:
    if value is None:
        return None
    if isinstance(value, _struct_pb2.Struct):
        return _copy(value)
    return struct_from_dict(value)


def _optional_value(value: Any) -> Any | None:
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
    if isinstance(item, Mapping):
        return dict(item)
    raise TypeError(
        f"expected protobuf message, mapping, or dataclass, got {type(item).__name__}"
    )


def _selected_oneof(data: Mapping[str, Any], names: Sequence[str]) -> list[str]:
    return [name for name in names if data.get(name) is not None]


def _message_list(values: Sequence[Any] | None, converter: Any) -> list[Any]:
    return [converter(item) for item in (values or [])]


def _workflow_text_or_none(value: Any | None) -> Any | None:
    return workflow_text(value) if value is not None else None


def _workflow_value_or_none(value: Any | None) -> Any | None:
    return workflow_value(value) if value is not None else None


def workflow_text(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowText):
        return _copy(value)
    if isinstance(value, str) and not kwargs:
        return pb.WorkflowText(template=value)
    data = _data(value, kwargs)
    return pb.WorkflowText(template=data.get("template", ""))


def workflow_path_source(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowPathSource):
        return _copy(value)
    if isinstance(value, str) and not kwargs:
        return pb.WorkflowPathSource(path=value)
    data = _data(value, kwargs)
    return pb.WorkflowPathSource(path=data.get("path", ""))


def workflow_step_output_source(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepOutputSource):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepOutputSource(
        step_id=data.get("step_id", ""),
        path=data.get("path", ""),
    )


def workflow_value(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowValue):
        return _copy(value)
    if value is None and not kwargs:
        return pb.WorkflowValue()

    mapping = _dataclass_mapping(value)
    if mapping is None and isinstance(value, Mapping):
        mapping = dict(value)
    known = {
        "literal",
        "object",
        "array",
        "template",
        "run_input",
        "signal_payload",
        "step_output",
    }
    if not kwargs and (mapping is None or not any(name in mapping for name in known)):
        return pb.WorkflowValue(literal=_value(value))

    data = _data(value, kwargs)
    literal = data.get("literal", _MISSING)
    selected = _selected_oneof(data, [name for name in known if name != "literal"])
    if literal is not _MISSING:
        selected.append("literal")
    if not selected:
        return pb.WorkflowValue()
    if len(selected) > 1:
        raise ValueError("workflow value must set exactly one kind")
    kind = selected[0]
    item = data.get(kind)
    if kind == "literal":
        return pb.WorkflowValue(literal=_value(literal))
    if kind == "object":
        return pb.WorkflowValue(object=workflow_object(item))
    if kind == "array":
        return pb.WorkflowValue(array=workflow_array(item))
    if kind == "template":
        return pb.WorkflowValue(template=workflow_text(item))
    if kind == "step_output":
        return pb.WorkflowValue(step_output=workflow_step_output_source(item))
    return pb.WorkflowValue(**{kind: workflow_path_source(item)})


def workflow_object(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowObject):
        return _copy(value)
    data = _data(value, kwargs) if kwargs or _dataclass_mapping(value) is not None else {}
    fields = data.get("fields") if data else value
    return pb.WorkflowObject(
        fields={key: workflow_value(item) for key, item in (fields or {}).items()}
    )


def workflow_array(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowArray):
        return _copy(value)
    if kwargs or _dataclass_mapping(value) is not None or isinstance(value, Mapping):
        data = _data(value, kwargs)
        values = data.get("values")
    else:
        values = value
    return pb.WorkflowArray(values=[workflow_value(item) for item in (values or [])])


def workflow_step_plugin_call(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepPluginCall):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepPluginCall(
        name=data.get("name", ""),
        operation=data.get("operation", ""),
        input=_workflow_value_or_none(data.get("input")),
        connection=data.get("connection", ""),
        instance=data.get("instance", ""),
        credential_mode=data.get("credential_mode", ""),
    )


def workflow_step_delivery(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepDelivery):
        return _copy(value)
    data = _data(value, kwargs)
    plugin = data.get("plugin")
    return pb.WorkflowStepDelivery(
        plugin=workflow_step_plugin_call(plugin) if plugin is not None else None
    )


def workflow_agent_message(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowAgentMessage):
        return _copy(value)
    data = _data(value, kwargs)
    text = data.get("text")
    return pb.WorkflowAgentMessage(
        role=data.get("role", ""),
        text=_workflow_text_or_none(text),
        metadata=_optional_struct(data.get("metadata")),
    )


def workflow_step_agent_turn(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepAgentTurn):
        return _copy(value)
    data = _data(value, kwargs)
    tools: list[Any] = []
    for item in data.get("tools") or []:
        if isinstance(item, _plugin_pb.AgentToolRef):
            tools.append(_copy(item))
        else:
            converted = agent_tool_ref_to_proto(item)
            if converted is None:
                raise TypeError("AgentToolRef item cannot be None")
            tools.append(converted)
    return pb.WorkflowStepAgentTurn(
        provider=data.get("provider", ""),
        model=data.get("model", ""),
        session_key=_workflow_text_or_none(data.get("session_key")),
        prompt=_workflow_text_or_none(data.get("prompt")),
        messages=_message_list(data.get("messages"), workflow_agent_message),
        tools=tools,
        response_schema=_optional_struct(data.get("response_schema")),
        model_options=_optional_struct(data.get("model_options")),
    )


def workflow_step_when(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepWhen):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepWhen(
        value=_workflow_value_or_none(data.get("value")),
        equals=_optional_value(data.get("equals")),
    )


def workflow_step(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStep):
        return _copy(value)
    data = _data(value, kwargs)
    selected = _selected_oneof(data, ("plugin", "agent"))
    if len(selected) > 1:
        raise ValueError("workflow step must set exactly one action")
    step = pb.WorkflowStep(
        id=data.get("id", ""),
        inputs={
            key: workflow_value(item) for key, item in (data.get("inputs") or {}).items()
        },
        when=workflow_step_when(data["when"]) if data.get("when") is not None else None,
        timeout_seconds=data.get("timeout_seconds", 0),
        output_delivery=workflow_step_delivery(data["output_delivery"])
        if data.get("output_delivery") is not None
        else None,
        metadata=_optional_struct(data.get("metadata")),
    )
    if selected == ["plugin"]:
        step.plugin.CopyFrom(workflow_step_plugin_call(data["plugin"]))
    elif selected == ["agent"]:
        step.agent.CopyFrom(workflow_step_agent_turn(data["agent"]))
    return step


def bound_workflow_target(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.BoundWorkflowTarget):
        return _copy(value)
    data = _data(value, kwargs)
    stale = [name for name in ("plugin", "agent") if data.get(name) is not None]
    if stale:
        raise ValueError("bound workflow target now uses steps; plugin/agent targets were removed")
    return pb.BoundWorkflowTarget(
        steps=_message_list(data.get("steps"), workflow_step)
    )


def workflow_actor(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowActor):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowActor(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
    )


def workflow_run_as_subject(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunAsSubject):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunAsSubject(
        subject_id=data.get("subject_id", ""),
        subject_kind=data.get("subject_kind", ""),
        display_name=data.get("display_name", ""),
        auth_source=data.get("auth_source", ""),
        credential_subject_id=data.get("credential_subject_id", ""),
    )


def workflow_event(value: Any | None = None, **kwargs: Any) -> Any:
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


def workflow_event_match(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowEventMatch):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowEventMatch(
        type=data.get("type", ""),
        source=data.get("source", ""),
        subject=data.get("subject", ""),
    )


def workflow_manual_activation(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowManualActivation):
        return _copy(value)
    return pb.WorkflowManualActivation()


def workflow_schedule_activation(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowScheduleActivation):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowScheduleActivation(
        cron=data.get("cron", ""),
        timezone=data.get("timezone", ""),
    )


def workflow_event_activation(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowEventActivation):
        return _copy(value)
    data = _data(value, kwargs)
    match = data.get("match")
    return pb.WorkflowEventActivation(
        match=workflow_event_match(match) if match is not None else None
    )


def workflow_activation(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowActivation):
        return _copy(value)
    data = _data(value, kwargs)
    selected = _selected_oneof(data, ("manual", "schedule", "event"))
    if len(selected) > 1:
        raise ValueError("workflow activation must set exactly one kind")
    activation = pb.WorkflowActivation(
        id=data.get("id", ""),
        paused=data.get("paused", False),
        mode=data.get("mode", WORKFLOW_ACTIVATION_MODE_UNSPECIFIED),
        input=_workflow_value_or_none(data.get("input")),
        run_key=_workflow_value_or_none(data.get("run_key")),
        idempotency_key=_workflow_value_or_none(data.get("idempotency_key")),
    )
    if selected == ["manual"]:
        activation.manual.CopyFrom(workflow_manual_activation(data["manual"]))
    elif selected == ["schedule"]:
        activation.schedule.CopyFrom(workflow_schedule_activation(data["schedule"]))
    elif selected == ["event"]:
        activation.event.CopyFrom(workflow_event_activation(data["event"]))
    return activation


def workflow_access_permission(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowAccessPermission):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowAccessPermission(
        plugin=data.get("plugin", ""),
        operations=list(data.get("operations") or []),
        actions=list(data.get("actions") or []),
    )


def workflow_definition_spec(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowDefinitionSpec):
        return _copy(value)
    data = _data(value, kwargs)
    target = data.get("target")
    run_as = data.get("run_as")
    return pb.WorkflowDefinitionSpec(
        id=data.get("id", ""),
        generation=data.get("generation", 0),
        target=bound_workflow_target(target) if target is not None else None,
        activations=_message_list(data.get("activations"), workflow_activation),
        paused=data.get("paused", False),
        run_as=workflow_run_as_subject(run_as) if run_as is not None else None,
        permissions=_message_list(data.get("permissions"), workflow_access_permission),
        labels=dict(data.get("labels") or {}),
        workflow_semantics_version=data.get("workflow_semantics_version", ""),
    )


def workflow_action_descriptor(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowActionDescriptor):
        return _copy(value)
    data = _data(value, kwargs)
    selected = _selected_oneof(data, ("plugin", "agent"))
    if len(selected) > 1:
        raise ValueError("workflow action descriptor must set at most one action")
    descriptor = pb.WorkflowActionDescriptor(
        action_id=data.get("action_id", ""),
        step_id=data.get("step_id", ""),
        kind=data.get("kind", WORKFLOW_ACTION_KIND_UNSPECIFIED),
    )
    if selected == ["plugin"]:
        descriptor.plugin.CopyFrom(workflow_step_plugin_call(data["plugin"]))
    elif selected == ["agent"]:
        descriptor.agent.CopyFrom(workflow_step_agent_turn(data["agent"]))
    return descriptor


def workflow_action_table(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowActionTable):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowActionTable(
        actions=_message_list(data.get("actions"), workflow_action_descriptor),
        digest=data.get("digest", ""),
    )


def workflow_definition_binding(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowDefinitionBinding):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowDefinitionBinding(**_message_mapping(WorkflowDefinitionBinding(**data)))


def workflow_run_error(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunError):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunError(
        code=data.get("code", ""),
        message=data.get("message", ""),
        step_id=data.get("step_id", ""),
        action_id=data.get("action_id", ""),
    )


def workflow_definition(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowDefinition):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowDefinition(
        spec=workflow_definition_spec(data["spec"]) if data.get("spec") is not None else None,
        status=data.get("status", WORKFLOW_DEFINITION_STATUS_UNSPECIFIED),
        created_at=_optional_timestamp(data.get("created_at")),
        updated_at=_optional_timestamp(data.get("updated_at")),
        applied_generation=data.get("applied_generation", 0),
        spec_digest=data.get("spec_digest", ""),
        target_digest=data.get("target_digest", ""),
        action_table_digest=data.get("action_table_digest", ""),
        provider_plan_id=data.get("provider_plan_id", ""),
        provider_plan_digest=data.get("provider_plan_digest", ""),
        binding=workflow_definition_binding(data["binding"])
        if data.get("binding") is not None
        else None,
        error=workflow_run_error(data["error"]) if data.get("error") is not None else None,
    )


def apply_workflow_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ApplyWorkflowDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ApplyWorkflowDefinitionRequest(
        spec=workflow_definition_spec(data["spec"]) if data.get("spec") is not None else None,
        binding=workflow_definition_binding(data["binding"])
        if data.get("binding") is not None
        else None,
        execution_ref=workflow_execution_reference(data["execution_ref"])
        if data.get("execution_ref") is not None
        else None,
        request_id=data.get("request_id", ""),
    )


def get_workflow_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowDefinitionRequest(definition_id=data.get("definition_id", ""))


def list_workflow_definitions_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ListWorkflowDefinitionsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ListWorkflowDefinitionsRequest(
        page_size=data.get("page_size", 0),
        page_token=data.get("page_token", ""),
        labels=dict(data.get("labels") or {}),
    )


def list_workflow_definitions_response(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ListWorkflowDefinitionsResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ListWorkflowDefinitionsResponse(
        definitions=_message_list(data.get("definitions"), workflow_definition),
        next_page_token=data.get("next_page_token", ""),
    )


def delete_workflow_definition_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.DeleteWorkflowDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.DeleteWorkflowDefinitionRequest(
        definition_id=data.get("definition_id", ""),
        generation=data.get("generation", 0),
        request_id=data.get("request_id", ""),
    )


def set_workflow_definition_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SetWorkflowDefinitionPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SetWorkflowDefinitionPausedRequest(
        definition_id=data.get("definition_id", ""),
        paused=data.get("paused", False),
        request_id=data.get("request_id", ""),
    )


def set_workflow_activation_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SetWorkflowActivationPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SetWorkflowActivationPausedRequest(
        definition_id=data.get("definition_id", ""),
        activation_id=data.get("activation_id", ""),
        paused=data.get("paused", False),
        request_id=data.get("request_id", ""),
    )


def workflow_manual_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowManualTrigger):
        return _copy(value)
    return pb.WorkflowManualTrigger()


def workflow_schedule_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowScheduleTrigger):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowScheduleTrigger(
        activation_id=data.get("activation_id", ""),
        scheduled_for=_optional_timestamp(data.get("scheduled_for")),
    )


def workflow_event_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowEventTrigger):
        return _copy(value)
    data = _data(value, kwargs)
    event = data.get("event")
    return pb.WorkflowEventTrigger(
        activation_id=data.get("activation_id", ""),
        event=workflow_event(event) if event is not None else None,
    )


def workflow_run_trigger(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunTrigger):
        return _copy(value)
    data = _data(value, kwargs)
    selected = _selected_oneof(data, ("manual", "schedule", "event"))
    if len(selected) > 1:
        raise ValueError("workflow run trigger must set exactly one kind")
    trigger = pb.WorkflowRunTrigger(
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        activation_id=data.get("activation_id", ""),
    )
    if selected == ["manual"]:
        trigger.manual.CopyFrom(workflow_manual_trigger(data["manual"]))
    elif selected == ["schedule"]:
        trigger.schedule.CopyFrom(workflow_schedule_trigger(data["schedule"]))
    elif selected == ["event"]:
        trigger.event.CopyFrom(workflow_event_trigger(data["event"]))
    return trigger


def workflow_signal(value: Any | None = None, **kwargs: Any) -> Any:
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


def workflow_output_summary(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowOutputSummary):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowOutputSummary(
        envelope_version=data.get("envelope_version", ""),
        kind=data.get("kind", ""),
        size_bytes=data.get("size_bytes", 0),
        sha256=data.get("sha256", ""),
        truncated=data.get("truncated", False),
        redacted=data.get("redacted", False),
        media_type=data.get("media_type", ""),
    )


def workflow_step_state(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowStepState):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowStepState(
        step_id=data.get("step_id", ""),
        step_index=data.get("step_index", 0),
        status=data.get("status", WORKFLOW_STEP_STATUS_UNSPECIFIED),
        skipped_reason=data.get("skipped_reason", ""),
        attempt_number=data.get("attempt_number", 0),
        output_summary=workflow_output_summary(data["output_summary"])
        if data.get("output_summary") is not None
        else None,
        output_ref=data.get("output_ref", ""),
        error=workflow_run_error(data["error"]) if data.get("error") is not None else None,
        updated_at=_optional_timestamp(data.get("updated_at")),
    )


def workflow_run(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRun):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRun(
        id=data.get("id", ""),
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        workflow_key=data.get("workflow_key", ""),
        status=data.get("status", WORKFLOW_RUN_STATUS_UNSPECIFIED),
        trigger=workflow_run_trigger(data["trigger"]) if data.get("trigger") is not None else None,
        input=_optional_struct(data.get("input")),
        created_by=workflow_actor(data["created_by"]) if data.get("created_by") is not None else None,
        created_at=_optional_timestamp(data.get("created_at")),
        started_at=_optional_timestamp(data.get("started_at")),
        completed_at=_optional_timestamp(data.get("completed_at")),
        status_message=data.get("status_message", ""),
        execution_ref=data.get("execution_ref", ""),
        execution_ref_generation=data.get("execution_ref_generation", 0),
        target_digest=data.get("target_digest", ""),
        spec_digest=data.get("spec_digest", ""),
        action_table_digest=data.get("action_table_digest", ""),
        steps=_message_list(data.get("steps"), workflow_step_state),
        error=workflow_run_error(data["error"]) if data.get("error") is not None else None,
    )


def start_workflow_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.StartWorkflowRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.StartWorkflowRunRequest(
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        activation_id=data.get("activation_id", ""),
        workflow_key=data.get("workflow_key", ""),
        input=_optional_struct(data.get("input")),
        idempotency_key=data.get("idempotency_key", ""),
        created_by=workflow_actor(data["created_by"]) if data.get("created_by") is not None else None,
    )


def signal_workflow_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.SignalWorkflowRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SignalWorkflowRunRequest(
        run_id=data.get("run_id", ""),
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
    )


def signal_or_start_workflow_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.SignalOrStartWorkflowRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.SignalOrStartWorkflowRunRequest(
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        activation_id=data.get("activation_id", ""),
        workflow_key=data.get("workflow_key", ""),
        input=_optional_struct(data.get("input")),
        idempotency_key=data.get("idempotency_key", ""),
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        created_by=workflow_actor(data["created_by"]) if data.get("created_by") is not None else None,
    )


def cancel_workflow_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.CancelWorkflowRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.CancelWorkflowRunRequest(
        run_id=data.get("run_id", ""),
        reason=data.get("reason", ""),
    )


def get_workflow_run_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowRunRequest(run_id=data.get("run_id", ""))


def list_workflow_runs_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ListWorkflowRunsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ListWorkflowRunsRequest(
        definition_id=data.get("definition_id", ""),
        page_size=data.get("page_size", 0),
        page_token=data.get("page_token", ""),
        status=data.get("status", WORKFLOW_RUN_STATUS_UNSPECIFIED),
    )


def list_workflow_runs_response(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ListWorkflowRunsResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ListWorkflowRunsResponse(
        runs=_message_list(data.get("runs"), workflow_run),
        next_page_token=data.get("next_page_token", ""),
    )


def workflow_run_signal(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunSignal):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunSignal(
        run=workflow_run(data["run"]) if data.get("run") is not None else None,
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        started_run=data.get("started_run", False),
        workflow_key=data.get("workflow_key", ""),
    )


def deliver_workflow_event_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.DeliverWorkflowEventRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.DeliverWorkflowEventRequest(
        delivery_id=data.get("delivery_id", ""),
        event=workflow_event(data["event"]) if data.get("event") is not None else None,
        published_by=workflow_actor(data["published_by"])
        if data.get("published_by") is not None
        else None,
        idempotency_key=data.get("idempotency_key", ""),
    )


def workflow_event_delivery_result(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowEventDeliveryResult):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowEventDeliveryResult(
        definition_id=data.get("definition_id", ""),
        activation_id=data.get("activation_id", ""),
        run=workflow_run(data["run"]) if data.get("run") is not None else None,
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        started_run=data.get("started_run", False),
    )


def deliver_workflow_event_response(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.DeliverWorkflowEventResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.DeliverWorkflowEventResponse(
        results=_message_list(data.get("results"), workflow_event_delivery_result)
    )


def workflow_run_event(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunEvent):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowRunEvent(
        id=data.get("id", ""),
        run_id=data.get("run_id", ""),
        sequence=data.get("sequence", 0),
        type=data.get("type", WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED),
        step_id=data.get("step_id", ""),
        action_id=data.get("action_id", ""),
        attempt_number=data.get("attempt_number", 0),
        message=data.get("message", ""),
        output_summary=workflow_output_summary(data["output_summary"])
        if data.get("output_summary") is not None
        else None,
        output_ref=data.get("output_ref", ""),
        error=workflow_run_error(data["error"]) if data.get("error") is not None else None,
        observed_at=_optional_timestamp(data.get("observed_at")),
    )


def get_workflow_run_events_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowRunEventsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowRunEventsRequest(
        run_id=data.get("run_id", ""),
        page_size=data.get("page_size", 0),
        page_token=data.get("page_token", ""),
    )


def list_workflow_run_events_response(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ListWorkflowRunEventsResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ListWorkflowRunEventsResponse(
        events=_message_list(data.get("events"), workflow_run_event),
        next_page_token=data.get("next_page_token", ""),
    )


def get_workflow_run_output_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.GetWorkflowRunOutputRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.GetWorkflowRunOutputRequest(
        run_id=data.get("run_id", ""),
        output_ref=data.get("output_ref", ""),
        step_id=data.get("step_id", ""),
    )


def workflow_run_output(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowRunOutput):
        return _copy(value)
    data = _data(value, kwargs)
    body = data.get("body", _MISSING)
    return pb.WorkflowRunOutput(
        output_ref=data.get("output_ref", ""),
        summary=workflow_output_summary(data["summary"]) if data.get("summary") is not None else None,
        body=_value(body) if body is not _MISSING else None,
    )


def workflow_host_action_selector(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowHostActionSelector):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowHostActionSelector(**_message_mapping(WorkflowHostActionSelector(**data)))


def workflow_plugin_action_payload(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowPluginActionPayload):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowPluginActionPayload(input=_optional_struct(data.get("input")))


def workflow_agent_turn_payload(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowAgentTurnPayload):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowAgentTurnPayload(
        prompt=_workflow_text_or_none(data.get("prompt")),
        messages=_message_list(data.get("messages"), workflow_agent_message),
    )


def invoke_workflow_action_request(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.InvokeWorkflowActionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    selected = _selected_oneof(data, ("plugin", "agent_turn"))
    if len(selected) > 1:
        raise ValueError("invoke workflow action request must set exactly one action")
    request = pb.InvokeWorkflowActionRequest(
        selector=workflow_host_action_selector(data["selector"])
        if data.get("selector") is not None
        else None,
        metadata=_optional_struct(data.get("metadata")),
        trigger=workflow_run_trigger(data["trigger"])
        if data.get("trigger") is not None
        else None,
        signals=_message_list(data.get("signals"), workflow_signal),
    )
    if selected == ["plugin"]:
        request.plugin.CopyFrom(workflow_plugin_action_payload(data["plugin"]))
    elif selected == ["agent_turn"]:
        request.agent_turn.CopyFrom(workflow_agent_turn_payload(data["agent_turn"]))
    return request


def workflow_action_result(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.WorkflowActionResult):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowActionResult(
        action_event_id=data.get("action_event_id", ""),
        status=data.get("status", 0),
        body=data.get("body", ""),
        output_summary=workflow_output_summary(data["output_summary"])
        if data.get("output_summary") is not None
        else None,
        output_ref=data.get("output_ref", ""),
        error=workflow_run_error(data["error"]) if data.get("error") is not None else None,
    )


def managed_workflow_definition(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ManagedWorkflowDefinition):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ManagedWorkflowDefinition(
        provider_name=data.get("provider_name", ""),
        definition=workflow_definition(data["definition"])
        if data.get("definition") is not None
        else None,
    )


def managed_workflow_run(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ManagedWorkflowRun):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ManagedWorkflowRun(
        provider_name=data.get("provider_name", ""),
        run=workflow_run(data["run"]) if data.get("run") is not None else None,
    )


def managed_workflow_run_signal(value: Any | None = None, **kwargs: Any) -> Any:
    if isinstance(value, pb.ManagedWorkflowRunSignal):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.ManagedWorkflowRunSignal(
        provider_name=data.get("provider_name", ""),
        run=workflow_run(data["run"]) if data.get("run") is not None else None,
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        started_run=data.get("started_run", False),
        workflow_key=data.get("workflow_key", ""),
    )


def workflow_manager_apply_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerApplyDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerApplyDefinitionRequest(
        provider_name=data.get("provider_name", ""),
        spec=workflow_definition_spec(data["spec"]) if data.get("spec") is not None else None,
        invocation_token=data.get("invocation_token", ""),
        idempotency_key=data.get("idempotency_key", ""),
    )


def workflow_manager_get_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerGetDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerGetDefinitionRequest(
        definition_id=data.get("definition_id", ""),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_list_definitions_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerListDefinitionsRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerListDefinitionsRequest(
        provider_name=data.get("provider_name", ""),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_list_definitions_response(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerListDefinitionsResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerListDefinitionsResponse(
        definitions=_message_list(data.get("definitions"), managed_workflow_definition)
    )


def workflow_manager_delete_definition_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerDeleteDefinitionRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerDeleteDefinitionRequest(
        definition_id=data.get("definition_id", ""),
        generation=data.get("generation", 0),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_set_definition_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSetDefinitionPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerSetDefinitionPausedRequest(
        definition_id=data.get("definition_id", ""),
        paused=data.get("paused", False),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_set_activation_paused_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSetActivationPausedRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerSetActivationPausedRequest(
        definition_id=data.get("definition_id", ""),
        activation_id=data.get("activation_id", ""),
        paused=data.get("paused", False),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_start_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerStartRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerStartRunRequest(
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        activation_id=data.get("activation_id", ""),
        workflow_key=data.get("workflow_key", ""),
        input=_optional_struct(data.get("input")),
        idempotency_key=data.get("idempotency_key", ""),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_signal_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSignalRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerSignalRunRequest(
        run_id=data.get("run_id", ""),
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_signal_or_start_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerSignalOrStartRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerSignalOrStartRunRequest(
        provider_name=data.get("provider_name", ""),
        definition_id=data.get("definition_id", ""),
        definition_generation=data.get("definition_generation", 0),
        activation_id=data.get("activation_id", ""),
        workflow_key=data.get("workflow_key", ""),
        input=_optional_struct(data.get("input")),
        idempotency_key=data.get("idempotency_key", ""),
        signal=workflow_signal(data["signal"]) if data.get("signal") is not None else None,
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_cancel_run_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerCancelRunRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerCancelRunRequest(
        run_id=data.get("run_id", ""),
        reason=data.get("reason", ""),
        invocation_token=data.get("invocation_token", ""),
    )


def workflow_manager_deliver_event_request(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerDeliverEventRequest):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerDeliverEventRequest(
        provider_name=data.get("provider_name", ""),
        event=workflow_event(data["event"]) if data.get("event") is not None else None,
        invocation_token=data.get("invocation_token", ""),
        idempotency_key=data.get("idempotency_key", ""),
    )


def workflow_manager_deliver_event_response(
    value: Any | None = None, **kwargs: Any
) -> Any:
    if isinstance(value, pb.WorkflowManagerDeliverEventResponse):
        return _copy(value)
    data = _data(value, kwargs)
    return pb.WorkflowManagerDeliverEventResponse(
        results=_message_list(data.get("results"), workflow_event_delivery_result)
    )


_NATIVE_BY_PROTO_NAME: dict[str, type[Any]] = {
    cls.__name__: cls
    for cls in (
        WorkflowText,
        WorkflowPathSource,
        WorkflowStepOutputSource,
        WorkflowValue,
        WorkflowObject,
        WorkflowArray,
        WorkflowStepPluginCall,
        WorkflowStepDelivery,
        WorkflowAgentMessage,
        WorkflowStepAgentTurn,
        WorkflowStepWhen,
        WorkflowStep,
        BoundWorkflowTarget,
        WorkflowActor,
        WorkflowRunAsSubject,
        WorkflowEvent,
        WorkflowEventMatch,
        WorkflowManualActivation,
        WorkflowScheduleActivation,
        WorkflowEventActivation,
        WorkflowActivation,
        WorkflowAccessPermission,
        WorkflowDefinitionSpec,
        WorkflowActionDescriptor,
        WorkflowActionTable,
        WorkflowDefinitionBinding,
        WorkflowRunError,
        WorkflowDefinition,
        ApplyWorkflowDefinitionRequest,
        GetWorkflowDefinitionRequest,
        ListWorkflowDefinitionsRequest,
        ListWorkflowDefinitionsResponse,
        DeleteWorkflowDefinitionRequest,
        SetWorkflowDefinitionPausedRequest,
        SetWorkflowActivationPausedRequest,
        WorkflowManualTrigger,
        WorkflowScheduleTrigger,
        WorkflowEventTrigger,
        WorkflowRunTrigger,
        WorkflowSignal,
        WorkflowOutputSummary,
        WorkflowStepState,
        WorkflowRun,
        StartWorkflowRunRequest,
        SignalWorkflowRunRequest,
        SignalOrStartWorkflowRunRequest,
        CancelWorkflowRunRequest,
        GetWorkflowRunRequest,
        ListWorkflowRunsRequest,
        ListWorkflowRunsResponse,
        WorkflowRunSignal,
        DeliverWorkflowEventRequest,
        WorkflowEventDeliveryResult,
        DeliverWorkflowEventResponse,
        WorkflowRunEvent,
        GetWorkflowRunEventsRequest,
        ListWorkflowRunEventsResponse,
        GetWorkflowRunOutputRequest,
        WorkflowRunOutput,
        WorkflowHostActionSelector,
        WorkflowPluginActionPayload,
        WorkflowAgentTurnPayload,
        InvokeWorkflowActionRequest,
        WorkflowActionResult,
        ManagedWorkflowDefinition,
        ManagedWorkflowRun,
        ManagedWorkflowRunSignal,
        WorkflowManagerApplyDefinitionRequest,
        WorkflowManagerGetDefinitionRequest,
        WorkflowManagerListDefinitionsRequest,
        WorkflowManagerListDefinitionsResponse,
        WorkflowManagerDeleteDefinitionRequest,
        WorkflowManagerSetDefinitionPausedRequest,
        WorkflowManagerSetActivationPausedRequest,
        WorkflowManagerStartRunRequest,
        WorkflowManagerSignalRunRequest,
        WorkflowManagerSignalOrStartRunRequest,
        WorkflowManagerCancelRunRequest,
        WorkflowManagerDeliverEventRequest,
        WorkflowManagerDeliverEventResponse,
    )
}


def _native_from_proto(value: Any | None) -> Any:
    if value is None:
        return None
    if isinstance(value, _struct_pb2.Struct):
        return struct_to_dict(value)
    if isinstance(value, _struct_pb2.Value):
        return value_to_json(value)
    if isinstance(value, _timestamp_pb2.Timestamp):
        return datetime_from_timestamp(value)
    if isinstance(value, _plugin_pb.AgentToolRef):
        return agent_tool_ref_from_proto(value)
    if not isinstance(value, _message.Message):
        return value
    cls = _NATIVE_BY_PROTO_NAME.get(type(value).__name__)
    if cls is None:
        return _copy(value)
    kwargs: dict[str, Any] = {}
    for field in _dataclasses.fields(cls):
        if not hasattr(value, field.name):
            continue
        item = getattr(value, field.name)
        if isinstance(item, _message.Message):
            if not has_field(value, field.name):
                continue
            kwargs[field.name] = _native_from_proto(item)
            continue
        if hasattr(item, "items"):
            kwargs[field.name] = {
                key: _native_from_proto(entry) for key, entry in item.items()
            }
            continue
        if (
            isinstance(item, Sequence)
            and not isinstance(item, str | bytes | bytearray)
        ):
            kwargs[field.name] = [_native_from_proto(entry) for entry in item]
            continue
        kwargs[field.name] = item
    return cls(**kwargs)


def _from_proto(value: Any) -> Any:
    return _native_from_proto(value)


def apply_workflow_definition_request_from_proto(
    value: Any,
) -> ApplyWorkflowDefinitionRequest:
    return _from_proto(value)


def get_workflow_definition_request_from_proto(
    value: Any,
) -> GetWorkflowDefinitionRequest:
    return _from_proto(value)


def list_workflow_definitions_request_from_proto(
    value: Any,
) -> ListWorkflowDefinitionsRequest:
    return _from_proto(value)


def list_workflow_definitions_response_to_proto(value: Any) -> Any:
    return list_workflow_definitions_response(value)


def delete_workflow_definition_request_from_proto(
    value: Any,
) -> DeleteWorkflowDefinitionRequest:
    return _from_proto(value)


def set_workflow_definition_paused_request_from_proto(
    value: Any,
) -> SetWorkflowDefinitionPausedRequest:
    return _from_proto(value)


def set_workflow_activation_paused_request_from_proto(
    value: Any,
) -> SetWorkflowActivationPausedRequest:
    return _from_proto(value)


def start_workflow_run_request_from_proto(value: Any) -> StartWorkflowRunRequest:
    return _from_proto(value)


def signal_workflow_run_request_from_proto(value: Any) -> SignalWorkflowRunRequest:
    return _from_proto(value)


def signal_or_start_workflow_run_request_from_proto(
    value: Any,
) -> SignalOrStartWorkflowRunRequest:
    return _from_proto(value)


def cancel_workflow_run_request_from_proto(value: Any) -> CancelWorkflowRunRequest:
    return _from_proto(value)


def deliver_workflow_event_request_from_proto(
    value: Any,
) -> DeliverWorkflowEventRequest:
    return _from_proto(value)


def deliver_workflow_event_response_to_proto(value: Any) -> Any:
    return deliver_workflow_event_response(value)


def get_workflow_run_request_from_proto(value: Any) -> GetWorkflowRunRequest:
    return _from_proto(value)


def list_workflow_runs_request_from_proto(value: Any) -> ListWorkflowRunsRequest:
    return _from_proto(value)


def list_workflow_runs_response_to_proto(value: Any) -> Any:
    return list_workflow_runs_response(value)


def get_workflow_run_events_request_from_proto(
    value: Any,
) -> GetWorkflowRunEventsRequest:
    return _from_proto(value)


def list_workflow_run_events_response_to_proto(value: Any) -> Any:
    return list_workflow_run_events_response(value)


def get_workflow_run_output_request_from_proto(
    value: Any,
) -> GetWorkflowRunOutputRequest:
    return _from_proto(value)


def workflow_definition_to_proto(value: Any) -> Any:
    return workflow_definition(value)


def workflow_run_to_proto(value: Any) -> Any:
    return workflow_run(value)


def workflow_run_signal_to_proto(value: Any) -> Any:
    return workflow_run_signal(value)


def workflow_run_output_to_proto(value: Any) -> Any:
    return workflow_run_output(value)


def workflow_action_result_from_proto(value: Any) -> WorkflowActionResult:
    return _from_proto(value)


def managed_workflow_definition_from_proto(value: Any) -> ManagedWorkflowDefinition:
    return _from_proto(value)


def managed_workflow_run_from_proto(value: Any) -> ManagedWorkflowRun:
    return _from_proto(value)


def managed_workflow_run_signal_from_proto(value: Any) -> ManagedWorkflowRunSignal:
    return _from_proto(value)


def workflow_manager_list_definitions_response_from_proto(
    value: Any,
) -> WorkflowManagerListDefinitionsResponse:
    return _from_proto(value)


def workflow_manager_deliver_event_response_from_proto(
    value: Any,
) -> WorkflowManagerDeliverEventResponse:
    return _from_proto(value)


def workflow_run_status_name(status: int) -> str:
    try:
        return pb.WorkflowRunStatus.Name(status)
    except ValueError:
        return str(status)


class WorkflowHost:
    """Client for invoking workflow actions through the workflow host."""

    def __init__(self) -> None:
        target = os.environ.get(ENV_WORKFLOW_HOST_SOCKET, "")
        if not target:
            raise RuntimeError(f"{ENV_WORKFLOW_HOST_SOCKET} is not set")
        relay_token = os.environ.get(ENV_WORKFLOW_HOST_SOCKET_TOKEN, "")
        self._channel = host_service_channel("workflow host", target, token=relay_token)
        self._stub = pb_grpc.WorkflowHostStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def invoke_action(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowActionResult:
        response = _grpc_call(
            self._stub.InvokeWorkflowAction,
            invoke_workflow_action_request(request, **kwargs),
        )
        return workflow_action_result_from_proto(response)

    def __enter__(self) -> WorkflowHost:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class WorkflowManager:
    """Client for workflow definition and run management from provider code."""

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
        self._channel.close()

    def _attach_token(self, request: Any, *, idempotent: bool = False) -> Any:
        request.invocation_token = self._invocation_token
        if idempotent and not getattr(request, "idempotency_key", "").strip():
            request.idempotency_key = self._idempotency_key
        return request

    def apply_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowDefinition:
        req = self._attach_token(
            workflow_manager_apply_definition_request(request, **kwargs),
            idempotent=True,
        )
        return managed_workflow_definition_from_proto(
            _grpc_call(self._stub.ApplyDefinition, req)
        )

    def get_definition(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowDefinition:
        req = self._attach_token(workflow_manager_get_definition_request(request, **kwargs))
        return managed_workflow_definition_from_proto(
            _grpc_call(self._stub.GetDefinition, req)
        )

    def list_definitions(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerListDefinitionsResponse:
        req = self._attach_token(
            workflow_manager_list_definitions_request(request, **kwargs)
        )
        return workflow_manager_list_definitions_response_from_proto(
            _grpc_call(self._stub.ListDefinitions, req)
        )

    def delete_definition(self, request: Any | None = None, **kwargs: Any) -> None:
        req = self._attach_token(
            workflow_manager_delete_definition_request(request, **kwargs)
        )
        _grpc_call(self._stub.DeleteDefinition, req)
        return None

    def set_definition_paused(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowDefinition:
        req = self._attach_token(
            workflow_manager_set_definition_paused_request(request, **kwargs)
        )
        return managed_workflow_definition_from_proto(
            _grpc_call(self._stub.SetDefinitionPaused, req)
        )

    def set_activation_paused(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowDefinition:
        req = self._attach_token(
            workflow_manager_set_activation_paused_request(request, **kwargs)
        )
        return managed_workflow_definition_from_proto(
            _grpc_call(self._stub.SetActivationPaused, req)
        )

    def start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowRun:
        req = self._attach_token(
            workflow_manager_start_run_request(request, **kwargs),
            idempotent=True,
        )
        return managed_workflow_run_from_proto(_grpc_call(self._stub.StartRun, req))

    def signal_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowRunSignal:
        req = self._attach_token(workflow_manager_signal_run_request(request, **kwargs))
        return managed_workflow_run_signal_from_proto(
            _grpc_call(self._stub.SignalRun, req)
        )

    def signal_or_start_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowRunSignal:
        req = self._attach_token(
            workflow_manager_signal_or_start_run_request(request, **kwargs),
            idempotent=True,
        )
        return managed_workflow_run_signal_from_proto(
            _grpc_call(self._stub.SignalOrStartRun, req)
        )

    def cancel_run(
        self, request: Any | None = None, **kwargs: Any
    ) -> ManagedWorkflowRun:
        req = self._attach_token(workflow_manager_cancel_run_request(request, **kwargs))
        return managed_workflow_run_from_proto(_grpc_call(self._stub.CancelRun, req))

    def deliver_event(
        self, request: Any | None = None, **kwargs: Any
    ) -> WorkflowManagerDeliverEventResponse:
        req = self._attach_token(
            workflow_manager_deliver_event_request(request, **kwargs),
            idempotent=True,
        )
        return workflow_manager_deliver_event_response_from_proto(
            _grpc_call(self._stub.DeliverEvent, req)
        )

    def __enter__(self) -> WorkflowManager:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _grpc_call(method: Any, request: Any) -> Any:
    try:
        return method(request)
    except grpc.RpcError:
        raise
