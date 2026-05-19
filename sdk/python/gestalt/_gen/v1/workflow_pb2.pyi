import datetime

from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import agent_pb2 as _agent_pb2
from . import plugin_pb2 as _plugin_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class WorkflowRunStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_RUN_STATUS_UNSPECIFIED: _ClassVar[WorkflowRunStatus]
    WORKFLOW_RUN_STATUS_PENDING: _ClassVar[WorkflowRunStatus]
    WORKFLOW_RUN_STATUS_RUNNING: _ClassVar[WorkflowRunStatus]
    WORKFLOW_RUN_STATUS_SUCCEEDED: _ClassVar[WorkflowRunStatus]
    WORKFLOW_RUN_STATUS_FAILED: _ClassVar[WorkflowRunStatus]
    WORKFLOW_RUN_STATUS_CANCELED: _ClassVar[WorkflowRunStatus]
WORKFLOW_RUN_STATUS_UNSPECIFIED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_PENDING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_RUNNING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_SUCCEEDED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_FAILED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_CANCELED: WorkflowRunStatus

class BoundWorkflowTarget(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    plugin: BoundWorkflowPluginTarget
    agent: BoundWorkflowAgentTarget
    def __init__(self, plugin: _Optional[_Union[BoundWorkflowPluginTarget, _Mapping]] = ..., agent: _Optional[_Union[BoundWorkflowAgentTarget, _Mapping]] = ...) -> None: ...

class BoundWorkflowPluginTarget(_message.Message):
    __slots__ = ()
    PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    plugin_name: str
    operation: str
    input: _struct_pb2.Struct
    connection: str
    instance: str
    credential_mode: str
    def __init__(self, plugin_name: _Optional[str] = ..., operation: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., credential_mode: _Optional[str] = ...) -> None: ...

class BoundWorkflowAgentTarget(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_DELIVERY_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    SESSION_READY_DELIVERY_FIELD_NUMBER: _ClassVar[int]
    STEPS_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    model: str
    prompt: str
    messages: _containers.RepeatedCompositeFieldContainer[_agent_pb2.AgentMessage]
    tool_refs: _containers.RepeatedCompositeFieldContainer[_plugin_pb2.AgentToolRef]
    response_schema: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    timeout_seconds: int
    output_delivery: WorkflowOutputDelivery
    model_options: _struct_pb2.Struct
    session_ready_delivery: WorkflowOutputDelivery
    steps: _containers.RepeatedCompositeFieldContainer[WorkflowAgentStep]
    def __init__(self, provider_name: _Optional[str] = ..., model: _Optional[str] = ..., prompt: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[_agent_pb2.AgentMessage, _Mapping]]] = ..., tool_refs: _Optional[_Iterable[_Union[_plugin_pb2.AgentToolRef, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., timeout_seconds: _Optional[int] = ..., output_delivery: _Optional[_Union[WorkflowOutputDelivery, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., session_ready_delivery: _Optional[_Union[WorkflowOutputDelivery, _Mapping]] = ..., steps: _Optional[_Iterable[_Union[WorkflowAgentStep, _Mapping]]] = ...) -> None: ...

class WorkflowAgentStep(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_DELIVERY_FIELD_NUMBER: _ClassVar[int]
    WHEN_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    id: str
    prompt: str
    messages: _containers.RepeatedCompositeFieldContainer[_agent_pb2.AgentMessage]
    tool_refs: _containers.RepeatedCompositeFieldContainer[_plugin_pb2.AgentToolRef]
    response_schema: _struct_pb2.Struct
    model_options: _struct_pb2.Struct
    timeout_seconds: int
    output_delivery: WorkflowOutputDelivery
    when: WorkflowAgentStepWhen
    metadata: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., prompt: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[_agent_pb2.AgentMessage, _Mapping]]] = ..., tool_refs: _Optional[_Iterable[_Union[_plugin_pb2.AgentToolRef, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., timeout_seconds: _Optional[int] = ..., output_delivery: _Optional[_Union[WorkflowOutputDelivery, _Mapping]] = ..., when: _Optional[_Union[WorkflowAgentStepWhen, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class WorkflowAgentStepWhen(_message.Message):
    __slots__ = ()
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_PATH_FIELD_NUMBER: _ClassVar[int]
    EQUALS_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    output_path: str
    equals: _struct_pb2.Value
    def __init__(self, step_id: _Optional[str] = ..., output_path: _Optional[str] = ..., equals: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class WorkflowOutputDelivery(_message.Message):
    __slots__ = ()
    TARGET_FIELD_NUMBER: _ClassVar[int]
    INPUT_BINDINGS_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    target: BoundWorkflowPluginTarget
    input_bindings: _containers.RepeatedCompositeFieldContainer[WorkflowOutputBinding]
    credential_mode: str
    def __init__(self, target: _Optional[_Union[BoundWorkflowPluginTarget, _Mapping]] = ..., input_bindings: _Optional[_Iterable[_Union[WorkflowOutputBinding, _Mapping]]] = ..., credential_mode: _Optional[str] = ...) -> None: ...

class WorkflowOutputBinding(_message.Message):
    __slots__ = ()
    INPUT_FIELD_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    input_field: str
    value: WorkflowOutputValueSource
    def __init__(self, input_field: _Optional[str] = ..., value: _Optional[_Union[WorkflowOutputValueSource, _Mapping]] = ...) -> None: ...

class WorkflowOutputValueSource(_message.Message):
    __slots__ = ()
    AGENT_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_METADATA_FIELD_NUMBER: _ClassVar[int]
    LITERAL_FIELD_NUMBER: _ClassVar[int]
    AGENT_SESSION_FIELD_NUMBER: _ClassVar[int]
    agent_output: str
    signal_payload: str
    signal_metadata: str
    literal: _struct_pb2.Value
    agent_session: str
    def __init__(self, agent_output: _Optional[str] = ..., signal_payload: _Optional[str] = ..., signal_metadata: _Optional[str] = ..., literal: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., agent_session: _Optional[str] = ...) -> None: ...

class WorkflowActor(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    display_name: str
    auth_source: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ...) -> None: ...

class WorkflowRunAsSubject(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    display_name: str
    auth_source: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ...) -> None: ...

class WorkflowEvent(_message.Message):
    __slots__ = ()
    class ExtensionsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: _struct_pb2.Value
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    SPEC_VERSION_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    TIME_FIELD_NUMBER: _ClassVar[int]
    DATACONTENTTYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    EXTENSIONS_FIELD_NUMBER: _ClassVar[int]
    id: str
    source: str
    spec_version: str
    type: str
    subject: str
    time: _timestamp_pb2.Timestamp
    datacontenttype: str
    data: _struct_pb2.Struct
    extensions: _containers.MessageMap[str, _struct_pb2.Value]
    def __init__(self, id: _Optional[str] = ..., source: _Optional[str] = ..., spec_version: _Optional[str] = ..., type: _Optional[str] = ..., subject: _Optional[str] = ..., time: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., datacontenttype: _Optional[str] = ..., data: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., extensions: _Optional[_Mapping[str, _struct_pb2.Value]] = ...) -> None: ...

class WorkflowEventMatch(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    type: str
    source: str
    subject: str
    def __init__(self, type: _Optional[str] = ..., source: _Optional[str] = ..., subject: _Optional[str] = ...) -> None: ...

class WorkflowManualTrigger(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class WorkflowScheduleTrigger(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    SCHEDULED_FOR_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    scheduled_for: _timestamp_pb2.Timestamp
    def __init__(self, schedule_id: _Optional[str] = ..., scheduled_for: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowEventTriggerInvocation(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    event: WorkflowEvent
    def __init__(self, trigger_id: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ...) -> None: ...

class WorkflowRunTrigger(_message.Message):
    __slots__ = ()
    MANUAL_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    manual: WorkflowManualTrigger
    schedule: WorkflowScheduleTrigger
    event: WorkflowEventTriggerInvocation
    def __init__(self, manual: _Optional[_Union[WorkflowManualTrigger, _Mapping]] = ..., schedule: _Optional[_Union[WorkflowScheduleTrigger, _Mapping]] = ..., event: _Optional[_Union[WorkflowEventTriggerInvocation, _Mapping]] = ...) -> None: ...

class BoundWorkflowRun(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RESULT_BODY_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    id: str
    status: WorkflowRunStatus
    target: BoundWorkflowTarget
    trigger: WorkflowRunTrigger
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    status_message: str
    result_body: str
    created_by: WorkflowActor
    execution_ref: str
    workflow_key: str
    def __init__(self, id: _Optional[str] = ..., status: _Optional[_Union[WorkflowRunStatus, str]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., trigger: _Optional[_Union[WorkflowRunTrigger, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., status_message: _Optional[str] = ..., result_body: _Optional[str] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class BoundWorkflowSchedule(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    CRON_FIELD_NUMBER: _ClassVar[int]
    TIMEZONE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    NEXT_RUN_AT_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    id: str
    cron: str
    timezone: str
    target: BoundWorkflowTarget
    paused: bool
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    next_run_at: _timestamp_pb2.Timestamp
    created_by: WorkflowActor
    execution_ref: str
    def __init__(self, id: _Optional[str] = ..., cron: _Optional[str] = ..., timezone: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., next_run_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ...) -> None: ...

class BoundWorkflowEventTrigger(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    id: str
    match: WorkflowEventMatch
    target: BoundWorkflowTarget
    paused: bool
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    created_by: WorkflowActor
    execution_ref: str
    def __init__(self, id: _Optional[str] = ..., match: _Optional[_Union[WorkflowEventMatch, _Mapping]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ...) -> None: ...

class BoundWorkflowDefinition(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    target: BoundWorkflowTarget
    created_by: WorkflowActor
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowAccessPermission(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operations: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, plugin: _Optional[str] = ..., operations: _Optional[_Iterable[str]] = ...) -> None: ...

class WorkflowExecutionReference(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REVOKED_AT_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    CALLER_PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    provider_name: str
    target: BoundWorkflowTarget
    subject_id: str
    credential_subject_id: str
    permissions: _containers.RepeatedCompositeFieldContainer[WorkflowAccessPermission]
    created_at: _timestamp_pb2.Timestamp
    revoked_at: _timestamp_pb2.Timestamp
    subject_kind: str
    display_name: str
    auth_source: str
    caller_plugin_name: str
    run_as: WorkflowRunAsSubject
    source_definition_id: str
    def __init__(self, id: _Optional[str] = ..., provider_name: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., subject_id: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., permissions: _Optional[_Iterable[_Union[WorkflowAccessPermission, _Mapping]]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., revoked_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., subject_kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ..., caller_plugin_name: _Optional[str] = ..., run_as: _Optional[_Union[WorkflowRunAsSubject, _Mapping]] = ..., source_definition_id: _Optional[str] = ...) -> None: ...

class WorkflowSignal(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    payload: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    created_by: WorkflowActor
    created_at: _timestamp_pb2.Timestamp
    idempotency_key: str
    sequence: int
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., payload: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., sequence: _Optional[int] = ...) -> None: ...

class StartWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    TARGET_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    target: BoundWorkflowTarget
    idempotency_key: str
    created_by: WorkflowActor
    execution_ref: str
    workflow_key: str
    def __init__(self, target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class GetWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    def __init__(self, run_id: _Optional[str] = ...) -> None: ...

class ListWorkflowProviderRunsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListWorkflowProviderRunsResponse(_message.Message):
    __slots__ = ()
    RUNS_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[BoundWorkflowRun]
    def __init__(self, runs: _Optional[_Iterable[_Union[BoundWorkflowRun, _Mapping]]] = ...) -> None: ...

class CancelWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    reason: str
    def __init__(self, run_id: _Optional[str] = ..., reason: _Optional[str] = ...) -> None: ...

class SignalWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    signal: WorkflowSignal
    def __init__(self, run_id: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ...) -> None: ...

class SignalOrStartWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    workflow_key: str
    target: BoundWorkflowTarget
    idempotency_key: str
    created_by: WorkflowActor
    execution_ref: str
    signal: WorkflowSignal
    def __init__(self, workflow_key: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ...) -> None: ...

class SignalWorkflowRunResponse(_message.Message):
    __slots__ = ()
    RUN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STARTED_RUN_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    run: BoundWorkflowRun
    signal: WorkflowSignal
    started_run: bool
    workflow_key: str
    def __init__(self, run: _Optional[_Union[BoundWorkflowRun, _Mapping]] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., started_run: _Optional[bool] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class UpsertWorkflowProviderScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    CRON_FIELD_NUMBER: _ClassVar[int]
    TIMEZONE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    cron: str
    timezone: str
    target: BoundWorkflowTarget
    paused: bool
    requested_by: WorkflowActor
    execution_ref: str
    def __init__(self, schedule_id: _Optional[str] = ..., cron: _Optional[str] = ..., timezone: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., requested_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ...) -> None: ...

class GetWorkflowProviderScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    def __init__(self, schedule_id: _Optional[str] = ...) -> None: ...

class ListWorkflowProviderSchedulesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListWorkflowProviderSchedulesResponse(_message.Message):
    __slots__ = ()
    SCHEDULES_FIELD_NUMBER: _ClassVar[int]
    schedules: _containers.RepeatedCompositeFieldContainer[BoundWorkflowSchedule]
    def __init__(self, schedules: _Optional[_Iterable[_Union[BoundWorkflowSchedule, _Mapping]]] = ...) -> None: ...

class DeleteWorkflowProviderScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    def __init__(self, schedule_id: _Optional[str] = ...) -> None: ...

class PauseWorkflowProviderScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    def __init__(self, schedule_id: _Optional[str] = ...) -> None: ...

class ResumeWorkflowProviderScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    def __init__(self, schedule_id: _Optional[str] = ...) -> None: ...

class UpsertWorkflowProviderEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    match: WorkflowEventMatch
    target: BoundWorkflowTarget
    paused: bool
    requested_by: WorkflowActor
    execution_ref: str
    def __init__(self, trigger_id: _Optional[str] = ..., match: _Optional[_Union[WorkflowEventMatch, _Mapping]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., requested_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ...) -> None: ...

class GetWorkflowProviderEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    def __init__(self, trigger_id: _Optional[str] = ...) -> None: ...

class ListWorkflowProviderEventTriggersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListWorkflowProviderEventTriggersResponse(_message.Message):
    __slots__ = ()
    TRIGGERS_FIELD_NUMBER: _ClassVar[int]
    triggers: _containers.RepeatedCompositeFieldContainer[BoundWorkflowEventTrigger]
    def __init__(self, triggers: _Optional[_Iterable[_Union[BoundWorkflowEventTrigger, _Mapping]]] = ...) -> None: ...

class PutWorkflowExecutionReferenceRequest(_message.Message):
    __slots__ = ()
    REFERENCE_FIELD_NUMBER: _ClassVar[int]
    reference: WorkflowExecutionReference
    def __init__(self, reference: _Optional[_Union[WorkflowExecutionReference, _Mapping]] = ...) -> None: ...

class GetWorkflowExecutionReferenceRequest(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class ListWorkflowExecutionReferencesRequest(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    def __init__(self, subject_id: _Optional[str] = ...) -> None: ...

class ListWorkflowExecutionReferencesResponse(_message.Message):
    __slots__ = ()
    REFERENCES_FIELD_NUMBER: _ClassVar[int]
    references: _containers.RepeatedCompositeFieldContainer[WorkflowExecutionReference]
    def __init__(self, references: _Optional[_Iterable[_Union[WorkflowExecutionReference, _Mapping]]] = ...) -> None: ...

class DeleteWorkflowProviderEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    def __init__(self, trigger_id: _Optional[str] = ...) -> None: ...

class PauseWorkflowProviderEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    def __init__(self, trigger_id: _Optional[str] = ...) -> None: ...

class ResumeWorkflowProviderEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    def __init__(self, trigger_id: _Optional[str] = ...) -> None: ...

class PublishWorkflowProviderEventRequest(_message.Message):
    __slots__ = ()
    PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    PUBLISHED_BY_FIELD_NUMBER: _ClassVar[int]
    plugin_name: str
    event: WorkflowEvent
    published_by: WorkflowActor
    def __init__(self, plugin_name: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ..., published_by: _Optional[_Union[WorkflowActor, _Mapping]] = ...) -> None: ...

class InvokeWorkflowOperationRequest(_message.Message):
    __slots__ = ()
    TARGET_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    SIGNALS_FIELD_NUMBER: _ClassVar[int]
    target: BoundWorkflowTarget
    run_id: str
    trigger: WorkflowRunTrigger
    input: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    created_by: WorkflowActor
    execution_ref: str
    signals: _containers.RepeatedCompositeFieldContainer[WorkflowSignal]
    def __init__(self, target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., run_id: _Optional[str] = ..., trigger: _Optional[_Union[WorkflowRunTrigger, _Mapping]] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., execution_ref: _Optional[str] = ..., signals: _Optional[_Iterable[_Union[WorkflowSignal, _Mapping]]] = ...) -> None: ...

class InvokeWorkflowOperationResponse(_message.Message):
    __slots__ = ()
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    status: int
    body: str
    def __init__(self, status: _Optional[int] = ..., body: _Optional[str] = ...) -> None: ...

class ManagedWorkflowSchedule(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    schedule: BoundWorkflowSchedule
    def __init__(self, provider_name: _Optional[str] = ..., schedule: _Optional[_Union[BoundWorkflowSchedule, _Mapping]] = ...) -> None: ...

class ManagedWorkflowEventTrigger(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    trigger: BoundWorkflowEventTrigger
    def __init__(self, provider_name: _Optional[str] = ..., trigger: _Optional[_Union[BoundWorkflowEventTrigger, _Mapping]] = ...) -> None: ...

class ManagedWorkflowDefinition(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    definition: BoundWorkflowDefinition
    def __init__(self, provider_name: _Optional[str] = ..., definition: _Optional[_Union[BoundWorkflowDefinition, _Mapping]] = ...) -> None: ...

class ManagedWorkflowRun(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    run: BoundWorkflowRun
    def __init__(self, provider_name: _Optional[str] = ..., run: _Optional[_Union[BoundWorkflowRun, _Mapping]] = ...) -> None: ...

class ManagedWorkflowRunSignal(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STARTED_RUN_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    run: BoundWorkflowRun
    signal: WorkflowSignal
    started_run: bool
    workflow_key: str
    def __init__(self, provider_name: _Optional[str] = ..., run: _Optional[_Union[BoundWorkflowRun, _Mapping]] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., started_run: _Optional[bool] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class WorkflowManagerCreateScheduleRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    CRON_FIELD_NUMBER: _ClassVar[int]
    TIMEZONE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    cron: str
    timezone: str
    target: BoundWorkflowTarget
    paused: bool
    invocation_token: str
    idempotency_key: str
    definition_id: str
    def __init__(self, provider_name: _Optional[str] = ..., cron: _Optional[str] = ..., timezone: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerGetScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    invocation_token: str
    def __init__(self, schedule_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerUpdateScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    CRON_FIELD_NUMBER: _ClassVar[int]
    TIMEZONE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    provider_name: str
    cron: str
    timezone: str
    target: BoundWorkflowTarget
    paused: bool
    invocation_token: str
    definition_id: str
    def __init__(self, schedule_id: _Optional[str] = ..., provider_name: _Optional[str] = ..., cron: _Optional[str] = ..., timezone: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerDeleteScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    invocation_token: str
    def __init__(self, schedule_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerPauseScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    invocation_token: str
    def __init__(self, schedule_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerResumeScheduleRequest(_message.Message):
    __slots__ = ()
    SCHEDULE_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    schedule_id: str
    invocation_token: str
    def __init__(self, schedule_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerCreateEventTriggerRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    match: WorkflowEventMatch
    target: BoundWorkflowTarget
    paused: bool
    invocation_token: str
    idempotency_key: str
    definition_id: str
    def __init__(self, provider_name: _Optional[str] = ..., match: _Optional[_Union[WorkflowEventMatch, _Mapping]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerGetEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    invocation_token: str
    def __init__(self, trigger_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerUpdateEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    provider_name: str
    match: WorkflowEventMatch
    target: BoundWorkflowTarget
    paused: bool
    invocation_token: str
    definition_id: str
    def __init__(self, trigger_id: _Optional[str] = ..., provider_name: _Optional[str] = ..., match: _Optional[_Union[WorkflowEventMatch, _Mapping]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerDeleteEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    invocation_token: str
    def __init__(self, trigger_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerPauseEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    invocation_token: str
    def __init__(self, trigger_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerResumeEventTriggerRequest(_message.Message):
    __slots__ = ()
    TRIGGER_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    trigger_id: str
    invocation_token: str
    def __init__(self, trigger_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerPublishEventRequest(_message.Message):
    __slots__ = ()
    EVENT_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    event: WorkflowEvent
    invocation_token: str
    provider_name: str
    def __init__(self, event: _Optional[_Union[WorkflowEvent, _Mapping]] = ..., invocation_token: _Optional[str] = ..., provider_name: _Optional[str] = ...) -> None: ...

class WorkflowManagerStartRunRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    target: BoundWorkflowTarget
    idempotency_key: str
    workflow_key: str
    invocation_token: str
    definition_id: str
    def __init__(self, provider_name: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., workflow_key: _Optional[str] = ..., invocation_token: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerSignalRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    signal: WorkflowSignal
    invocation_token: str
    def __init__(self, run_id: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerSignalOrStartRunRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    workflow_key: str
    target: BoundWorkflowTarget
    idempotency_key: str
    signal: WorkflowSignal
    invocation_token: str
    definition_id: str
    def __init__(self, provider_name: _Optional[str] = ..., workflow_key: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., invocation_token: _Optional[str] = ..., definition_id: _Optional[str] = ...) -> None: ...

class WorkflowManagerCreateDefinitionRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    target: BoundWorkflowTarget
    invocation_token: str
    idempotency_key: str
    def __init__(self, provider_name: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class WorkflowManagerGetDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerUpdateDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    provider_name: str
    target: BoundWorkflowTarget
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., provider_name: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerDeleteDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...
