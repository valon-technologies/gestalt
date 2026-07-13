import datetime

from google.api import visibility_pb2 as _visibility_pb2
from google.protobuf import empty_pb2 as _empty_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import agent_pb2 as _agent_pb2
from . import annotations_pb2 as _annotations_pb2
from . import app_pb2 as _app_pb2
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

class WorkflowStepStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_STEP_STATUS_UNSPECIFIED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_PENDING: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_RUNNING: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_SKIPPED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_SUCCEEDED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_FAILED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_UNKNOWN: _ClassVar[WorkflowStepStatus]
WORKFLOW_RUN_STATUS_UNSPECIFIED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_PENDING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_RUNNING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_SUCCEEDED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_FAILED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_CANCELED: WorkflowRunStatus
WORKFLOW_STEP_STATUS_UNSPECIFIED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_PENDING: WorkflowStepStatus
WORKFLOW_STEP_STATUS_RUNNING: WorkflowStepStatus
WORKFLOW_STEP_STATUS_SKIPPED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_SUCCEEDED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_FAILED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_UNKNOWN: WorkflowStepStatus

class BoundWorkflowTarget(_message.Message):
    __slots__ = ()
    STEPS_FIELD_NUMBER: _ClassVar[int]
    steps: _containers.RepeatedCompositeFieldContainer[WorkflowStep]
    def __init__(self, steps: _Optional[_Iterable[_Union[WorkflowStep, _Mapping]]] = ...) -> None: ...

class WorkflowStep(_message.Message):
    __slots__ = ()
    class InputsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: WorkflowValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[WorkflowValue, _Mapping]] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    INPUTS_FIELD_NUMBER: _ClassVar[int]
    WHEN_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    APP_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    id: str
    inputs: _containers.MessageMap[str, WorkflowValue]
    when: WorkflowStepWhen
    timeout_seconds: int
    metadata: _struct_pb2.Struct
    app: WorkflowStepAppCall
    agent: WorkflowStepAgentTurn
    def __init__(self, id: _Optional[str] = ..., inputs: _Optional[_Mapping[str, WorkflowValue]] = ..., when: _Optional[_Union[WorkflowStepWhen, _Mapping]] = ..., timeout_seconds: _Optional[int] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., app: _Optional[_Union[WorkflowStepAppCall, _Mapping]] = ..., agent: _Optional[_Union[WorkflowStepAgentTurn, _Mapping]] = ...) -> None: ...

class WorkflowStepAppCall(_message.Message):
    __slots__ = ()
    NAME_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    name: str
    operation: str
    input: WorkflowValue
    connection: str
    instance: str
    credential_mode: str
    def __init__(self, name: _Optional[str] = ..., operation: _Optional[str] = ..., input: _Optional[_Union[WorkflowValue, _Mapping]] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., credential_mode: _Optional[str] = ...) -> None: ...

