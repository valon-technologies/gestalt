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

class WorkflowActivationMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_ACTIVATION_MODE_UNSPECIFIED: _ClassVar[WorkflowActivationMode]
    WORKFLOW_ACTIVATION_MODE_START: _ClassVar[WorkflowActivationMode]
    WORKFLOW_ACTIVATION_MODE_SIGNAL: _ClassVar[WorkflowActivationMode]
    WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START: _ClassVar[WorkflowActivationMode]

class WorkflowActionKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_ACTION_KIND_UNSPECIFIED: _ClassVar[WorkflowActionKind]
    WORKFLOW_ACTION_KIND_PLUGIN: _ClassVar[WorkflowActionKind]
    WORKFLOW_ACTION_KIND_AGENT_TURN: _ClassVar[WorkflowActionKind]
    WORKFLOW_ACTION_KIND_DELIVERY: _ClassVar[WorkflowActionKind]

class WorkflowDefinitionStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_DEFINITION_STATUS_UNSPECIFIED: _ClassVar[WorkflowDefinitionStatus]
    WORKFLOW_DEFINITION_STATUS_PENDING: _ClassVar[WorkflowDefinitionStatus]
    WORKFLOW_DEFINITION_STATUS_ACTIVE: _ClassVar[WorkflowDefinitionStatus]
    WORKFLOW_DEFINITION_STATUS_PAUSED: _ClassVar[WorkflowDefinitionStatus]
    WORKFLOW_DEFINITION_STATUS_DELETED: _ClassVar[WorkflowDefinitionStatus]
    WORKFLOW_DEFINITION_STATUS_FAILED: _ClassVar[WorkflowDefinitionStatus]

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
    WORKFLOW_STEP_STATUS_SUCCEEDED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_FAILED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_SKIPPED: _ClassVar[WorkflowStepStatus]
    WORKFLOW_STEP_STATUS_CANCELED: _ClassVar[WorkflowStepStatus]

class WorkflowRunEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_RUN_STARTED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_RUN_COMPLETED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_RUN_FAILED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_RUN_CANCELED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_SIGNAL_RECEIVED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_STEP_STARTED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_STEP_SUCCEEDED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_STEP_FAILED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_STEP_SKIPPED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_ACTION_INVOKED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_ACTION_COMPLETED: _ClassVar[WorkflowRunEventType]
    WORKFLOW_RUN_EVENT_TYPE_ACTION_FAILED: _ClassVar[WorkflowRunEventType]