class WorkflowStepAgentTurn(_message.Message):
    __slots__ = ()
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    SESSION_KEY_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    model: str
    session_key: str
    prompt: WorkflowText
    messages: _containers.RepeatedCompositeFieldContainer[WorkflowAgentMessage]
    tools: _containers.RepeatedCompositeFieldContainer[_app_pb2.AgentToolRef]
    output: _agent_pb2.AgentOutput
    model_options: _struct_pb2.Struct
    def __init__(self, provider: _Optional[str] = ..., model: _Optional[str] = ..., session_key: _Optional[str] = ..., prompt: _Optional[_Union[WorkflowText, _Mapping]] = ..., messages: _Optional[_Iterable[_Union[WorkflowAgentMessage, _Mapping]]] = ..., tools: _Optional[_Iterable[_Union[_app_pb2.AgentToolRef, _Mapping]]] = ..., output: _Optional[_Union[_agent_pb2.AgentOutput, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class WorkflowAgentMessage(_message.Message):
    __slots__ = ()
    ROLE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    role: str
    text: WorkflowText
    metadata: _struct_pb2.Struct
    def __init__(self, role: _Optional[str] = ..., text: _Optional[_Union[WorkflowText, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class WorkflowText(_message.Message):
    __slots__ = ()
    TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    template: str
    def __init__(self, template: _Optional[str] = ...) -> None: ...

class WorkflowStepWhen(_message.Message):
    __slots__ = ()
    VALUE_FIELD_NUMBER: _ClassVar[int]
    EQUALS_FIELD_NUMBER: _ClassVar[int]
    value: WorkflowValue
    equals: _struct_pb2.Value
    def __init__(self, value: _Optional[_Union[WorkflowValue, _Mapping]] = ..., equals: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class WorkflowValue(_message.Message):
    __slots__ = ()
    LITERAL_FIELD_NUMBER: _ClassVar[int]
    OBJECT_FIELD_NUMBER: _ClassVar[int]
    ARRAY_FIELD_NUMBER: _ClassVar[int]
    TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STEP_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STEP_INPUT_FIELD_NUMBER: _ClassVar[int]
    literal: _struct_pb2.Value
    object: WorkflowObject
    array: WorkflowArray
    template: WorkflowText
    input: WorkflowPathSource
    signal: WorkflowPathSource
    step_output: WorkflowStepOutputSource
    step_input: WorkflowStepInputSource
    def __init__(self, literal: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., object: _Optional[_Union[WorkflowObject, _Mapping]] = ..., array: _Optional[_Union[WorkflowArray, _Mapping]] = ..., template: _Optional[_Union[WorkflowText, _Mapping]] = ..., input: _Optional[_Union[WorkflowPathSource, _Mapping]] = ..., signal: _Optional[_Union[WorkflowPathSource, _Mapping]] = ..., step_output: _Optional[_Union[WorkflowStepOutputSource, _Mapping]] = ..., step_input: _Optional[_Union[WorkflowStepInputSource, _Mapping]] = ...) -> None: ...

class WorkflowObject(_message.Message):
    __slots__ = ()
    class FieldsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: WorkflowValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[WorkflowValue, _Mapping]] = ...) -> None: ...
    FIELDS_FIELD_NUMBER: _ClassVar[int]
    fields: _containers.MessageMap[str, WorkflowValue]
    def __init__(self, fields: _Optional[_Mapping[str, WorkflowValue]] = ...) -> None: ...

class WorkflowArray(_message.Message):
    __slots__ = ()
    VALUES_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedCompositeFieldContainer[WorkflowValue]
    def __init__(self, values: _Optional[_Iterable[_Union[WorkflowValue, _Mapping]]] = ...) -> None: ...

class WorkflowPathSource(_message.Message):
    __slots__ = ()
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class WorkflowStepOutputSource(_message.Message):
    __slots__ = ()
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    path: str
    def __init__(self, step_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class WorkflowStepInputSource(_message.Message):
    __slots__ = ()
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    path: str
    def __init__(self, step_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

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

class WorkflowScheduleActivation(_message.Message):
    __slots__ = ()
    CRON_FIELD_NUMBER: _ClassVar[int]
    TIMEZONE_FIELD_NUMBER: _ClassVar[int]
    cron: str
    timezone: str
    def __init__(self, cron: _Optional[str] = ..., timezone: _Optional[str] = ...) -> None: ...

class WorkflowEventActivation(_message.Message):
    __slots__ = ()
    MATCH_FIELD_NUMBER: _ClassVar[int]
    match: WorkflowEventMatch
    def __init__(self, match: _Optional[_Union[WorkflowEventMatch, _Mapping]] = ...) -> None: ...

class WorkflowActivation(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    id: str
    input: WorkflowValue
    paused: bool
    schedule: WorkflowScheduleActivation
    event: WorkflowEventActivation
    def __init__(self, id: _Optional[str] = ..., input: _Optional[_Union[WorkflowValue, _Mapping]] = ..., paused: _Optional[bool] = ..., schedule: _Optional[_Union[WorkflowScheduleActivation, _Mapping]] = ..., event: _Optional[_Union[WorkflowEventActivation, _Mapping]] = ...) -> None: ...

class WorkflowDefinitionSpec(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    ACTIVATIONS_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    id: str
    target: BoundWorkflowTarget
    activations: _containers.RepeatedCompositeFieldContainer[WorkflowActivation]
    paused: bool
    run_as: str
    def __init__(self, id: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., activations: _Optional[_Iterable[_Union[WorkflowActivation, _Mapping]]] = ..., paused: _Optional[bool] = ..., run_as: _Optional[str] = ...) -> None: ...

class WorkflowDefinition(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    ACTIVATIONS_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    id: str
    generation: int
    target: BoundWorkflowTarget
    activations: _containers.RepeatedCompositeFieldContainer[WorkflowActivation]
    paused: bool
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    provider_name: str
    run_as: str
    def __init__(self, id: _Optional[str] = ..., generation: _Optional[int] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., activations: _Optional[_Iterable[_Union[WorkflowActivation, _Mapping]]] = ..., paused: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., provider_name: _Optional[str] = ..., run_as: _Optional[str] = ...) -> None: ...

class WorkflowManualTrigger(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class WorkflowScheduleTrigger(_message.Message):
    __slots__ = ()
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    SCHEDULED_FOR_FIELD_NUMBER: _ClassVar[int]
    activation_id: str
    scheduled_for: _timestamp_pb2.Timestamp
    def __init__(self, activation_id: _Optional[str] = ..., scheduled_for: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowEventTriggerInvocation(_message.Message):
    __slots__ = ()
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    activation_id: str
    event: WorkflowEvent
    def __init__(self, activation_id: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ...) -> None: ...

class WorkflowRunTrigger(_message.Message):
    __slots__ = ()
    MANUAL_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    manual: WorkflowManualTrigger
    schedule: WorkflowScheduleTrigger
    event: WorkflowEventTriggerInvocation
    def __init__(self, manual: _Optional[_Union[WorkflowManualTrigger, _Mapping]] = ..., schedule: _Optional[_Union[WorkflowScheduleTrigger, _Mapping]] = ..., event: _Optional[_Union[WorkflowEventTriggerInvocation, _Mapping]] = ...) -> None: ...

class WorkflowStepAttempt(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    status: WorkflowStepStatus
    idempotency_key: str
    input: _struct_pb2.Value
    output: _struct_pb2.Value
    status_message: str
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., status: _Optional[_Union[WorkflowStepStatus, str]] = ..., idempotency_key: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., status_message: _Optional[str] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowStepExecution(_message.Message):
    __slots__ = ()
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    SKIP_REASON_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    status: WorkflowStepStatus
    attempts: _containers.RepeatedCompositeFieldContainer[WorkflowStepAttempt]
    input: _struct_pb2.Value
    output: _struct_pb2.Value
    status_message: str
    skip_reason: str
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    def __init__(self, step_id: _Optional[str] = ..., status: _Optional[_Union[WorkflowStepStatus, str]] = ..., attempts: _Optional[_Iterable[_Union[WorkflowStepAttempt, _Mapping]]] = ..., input: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., status_message: _Optional[str] = ..., skip_reason: _Optional[str] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowRun(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    CURRENT_STEP_ID_FIELD_NUMBER: _ClassVar[int]
    STEPS_FIELD_NUMBER: _ClassVar[int]
    id: str
    status: WorkflowRunStatus
    target: BoundWorkflowTarget
    trigger: WorkflowRunTrigger
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    status_message: str
    output: _struct_pb2.Value
    workflow_key: str
    provider_name: str
    definition_id: str
    run_as: str
    input: _struct_pb2.Struct
    definition_generation: int
    current_step_id: str
    steps: _containers.RepeatedCompositeFieldContainer[WorkflowStepExecution]
    def __init__(self, id: _Optional[str] = ..., status: _Optional[_Union[WorkflowRunStatus, str]] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., trigger: _Optional[_Union[WorkflowRunTrigger, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., status_message: _Optional[str] = ..., output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., workflow_key: _Optional[str] = ..., provider_name: _Optional[str] = ..., definition_id: _Optional[str] = ..., run_as: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., definition_generation: _Optional[int] = ..., current_step_id: _Optional[str] = ..., steps: _Optional[_Iterable[_Union[WorkflowStepExecution, _Mapping]]] = ...) -> None: ...

class WorkflowSignal(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    payload: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    created_at: _timestamp_pb2.Timestamp
    idempotency_key: str
    sequence: int
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., payload: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., sequence: _Optional[int] = ...) -> None: ...

class ApplyWorkflowProviderDefinitionRequest(_message.Message):
    __slots__ = ()
    SPEC_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    spec: WorkflowDefinitionSpec
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, spec: _Optional[_Union[WorkflowDefinitionSpec, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetWorkflowProviderDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    context: _app_pb2.RequestContext
    def __init__(self, definition_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListWorkflowProviderDefinitionsRequest(_message.Message):
    __slots__ = ()
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    context: _app_pb2.RequestContext
    def __init__(self, context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListWorkflowProviderDefinitionsResponse(_message.Message):
    __slots__ = ()
    DEFINITIONS_FIELD_NUMBER: _ClassVar[int]
    definitions: _containers.RepeatedCompositeFieldContainer[WorkflowDefinition]
    def __init__(self, definitions: _Optional[_Iterable[_Union[WorkflowDefinition, _Mapping]]] = ...) -> None: ...

class SetWorkflowProviderDefinitionPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    paused: bool
    context: _app_pb2.RequestContext
    def __init__(self, definition_id: _Optional[str] = ..., paused: _Optional[bool] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class SetWorkflowProviderActivationPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    activation_id: str
    paused: bool
    context: _app_pb2.RequestContext
    def __init__(self, definition_id: _Optional[str] = ..., activation_id: _Optional[str] = ..., paused: _Optional[bool] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class DeleteWorkflowProviderDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    context: _app_pb2.RequestContext
    def __init__(self, definition_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class StartWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    idempotency_key: str
    workflow_key: str
    definition_id: str
    input: _struct_pb2.Struct
    expected_definition_generation: int
    context: _app_pb2.RequestContext
    def __init__(self, idempotency_key: _Optional[str] = ..., workflow_key: _Optional[str] = ..., definition_id: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., expected_definition_generation: _Optional[int] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    context: _app_pb2.RequestContext
    def __init__(self, run_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListWorkflowProviderRunsRequest(_message.Message):
    __slots__ = ()
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TARGET_APP_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    page_size: int
    page_token: str
    status: WorkflowRunStatus
    target_app: str
    context: _app_pb2.RequestContext
    def __init__(self, page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., status: _Optional[_Union[WorkflowRunStatus, str]] = ..., target_app: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListWorkflowProviderRunsResponse(_message.Message):
    __slots__ = ()
    RUNS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[WorkflowRun]
    next_page_token: str
    def __init__(self, runs: _Optional[_Iterable[_Union[WorkflowRun, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class CancelWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    reason: str
    context: _app_pb2.RequestContext
    def __init__(self, run_id: _Optional[str] = ..., reason: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class SignalWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    signal: WorkflowSignal
    context: _app_pb2.RequestContext
    def __init__(self, run_id: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class SignalOrStartWorkflowProviderRunRequest(_message.Message):
    __slots__ = ()
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    workflow_key: str
    idempotency_key: str
    signal: WorkflowSignal
    definition_id: str
    input: _struct_pb2.Struct
    expected_definition_generation: int
    context: _app_pb2.RequestContext
    def __init__(self, workflow_key: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., definition_id: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., expected_definition_generation: _Optional[int] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class SignalWorkflowRunResponse(_message.Message):
    __slots__ = ()
    RUN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STARTED_RUN_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    run: WorkflowRun
    signal: WorkflowSignal
    started_run: bool
    workflow_key: str
    def __init__(self, run: _Optional[_Union[WorkflowRun, _Mapping]] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., started_run: _Optional[bool] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class DeliverWorkflowProviderEventRequest(_message.Message):
    __slots__ = ()
    EVENT_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    event: WorkflowEvent
    context: _app_pb2.RequestContext
    def __init__(self, event: _Optional[_Union[WorkflowEvent, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class WorkflowRunEvent(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    run_id: str
    step_id: str
    type: str
    data: _struct_pb2.Struct
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., run_id: _Optional[str] = ..., step_id: _Optional[str] = ..., type: _Optional[str] = ..., data: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetWorkflowProviderRunEventsRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    context: _app_pb2.RequestContext
    def __init__(self, run_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetWorkflowProviderRunEventsResponse(_message.Message):
    __slots__ = ()
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[WorkflowRunEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[WorkflowRunEvent, _Mapping]]] = ...) -> None: ...

class GetWorkflowProviderRunOutputRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    context: _app_pb2.RequestContext
    def __init__(self, run_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetWorkflowProviderRunOutputResponse(_message.Message):
    __slots__ = ()
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    output: _struct_pb2.Value
    def __init__(self, output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...