WORKFLOW_ACTIVATION_MODE_UNSPECIFIED: WorkflowActivationMode
WORKFLOW_ACTIVATION_MODE_START: WorkflowActivationMode
WORKFLOW_ACTIVATION_MODE_SIGNAL: WorkflowActivationMode
WORKFLOW_ACTIVATION_MODE_SIGNAL_OR_START: WorkflowActivationMode
WORKFLOW_ACTION_KIND_UNSPECIFIED: WorkflowActionKind
WORKFLOW_ACTION_KIND_PLUGIN: WorkflowActionKind
WORKFLOW_ACTION_KIND_AGENT_TURN: WorkflowActionKind
WORKFLOW_ACTION_KIND_DELIVERY: WorkflowActionKind
WORKFLOW_DEFINITION_STATUS_UNSPECIFIED: WorkflowDefinitionStatus
WORKFLOW_DEFINITION_STATUS_PENDING: WorkflowDefinitionStatus
WORKFLOW_DEFINITION_STATUS_ACTIVE: WorkflowDefinitionStatus
WORKFLOW_DEFINITION_STATUS_PAUSED: WorkflowDefinitionStatus
WORKFLOW_DEFINITION_STATUS_DELETED: WorkflowDefinitionStatus
WORKFLOW_DEFINITION_STATUS_FAILED: WorkflowDefinitionStatus
WORKFLOW_RUN_STATUS_UNSPECIFIED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_PENDING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_RUNNING: WorkflowRunStatus
WORKFLOW_RUN_STATUS_SUCCEEDED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_FAILED: WorkflowRunStatus
WORKFLOW_RUN_STATUS_CANCELED: WorkflowRunStatus
WORKFLOW_STEP_STATUS_UNSPECIFIED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_PENDING: WorkflowStepStatus
WORKFLOW_STEP_STATUS_RUNNING: WorkflowStepStatus
WORKFLOW_STEP_STATUS_SUCCEEDED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_FAILED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_SKIPPED: WorkflowStepStatus
WORKFLOW_STEP_STATUS_CANCELED: WorkflowStepStatus
WORKFLOW_RUN_EVENT_TYPE_UNSPECIFIED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_RUN_STARTED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_RUN_COMPLETED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_RUN_FAILED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_RUN_CANCELED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_SIGNAL_RECEIVED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_STEP_STARTED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_STEP_SUCCEEDED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_STEP_FAILED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_STEP_SKIPPED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_ACTION_INVOKED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_ACTION_COMPLETED: WorkflowRunEventType
WORKFLOW_RUN_EVENT_TYPE_ACTION_FAILED: WorkflowRunEventType

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
    OUTPUT_DELIVERY_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    id: str
    inputs: _containers.MessageMap[str, WorkflowValue]
    when: WorkflowStepWhen
    timeout_seconds: int
    output_delivery: WorkflowStepDelivery
    metadata: _struct_pb2.Struct
    plugin: WorkflowStepPluginCall
    agent: WorkflowStepAgentTurn
    def __init__(self, id: _Optional[str] = ..., inputs: _Optional[_Mapping[str, WorkflowValue]] = ..., when: _Optional[_Union[WorkflowStepWhen, _Mapping]] = ..., timeout_seconds: _Optional[int] = ..., output_delivery: _Optional[_Union[WorkflowStepDelivery, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., plugin: _Optional[_Union[WorkflowStepPluginCall, _Mapping]] = ..., agent: _Optional[_Union[WorkflowStepAgentTurn, _Mapping]] = ...) -> None: ...

class WorkflowStepPluginCall(_message.Message):
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

class WorkflowStepDelivery(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    plugin: WorkflowStepPluginCall
    def __init__(self, plugin: _Optional[_Union[WorkflowStepPluginCall, _Mapping]] = ...) -> None: ...

class WorkflowStepAgentTurn(_message.Message):
    __slots__ = ()
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    SESSION_KEY_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    model: str
    session_key: WorkflowText
    prompt: WorkflowText
    messages: _containers.RepeatedCompositeFieldContainer[WorkflowAgentMessage]
    tools: _containers.RepeatedCompositeFieldContainer[_plugin_pb2.AgentToolRef]
    response_schema: _struct_pb2.Struct
    model_options: _struct_pb2.Struct
    def __init__(self, provider: _Optional[str] = ..., model: _Optional[str] = ..., session_key: _Optional[_Union[WorkflowText, _Mapping]] = ..., prompt: _Optional[_Union[WorkflowText, _Mapping]] = ..., messages: _Optional[_Iterable[_Union[WorkflowAgentMessage, _Mapping]]] = ..., tools: _Optional[_Iterable[_Union[_plugin_pb2.AgentToolRef, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

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
    RUN_INPUT_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    STEP_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    literal: _struct_pb2.Value
    object: WorkflowObject
    array: WorkflowArray
    template: WorkflowText
    run_input: WorkflowPathSource
    signal_payload: WorkflowPathSource
    step_output: WorkflowStepOutputSource
    def __init__(self, literal: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., object: _Optional[_Union[WorkflowObject, _Mapping]] = ..., array: _Optional[_Union[WorkflowArray, _Mapping]] = ..., template: _Optional[_Union[WorkflowText, _Mapping]] = ..., run_input: _Optional[_Union[WorkflowPathSource, _Mapping]] = ..., signal_payload: _Optional[_Union[WorkflowPathSource, _Mapping]] = ..., step_output: _Optional[_Union[WorkflowStepOutputSource, _Mapping]] = ...) -> None: ...

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
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    display_name: str
    auth_source: str
    credential_subject_id: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ..., credential_subject_id: _Optional[str] = ...) -> None: ...

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

class WorkflowManualActivation(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

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
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    RUN_KEY_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    MANUAL_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    id: str
    paused: bool
    mode: WorkflowActivationMode
    input: WorkflowValue
    run_key: WorkflowValue
    idempotency_key: WorkflowValue
    manual: WorkflowManualActivation
    schedule: WorkflowScheduleActivation
    event: WorkflowEventActivation
    def __init__(self, id: _Optional[str] = ..., paused: _Optional[bool] = ..., mode: _Optional[_Union[WorkflowActivationMode, str]] = ..., input: _Optional[_Union[WorkflowValue, _Mapping]] = ..., run_key: _Optional[_Union[WorkflowValue, _Mapping]] = ..., idempotency_key: _Optional[_Union[WorkflowValue, _Mapping]] = ..., manual: _Optional[_Union[WorkflowManualActivation, _Mapping]] = ..., schedule: _Optional[_Union[WorkflowScheduleActivation, _Mapping]] = ..., event: _Optional[_Union[WorkflowEventActivation, _Mapping]] = ...) -> None: ...

class WorkflowAccessPermission(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATIONS_FIELD_NUMBER: _ClassVar[int]
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operations: _containers.RepeatedScalarFieldContainer[str]
    actions: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, plugin: _Optional[str] = ..., operations: _Optional[_Iterable[str]] = ..., actions: _Optional[_Iterable[str]] = ...) -> None: ...

class WorkflowExecutionReference(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    CALLER_PLUGIN_NAME_FIELD_NUMBER: _ClassVar[int]
    SOURCE_DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    SOURCE_DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REVOKED_AT_FIELD_NUMBER: _ClassVar[int]
    TARGET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    ACTION_TABLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_DIGEST_FIELD_NUMBER: _ClassVar[int]
    SEMANTICS_VERSION_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    id: str
    provider_name: str
    target: BoundWorkflowTarget
    caller_plugin_name: str
    source_definition_id: str
    source_definition_generation: int
    subject_id: str
    subject_kind: str
    display_name: str
    auth_source: str
    credential_subject_id: str
    run_as: WorkflowRunAsSubject
    permissions: _containers.RepeatedCompositeFieldContainer[WorkflowAccessPermission]
    created_at: _timestamp_pb2.Timestamp
    revoked_at: _timestamp_pb2.Timestamp
    target_digest: str
    action_table_digest: str
    permissions_digest: str
    semantics_version: str
    generation: int
    def __init__(self, id: _Optional[str] = ..., provider_name: _Optional[str] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., caller_plugin_name: _Optional[str] = ..., source_definition_id: _Optional[str] = ..., source_definition_generation: _Optional[int] = ..., subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., run_as: _Optional[_Union[WorkflowRunAsSubject, _Mapping]] = ..., permissions: _Optional[_Iterable[_Union[WorkflowAccessPermission, _Mapping]]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., revoked_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., target_digest: _Optional[str] = ..., action_table_digest: _Optional[str] = ..., permissions_digest: _Optional[str] = ..., semantics_version: _Optional[str] = ..., generation: _Optional[int] = ...) -> None: ...

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
    EXECUTION_REFS_FIELD_NUMBER: _ClassVar[int]
    execution_refs: _containers.RepeatedCompositeFieldContainer[WorkflowExecutionReference]
    def __init__(self, execution_refs: _Optional[_Iterable[_Union[WorkflowExecutionReference, _Mapping]]] = ...) -> None: ...

class WorkflowDefinitionSpec(_message.Message):
    __slots__ = ()
    class LabelsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    ACTIVATIONS_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_SEMANTICS_VERSION_FIELD_NUMBER: _ClassVar[int]
    id: str
    generation: int
    target: BoundWorkflowTarget
    activations: _containers.RepeatedCompositeFieldContainer[WorkflowActivation]
    paused: bool
    run_as: WorkflowRunAsSubject
    permissions: _containers.RepeatedCompositeFieldContainer[WorkflowAccessPermission]
    labels: _containers.ScalarMap[str, str]
    workflow_semantics_version: str
    def __init__(self, id: _Optional[str] = ..., generation: _Optional[int] = ..., target: _Optional[_Union[BoundWorkflowTarget, _Mapping]] = ..., activations: _Optional[_Iterable[_Union[WorkflowActivation, _Mapping]]] = ..., paused: _Optional[bool] = ..., run_as: _Optional[_Union[WorkflowRunAsSubject, _Mapping]] = ..., permissions: _Optional[_Iterable[_Union[WorkflowAccessPermission, _Mapping]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., workflow_semantics_version: _Optional[str] = ...) -> None: ...

class WorkflowActionDescriptor(_message.Message):
    __slots__ = ()
    ACTION_ID_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    action_id: str
    step_id: str
    kind: WorkflowActionKind
    plugin: WorkflowStepPluginCall
    agent: WorkflowStepAgentTurn
    def __init__(self, action_id: _Optional[str] = ..., step_id: _Optional[str] = ..., kind: _Optional[_Union[WorkflowActionKind, str]] = ..., plugin: _Optional[_Union[WorkflowStepPluginCall, _Mapping]] = ..., agent: _Optional[_Union[WorkflowStepAgentTurn, _Mapping]] = ...) -> None: ...

class WorkflowActionTable(_message.Message):
    __slots__ = ()
    ACTIONS_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    actions: _containers.RepeatedCompositeFieldContainer[WorkflowActionDescriptor]
    digest: str
    def __init__(self, actions: _Optional[_Iterable[_Union[WorkflowActionDescriptor, _Mapping]]] = ..., digest: _Optional[str] = ...) -> None: ...

class WorkflowDefinitionBinding(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_GENERATION_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    SPEC_DIGEST_FIELD_NUMBER: _ClassVar[int]
    TARGET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    ACTION_TABLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_DIGEST_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_SEMANTICS_VERSION_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    execution_ref: str
    execution_ref_generation: int
    definition_id: str
    definition_generation: int
    spec_digest: str
    target_digest: str
    action_table_digest: str
    permissions_digest: str
    workflow_semantics_version: str
    request_id: str
    def __init__(self, id: _Optional[str] = ..., execution_ref: _Optional[str] = ..., execution_ref_generation: _Optional[int] = ..., definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., spec_digest: _Optional[str] = ..., target_digest: _Optional[str] = ..., action_table_digest: _Optional[str] = ..., permissions_digest: _Optional[str] = ..., workflow_semantics_version: _Optional[str] = ..., request_id: _Optional[str] = ...) -> None: ...

class WorkflowDefinition(_message.Message):
    __slots__ = ()
    SPEC_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    APPLIED_GENERATION_FIELD_NUMBER: _ClassVar[int]
    SPEC_DIGEST_FIELD_NUMBER: _ClassVar[int]
    TARGET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    ACTION_TABLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_PLAN_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_PLAN_DIGEST_FIELD_NUMBER: _ClassVar[int]
    BINDING_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    spec: WorkflowDefinitionSpec
    status: WorkflowDefinitionStatus
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    applied_generation: int
    spec_digest: str
    target_digest: str
    action_table_digest: str
    provider_plan_id: str
    provider_plan_digest: str
    binding: WorkflowDefinitionBinding
    error: WorkflowRunError
    def __init__(self, spec: _Optional[_Union[WorkflowDefinitionSpec, _Mapping]] = ..., status: _Optional[_Union[WorkflowDefinitionStatus, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., applied_generation: _Optional[int] = ..., spec_digest: _Optional[str] = ..., target_digest: _Optional[str] = ..., action_table_digest: _Optional[str] = ..., provider_plan_id: _Optional[str] = ..., provider_plan_digest: _Optional[str] = ..., binding: _Optional[_Union[WorkflowDefinitionBinding, _Mapping]] = ..., error: _Optional[_Union[WorkflowRunError, _Mapping]] = ...) -> None: ...

class ApplyWorkflowDefinitionRequest(_message.Message):
    __slots__ = ()
    SPEC_FIELD_NUMBER: _ClassVar[int]
    BINDING_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    spec: WorkflowDefinitionSpec
    binding: WorkflowDefinitionBinding
    execution_ref: WorkflowExecutionReference
    request_id: str
    def __init__(self, spec: _Optional[_Union[WorkflowDefinitionSpec, _Mapping]] = ..., binding: _Optional[_Union[WorkflowDefinitionBinding, _Mapping]] = ..., execution_ref: _Optional[_Union[WorkflowExecutionReference, _Mapping]] = ..., request_id: _Optional[str] = ...) -> None: ...

class GetWorkflowDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    def __init__(self, definition_id: _Optional[str] = ...) -> None: ...

class ListWorkflowDefinitionsRequest(_message.Message):
    __slots__ = ()
    class LabelsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    page_size: int
    page_token: str
    labels: _containers.ScalarMap[str, str]
    def __init__(self, page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ListWorkflowDefinitionsResponse(_message.Message):
    __slots__ = ()
    DEFINITIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definitions: _containers.RepeatedCompositeFieldContainer[WorkflowDefinition]
    next_page_token: str
    def __init__(self, definitions: _Optional[_Iterable[_Union[WorkflowDefinition, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class DeleteWorkflowDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    generation: int
    request_id: str
    def __init__(self, definition_id: _Optional[str] = ..., generation: _Optional[int] = ..., request_id: _Optional[str] = ...) -> None: ...

class SetWorkflowDefinitionPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    paused: bool
    request_id: str
    def __init__(self, definition_id: _Optional[str] = ..., paused: _Optional[bool] = ..., request_id: _Optional[str] = ...) -> None: ...

class SetWorkflowActivationPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    activation_id: str
    paused: bool
    request_id: str
    def __init__(self, definition_id: _Optional[str] = ..., activation_id: _Optional[str] = ..., paused: _Optional[bool] = ..., request_id: _Optional[str] = ...) -> None: ...

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

class WorkflowEventTrigger(_message.Message):
    __slots__ = ()
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    activation_id: str
    event: WorkflowEvent
    def __init__(self, activation_id: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ...) -> None: ...

class WorkflowRunTrigger(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    MANUAL_FIELD_NUMBER: _ClassVar[int]
    SCHEDULE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    definition_generation: int
    activation_id: str
    manual: WorkflowManualTrigger
    schedule: WorkflowScheduleTrigger
    event: WorkflowEventTrigger
    def __init__(self, definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., activation_id: _Optional[str] = ..., manual: _Optional[_Union[WorkflowManualTrigger, _Mapping]] = ..., schedule: _Optional[_Union[WorkflowScheduleTrigger, _Mapping]] = ..., event: _Optional[_Union[WorkflowEventTrigger, _Mapping]] = ...) -> None: ...

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

class WorkflowOutputSummary(_message.Message):
    __slots__ = ()
    ENVELOPE_VERSION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    REDACTED_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    envelope_version: str
    kind: str
    size_bytes: int
    sha256: str
    truncated: bool
    redacted: bool
    media_type: str
    def __init__(self, envelope_version: _Optional[str] = ..., kind: _Optional[str] = ..., size_bytes: _Optional[int] = ..., sha256: _Optional[str] = ..., truncated: _Optional[bool] = ..., redacted: _Optional[bool] = ..., media_type: _Optional[str] = ...) -> None: ...

class WorkflowRunError(_message.Message):
    __slots__ = ()
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    ACTION_ID_FIELD_NUMBER: _ClassVar[int]
    code: str
    message: str
    step_id: str
    action_id: str
    def __init__(self, code: _Optional[str] = ..., message: _Optional[str] = ..., step_id: _Optional[str] = ..., action_id: _Optional[str] = ...) -> None: ...

class WorkflowStepState(_message.Message):
    __slots__ = ()
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    STEP_INDEX_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    SKIPPED_REASON_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_NUMBER_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_REF_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    step_id: str
    step_index: int
    status: WorkflowStepStatus
    skipped_reason: str
    attempt_number: int
    output_summary: WorkflowOutputSummary
    output_ref: str
    error: WorkflowRunError
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, step_id: _Optional[str] = ..., step_index: _Optional[int] = ..., status: _Optional[_Union[WorkflowStepStatus, str]] = ..., skipped_reason: _Optional[str] = ..., attempt_number: _Optional[int] = ..., output_summary: _Optional[_Union[WorkflowOutputSummary, _Mapping]] = ..., output_ref: _Optional[str] = ..., error: _Optional[_Union[WorkflowRunError, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkflowRun(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_GENERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    SPEC_DIGEST_FIELD_NUMBER: _ClassVar[int]
    ACTION_TABLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    STEPS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    id: str
    definition_id: str
    definition_generation: int
    workflow_key: str
    status: WorkflowRunStatus
    trigger: WorkflowRunTrigger
    input: _struct_pb2.Struct
    created_by: WorkflowActor
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    status_message: str
    execution_ref: str
    execution_ref_generation: int
    target_digest: str
    spec_digest: str
    action_table_digest: str
    steps: _containers.RepeatedCompositeFieldContainer[WorkflowStepState]
    error: WorkflowRunError
    def __init__(self, id: _Optional[str] = ..., definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., workflow_key: _Optional[str] = ..., status: _Optional[_Union[WorkflowRunStatus, str]] = ..., trigger: _Optional[_Union[WorkflowRunTrigger, _Mapping]] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., status_message: _Optional[str] = ..., execution_ref: _Optional[str] = ..., execution_ref_generation: _Optional[int] = ..., target_digest: _Optional[str] = ..., spec_digest: _Optional[str] = ..., action_table_digest: _Optional[str] = ..., steps: _Optional[_Iterable[_Union[WorkflowStepState, _Mapping]]] = ..., error: _Optional[_Union[WorkflowRunError, _Mapping]] = ...) -> None: ...

class StartWorkflowRunRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    definition_generation: int
    activation_id: str
    workflow_key: str
    input: _struct_pb2.Struct
    idempotency_key: str
    created_by: WorkflowActor
    def __init__(self, definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., activation_id: _Optional[str] = ..., workflow_key: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ...) -> None: ...

class SignalWorkflowRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    signal: WorkflowSignal
    def __init__(self, run_id: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ...) -> None: ...

class SignalOrStartWorkflowRunRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    definition_generation: int
    activation_id: str
    workflow_key: str
    input: _struct_pb2.Struct
    idempotency_key: str
    signal: WorkflowSignal
    created_by: WorkflowActor
    def __init__(self, definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., activation_id: _Optional[str] = ..., workflow_key: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., created_by: _Optional[_Union[WorkflowActor, _Mapping]] = ...) -> None: ...

class CancelWorkflowRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    reason: str
    def __init__(self, run_id: _Optional[str] = ..., reason: _Optional[str] = ...) -> None: ...

class GetWorkflowRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    def __init__(self, run_id: _Optional[str] = ...) -> None: ...

class ListWorkflowRunsRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    page_size: int
    page_token: str
    status: WorkflowRunStatus
    def __init__(self, definition_id: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., status: _Optional[_Union[WorkflowRunStatus, str]] = ...) -> None: ...

class ListWorkflowRunsResponse(_message.Message):
    __slots__ = ()
    RUNS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[WorkflowRun]
    next_page_token: str
    def __init__(self, runs: _Optional[_Iterable[_Union[WorkflowRun, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class WorkflowRunSignal(_message.Message):
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

class DeliverWorkflowEventRequest(_message.Message):
    __slots__ = ()
    DELIVERY_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    PUBLISHED_BY_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    delivery_id: str
    event: WorkflowEvent
    published_by: WorkflowActor
    idempotency_key: str
    def __init__(self, delivery_id: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ..., published_by: _Optional[_Union[WorkflowActor, _Mapping]] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class WorkflowEventDeliveryResult(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STARTED_RUN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    activation_id: str
    run: WorkflowRun
    signal: WorkflowSignal
    started_run: bool
    def __init__(self, definition_id: _Optional[str] = ..., activation_id: _Optional[str] = ..., run: _Optional[_Union[WorkflowRun, _Mapping]] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., started_run: _Optional[bool] = ...) -> None: ...

class DeliverWorkflowEventResponse(_message.Message):
    __slots__ = ()
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    results: _containers.RepeatedCompositeFieldContainer[WorkflowEventDeliveryResult]
    def __init__(self, results: _Optional[_Iterable[_Union[WorkflowEventDeliveryResult, _Mapping]]] = ...) -> None: ...

class WorkflowRunEvent(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    ACTION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_NUMBER_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_REF_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    run_id: str
    sequence: int
    type: WorkflowRunEventType
    step_id: str
    action_id: str
    attempt_number: int
    message: str
    output_summary: WorkflowOutputSummary
    output_ref: str
    error: WorkflowRunError
    observed_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., run_id: _Optional[str] = ..., sequence: _Optional[int] = ..., type: _Optional[_Union[WorkflowRunEventType, str]] = ..., step_id: _Optional[str] = ..., action_id: _Optional[str] = ..., attempt_number: _Optional[int] = ..., message: _Optional[str] = ..., output_summary: _Optional[_Union[WorkflowOutputSummary, _Mapping]] = ..., output_ref: _Optional[str] = ..., error: _Optional[_Union[WorkflowRunError, _Mapping]] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetWorkflowRunEventsRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    page_size: int
    page_token: str
    def __init__(self, run_id: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class ListWorkflowRunEventsResponse(_message.Message):
    __slots__ = ()
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[WorkflowRunEvent]
    next_page_token: str
    def __init__(self, events: _Optional[_Iterable[_Union[WorkflowRunEvent, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class GetWorkflowRunOutputRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_REF_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    output_ref: str
    step_id: str
    def __init__(self, run_id: _Optional[str] = ..., output_ref: _Optional[str] = ..., step_id: _Optional[str] = ...) -> None: ...

class WorkflowRunOutput(_message.Message):
    __slots__ = ()
    OUTPUT_REF_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    output_ref: str
    summary: WorkflowOutputSummary
    body: _struct_pb2.Value
    def __init__(self, output_ref: _Optional[str] = ..., summary: _Optional[_Union[WorkflowOutputSummary, _Mapping]] = ..., body: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class WorkflowHostActionSelector(_message.Message):
    __slots__ = ()
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_GENERATION_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    STEP_ID_FIELD_NUMBER: _ClassVar[int]
    ACTION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_NUMBER_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    execution_ref: str
    execution_ref_generation: int
    run_id: str
    definition_id: str
    definition_generation: int
    step_id: str
    action_id: str
    attempt_number: int
    idempotency_key: str
    def __init__(self, execution_ref: _Optional[str] = ..., execution_ref_generation: _Optional[int] = ..., run_id: _Optional[str] = ..., definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., step_id: _Optional[str] = ..., action_id: _Optional[str] = ..., attempt_number: _Optional[int] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class WorkflowPluginActionPayload(_message.Message):
    __slots__ = ()
    INPUT_FIELD_NUMBER: _ClassVar[int]
    input: _struct_pb2.Struct
    def __init__(self, input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class WorkflowAgentTurnPayload(_message.Message):
    __slots__ = ()
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    prompt: WorkflowText
    messages: _containers.RepeatedCompositeFieldContainer[WorkflowAgentMessage]
    def __init__(self, prompt: _Optional[_Union[WorkflowText, _Mapping]] = ..., messages: _Optional[_Iterable[_Union[WorkflowAgentMessage, _Mapping]]] = ...) -> None: ...

class InvokeWorkflowActionRequest(_message.Message):
    __slots__ = ()
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_FIELD_NUMBER: _ClassVar[int]
    SIGNALS_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    AGENT_TURN_FIELD_NUMBER: _ClassVar[int]
    selector: WorkflowHostActionSelector
    metadata: _struct_pb2.Struct
    trigger: WorkflowRunTrigger
    signals: _containers.RepeatedCompositeFieldContainer[WorkflowSignal]
    plugin: WorkflowPluginActionPayload
    agent_turn: WorkflowAgentTurnPayload
    def __init__(self, selector: _Optional[_Union[WorkflowHostActionSelector, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., trigger: _Optional[_Union[WorkflowRunTrigger, _Mapping]] = ..., signals: _Optional[_Iterable[_Union[WorkflowSignal, _Mapping]]] = ..., plugin: _Optional[_Union[WorkflowPluginActionPayload, _Mapping]] = ..., agent_turn: _Optional[_Union[WorkflowAgentTurnPayload, _Mapping]] = ...) -> None: ...

class WorkflowActionResult(_message.Message):
    __slots__ = ()
    ACTION_EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_REF_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    action_event_id: str
    status: int
    body: str
    output_summary: WorkflowOutputSummary
    output_ref: str
    error: WorkflowRunError
    def __init__(self, action_event_id: _Optional[str] = ..., status: _Optional[int] = ..., body: _Optional[str] = ..., output_summary: _Optional[_Union[WorkflowOutputSummary, _Mapping]] = ..., output_ref: _Optional[str] = ..., error: _Optional[_Union[WorkflowRunError, _Mapping]] = ...) -> None: ...

class ManagedWorkflowDefinition(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    definition: WorkflowDefinition
    def __init__(self, provider_name: _Optional[str] = ..., definition: _Optional[_Union[WorkflowDefinition, _Mapping]] = ...) -> None: ...

class ManagedWorkflowRun(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    run: WorkflowRun
    def __init__(self, provider_name: _Optional[str] = ..., run: _Optional[_Union[WorkflowRun, _Mapping]] = ...) -> None: ...

class ManagedWorkflowRunSignal(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    RUN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    STARTED_RUN_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    run: WorkflowRun
    signal: WorkflowSignal
    started_run: bool
    workflow_key: str
    def __init__(self, provider_name: _Optional[str] = ..., run: _Optional[_Union[WorkflowRun, _Mapping]] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., started_run: _Optional[bool] = ..., workflow_key: _Optional[str] = ...) -> None: ...

class WorkflowManagerApplyDefinitionRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    spec: WorkflowDefinitionSpec
    invocation_token: str
    idempotency_key: str
    def __init__(self, provider_name: _Optional[str] = ..., spec: _Optional[_Union[WorkflowDefinitionSpec, _Mapping]] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class WorkflowManagerGetDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerListDefinitionsRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    invocation_token: str
    def __init__(self, provider_name: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerListDefinitionsResponse(_message.Message):
    __slots__ = ()
    DEFINITIONS_FIELD_NUMBER: _ClassVar[int]
    definitions: _containers.RepeatedCompositeFieldContainer[ManagedWorkflowDefinition]
    def __init__(self, definitions: _Optional[_Iterable[_Union[ManagedWorkflowDefinition, _Mapping]]] = ...) -> None: ...

class WorkflowManagerDeleteDefinitionRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    generation: int
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., generation: _Optional[int] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerSetDefinitionPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    paused: bool
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerSetActivationPausedRequest(_message.Message):
    __slots__ = ()
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    PAUSED_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    definition_id: str
    activation_id: str
    paused: bool
    invocation_token: str
    def __init__(self, definition_id: _Optional[str] = ..., activation_id: _Optional[str] = ..., paused: _Optional[bool] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerStartRunRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    definition_id: str
    definition_generation: int
    activation_id: str
    workflow_key: str
    input: _struct_pb2.Struct
    idempotency_key: str
    invocation_token: str
    def __init__(self, provider_name: _Optional[str] = ..., definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., activation_id: _Optional[str] = ..., workflow_key: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

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
    DEFINITION_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    ACTIVATION_ID_FIELD_NUMBER: _ClassVar[int]
    WORKFLOW_KEY_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    definition_id: str
    definition_generation: int
    activation_id: str
    workflow_key: str
    input: _struct_pb2.Struct
    idempotency_key: str
    signal: WorkflowSignal
    invocation_token: str
    def __init__(self, provider_name: _Optional[str] = ..., definition_id: _Optional[str] = ..., definition_generation: _Optional[int] = ..., activation_id: _Optional[str] = ..., workflow_key: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., signal: _Optional[_Union[WorkflowSignal, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerCancelRunRequest(_message.Message):
    __slots__ = ()
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    reason: str
    invocation_token: str
    def __init__(self, run_id: _Optional[str] = ..., reason: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class WorkflowManagerDeliverEventRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    event: WorkflowEvent
    invocation_token: str
    idempotency_key: str
    def __init__(self, provider_name: _Optional[str] = ..., event: _Optional[_Union[WorkflowEvent, _Mapping]] = ..., invocation_token: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class WorkflowManagerDeliverEventResponse(_message.Message):
    __slots__ = ()
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    results: _containers.RepeatedCompositeFieldContainer[WorkflowEventDeliveryResult]
    def __init__(self, results: _Optional[_Iterable[_Union[WorkflowEventDeliveryResult, _Mapping]]] = ...) -> None: ...
