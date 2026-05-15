import datetime

from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import plugin_pb2 as _plugin_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentMessagePartType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_MESSAGE_PART_TYPE_UNSPECIFIED: _ClassVar[AgentMessagePartType]
    AGENT_MESSAGE_PART_TYPE_TEXT: _ClassVar[AgentMessagePartType]
    AGENT_MESSAGE_PART_TYPE_JSON: _ClassVar[AgentMessagePartType]
    AGENT_MESSAGE_PART_TYPE_TOOL_CALL: _ClassVar[AgentMessagePartType]
    AGENT_MESSAGE_PART_TYPE_TOOL_RESULT: _ClassVar[AgentMessagePartType]
    AGENT_MESSAGE_PART_TYPE_IMAGE_REF: _ClassVar[AgentMessagePartType]

class AgentToolSourceMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_TOOL_SOURCE_MODE_UNSPECIFIED: _ClassVar[AgentToolSourceMode]
    AGENT_TOOL_SOURCE_MODE_MCP_CATALOG: _ClassVar[AgentToolSourceMode]
    AGENT_TOOL_SOURCE_MODE_NONE: _ClassVar[AgentToolSourceMode]

class AgentExecutionStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_EXECUTION_STATUS_UNSPECIFIED: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_PENDING: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_RUNNING: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_SUCCEEDED: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_FAILED: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_CANCELED: _ClassVar[AgentExecutionStatus]
    AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT: _ClassVar[AgentExecutionStatus]

class AgentSessionState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_SESSION_STATE_UNSPECIFIED: _ClassVar[AgentSessionState]
    AGENT_SESSION_STATE_ACTIVE: _ClassVar[AgentSessionState]
    AGENT_SESSION_STATE_ARCHIVED: _ClassVar[AgentSessionState]

class AgentInteractionType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_INTERACTION_TYPE_UNSPECIFIED: _ClassVar[AgentInteractionType]
    AGENT_INTERACTION_TYPE_APPROVAL: _ClassVar[AgentInteractionType]
    AGENT_INTERACTION_TYPE_CLARIFICATION: _ClassVar[AgentInteractionType]
    AGENT_INTERACTION_TYPE_INPUT: _ClassVar[AgentInteractionType]

class AgentInteractionState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_INTERACTION_STATE_UNSPECIFIED: _ClassVar[AgentInteractionState]
    AGENT_INTERACTION_STATE_PENDING: _ClassVar[AgentInteractionState]
    AGENT_INTERACTION_STATE_RESOLVED: _ClassVar[AgentInteractionState]
    AGENT_INTERACTION_STATE_CANCELED: _ClassVar[AgentInteractionState]
AGENT_MESSAGE_PART_TYPE_UNSPECIFIED: AgentMessagePartType
AGENT_MESSAGE_PART_TYPE_TEXT: AgentMessagePartType
AGENT_MESSAGE_PART_TYPE_JSON: AgentMessagePartType
AGENT_MESSAGE_PART_TYPE_TOOL_CALL: AgentMessagePartType
AGENT_MESSAGE_PART_TYPE_TOOL_RESULT: AgentMessagePartType
AGENT_MESSAGE_PART_TYPE_IMAGE_REF: AgentMessagePartType
AGENT_TOOL_SOURCE_MODE_UNSPECIFIED: AgentToolSourceMode
AGENT_TOOL_SOURCE_MODE_MCP_CATALOG: AgentToolSourceMode
AGENT_TOOL_SOURCE_MODE_NONE: AgentToolSourceMode
AGENT_EXECUTION_STATUS_UNSPECIFIED: AgentExecutionStatus
AGENT_EXECUTION_STATUS_PENDING: AgentExecutionStatus
AGENT_EXECUTION_STATUS_RUNNING: AgentExecutionStatus
AGENT_EXECUTION_STATUS_SUCCEEDED: AgentExecutionStatus
AGENT_EXECUTION_STATUS_FAILED: AgentExecutionStatus
AGENT_EXECUTION_STATUS_CANCELED: AgentExecutionStatus
AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT: AgentExecutionStatus
AGENT_SESSION_STATE_UNSPECIFIED: AgentSessionState
AGENT_SESSION_STATE_ACTIVE: AgentSessionState
AGENT_SESSION_STATE_ARCHIVED: AgentSessionState
AGENT_INTERACTION_TYPE_UNSPECIFIED: AgentInteractionType
AGENT_INTERACTION_TYPE_APPROVAL: AgentInteractionType
AGENT_INTERACTION_TYPE_CLARIFICATION: AgentInteractionType
AGENT_INTERACTION_TYPE_INPUT: AgentInteractionType
AGENT_INTERACTION_STATE_UNSPECIFIED: AgentInteractionState
AGENT_INTERACTION_STATE_PENDING: AgentInteractionState
AGENT_INTERACTION_STATE_RESOLVED: AgentInteractionState
AGENT_INTERACTION_STATE_CANCELED: AgentInteractionState

class AgentMessage(_message.Message):
    __slots__ = ()
    ROLE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    PARTS_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    role: str
    text: str
    parts: _containers.RepeatedCompositeFieldContainer[AgentMessagePart]
    metadata: _struct_pb2.Struct
    def __init__(self, role: _Optional[str] = ..., text: _Optional[str] = ..., parts: _Optional[_Iterable[_Union[AgentMessagePart, _Mapping]]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentMessagePartToolCall(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_ID_FIELD_NUMBER: _ClassVar[int]
    ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
    id: str
    tool_id: str
    arguments: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., tool_id: _Optional[str] = ..., arguments: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentMessagePartToolResult(_message.Message):
    __slots__ = ()
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    tool_call_id: str
    status: int
    content: str
    output: _struct_pb2.Struct
    def __init__(self, tool_call_id: _Optional[str] = ..., status: _Optional[int] = ..., content: _Optional[str] = ..., output: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentMessagePartImageRef(_message.Message):
    __slots__ = ()
    URI_FIELD_NUMBER: _ClassVar[int]
    MIME_TYPE_FIELD_NUMBER: _ClassVar[int]
    uri: str
    mime_type: str
    def __init__(self, uri: _Optional[str] = ..., mime_type: _Optional[str] = ...) -> None: ...

class AgentMessagePart(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    JSON_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_FIELD_NUMBER: _ClassVar[int]
    TOOL_RESULT_FIELD_NUMBER: _ClassVar[int]
    IMAGE_REF_FIELD_NUMBER: _ClassVar[int]
    type: AgentMessagePartType
    text: str
    json: _struct_pb2.Struct
    tool_call: AgentMessagePartToolCall
    tool_result: AgentMessagePartToolResult
    image_ref: AgentMessagePartImageRef
    def __init__(self, type: _Optional[_Union[AgentMessagePartType, str]] = ..., text: _Optional[str] = ..., json: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., tool_call: _Optional[_Union[AgentMessagePartToolCall, _Mapping]] = ..., tool_result: _Optional[_Union[AgentMessagePartToolResult, _Mapping]] = ..., image_ref: _Optional[_Union[AgentMessagePartImageRef, _Mapping]] = ...) -> None: ...

class AgentActor(_message.Message):
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

class AgentSubjectContext(_message.Message):
    __slots__ = ()
    SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_KIND_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTH_SOURCE_FIELD_NUMBER: _ClassVar[int]
    subject_id: str
    subject_kind: str
    credential_subject_id: str
    display_name: str
    auth_source: str
    def __init__(self, subject_id: _Optional[str] = ..., subject_kind: _Optional[str] = ..., credential_subject_id: _Optional[str] = ..., display_name: _Optional[str] = ..., auth_source: _Optional[str] = ...) -> None: ...

class AgentWorkspace(_message.Message):
    __slots__ = ()
    CHECKOUTS_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    checkouts: _containers.RepeatedCompositeFieldContainer[AgentWorkspaceGitCheckout]
    cwd: str
    def __init__(self, checkouts: _Optional[_Iterable[_Union[AgentWorkspaceGitCheckout, _Mapping]]] = ..., cwd: _Optional[str] = ...) -> None: ...

class AgentWorkspaceGitCheckout(_message.Message):
    __slots__ = ()
    URL_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    url: str
    ref: str
    path: str
    def __init__(self, url: _Optional[str] = ..., ref: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class PreparedAgentWorkspace(_message.Message):
    __slots__ = ()
    ROOT_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    root: str
    cwd: str
    def __init__(self, root: _Optional[str] = ..., cwd: _Optional[str] = ...) -> None: ...

class ResolvedAgentTool(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    description: str
    parameters_schema: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., description: _Optional[str] = ..., parameters_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentToolRef(_message.Message):
    __slots__ = ()
    PLUGIN_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    SYSTEM_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_FIELD_NUMBER: _ClassVar[int]
    RUN_AS_EXTERNAL_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    plugin: str
    operation: str
    connection: str
    instance: str
    title: str
    description: str
    system: str
    run_as: AgentSubjectContext
    run_as_external_identity: _plugin_pb2.ExternalIdentityContext
    def __init__(self, plugin: _Optional[str] = ..., operation: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., title: _Optional[str] = ..., description: _Optional[str] = ..., system: _Optional[str] = ..., run_as: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., run_as_external_identity: _Optional[_Union[_plugin_pb2.ExternalIdentityContext, _Mapping]] = ...) -> None: ...

class AgentProviderCapabilities(_message.Message):
    __slots__ = ()
    STREAMING_TEXT_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    PARALLEL_TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    INTERACTIONS_FIELD_NUMBER: _ClassVar[int]
    RESUMABLE_TURNS_FIELD_NUMBER: _ClassVar[int]
    REASONING_SUMMARIES_FIELD_NUMBER: _ClassVar[int]
    BOUNDED_LIST_HYDRATION_FIELD_NUMBER: _ClassVar[int]
    SUPPORTED_TOOL_SOURCES_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_SESSION_START_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_PREPARED_WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    streaming_text: bool
    tool_calls: bool
    parallel_tool_calls: bool
    structured_output: bool
    interactions: bool
    resumable_turns: bool
    reasoning_summaries: bool
    bounded_list_hydration: bool
    supported_tool_sources: _containers.RepeatedScalarFieldContainer[AgentToolSourceMode]
    supports_session_start: bool
    supports_prepared_workspace: bool
    def __init__(self, streaming_text: _Optional[bool] = ..., tool_calls: _Optional[bool] = ..., parallel_tool_calls: _Optional[bool] = ..., structured_output: _Optional[bool] = ..., interactions: _Optional[bool] = ..., resumable_turns: _Optional[bool] = ..., reasoning_summaries: _Optional[bool] = ..., bounded_list_hydration: _Optional[bool] = ..., supported_tool_sources: _Optional[_Iterable[_Union[AgentToolSourceMode, str]]] = ..., supports_session_start: _Optional[bool] = ..., supports_prepared_workspace: _Optional[bool] = ...) -> None: ...

class GetAgentProviderCapabilitiesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AgentInteraction(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    REQUEST_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_AT_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    type: AgentInteractionType
    state: AgentInteractionState
    title: str
    prompt: str
    request: _struct_pb2.Struct
    resolution: _struct_pb2.Struct
    created_at: _timestamp_pb2.Timestamp
    resolved_at: _timestamp_pb2.Timestamp
    turn_id: str
    session_id: str
    def __init__(self, id: _Optional[str] = ..., type: _Optional[_Union[AgentInteractionType, str]] = ..., state: _Optional[_Union[AgentInteractionState, str]] = ..., title: _Optional[str] = ..., prompt: _Optional[str] = ..., request: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., resolution: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., resolved_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., turn_id: _Optional[str] = ..., session_id: _Optional[str] = ...) -> None: ...

class AgentSession(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    CLIENT_REF_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_TURN_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    provider_name: str
    model: str
    client_ref: str
    state: AgentSessionState
    metadata: _struct_pb2.Struct
    created_by: AgentActor
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    last_turn_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., provider_name: _Optional[str] = ..., model: _Optional[str] = ..., client_ref: _Optional[str] = ..., state: _Optional[_Union[AgentSessionState, str]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[AgentActor, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_turn_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class CreateAgentProviderSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    CLIENT_REF_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    SESSION_START_FIELD_NUMBER: _ClassVar[int]
    PREPARED_WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    idempotency_key: str
    model: str
    client_ref: str
    metadata: _struct_pb2.Struct
    created_by: AgentActor
    subject: AgentSubjectContext
    session_start: AgentSessionStartConfig
    prepared_workspace: PreparedAgentWorkspace
    def __init__(self, session_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., model: _Optional[str] = ..., client_ref: _Optional[str] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[AgentActor, _Mapping]] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., session_start: _Optional[_Union[AgentSessionStartConfig, _Mapping]] = ..., prepared_workspace: _Optional[_Union[PreparedAgentWorkspace, _Mapping]] = ...) -> None: ...

class AgentSessionStartConfig(_message.Message):
    __slots__ = ()
    HOOKS_FIELD_NUMBER: _ClassVar[int]
    hooks: _containers.RepeatedCompositeFieldContainer[AgentSessionStartHook]
    def __init__(self, hooks: _Optional[_Iterable[_Union[AgentSessionStartHook, _Mapping]]] = ...) -> None: ...

class AgentSessionStartHook(_message.Message):
    __slots__ = ()
    class EnvEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    id: str
    type: str
    command: _containers.RepeatedScalarFieldContainer[str]
    cwd: str
    timeout: str
    env: _containers.ScalarMap[str, str]
    output: AgentSessionStartHookOutput
    def __init__(self, id: _Optional[str] = ..., type: _Optional[str] = ..., command: _Optional[_Iterable[str]] = ..., cwd: _Optional[str] = ..., timeout: _Optional[str] = ..., env: _Optional[_Mapping[str, str]] = ..., output: _Optional[_Union[AgentSessionStartHookOutput, _Mapping]] = ...) -> None: ...

class AgentSessionStartHookOutput(_message.Message):
    __slots__ = ()
    ADDITIONAL_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    additional_context: bool
    metadata: bool
    def __init__(self, additional_context: _Optional[bool] = ..., metadata: _Optional[bool] = ...) -> None: ...

class GetAgentProviderSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    subject: AgentSubjectContext
    def __init__(self, session_id: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ListAgentProviderSessionsRequest(_message.Message):
    __slots__ = ()
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    SESSION_IDS_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_ONLY_FIELD_NUMBER: _ClassVar[int]
    subject: AgentSubjectContext
    session_ids: _containers.RepeatedScalarFieldContainer[str]
    state: AgentSessionState
    limit: int
    summary_only: bool
    def __init__(self, subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., session_ids: _Optional[_Iterable[str]] = ..., state: _Optional[_Union[AgentSessionState, str]] = ..., limit: _Optional[int] = ..., summary_only: _Optional[bool] = ...) -> None: ...

class ListAgentProviderSessionsResponse(_message.Message):
    __slots__ = ()
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[AgentSession]
    def __init__(self, sessions: _Optional[_Iterable[_Union[AgentSession, _Mapping]]] = ...) -> None: ...

class UpdateAgentProviderSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_REF_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    client_ref: str
    state: AgentSessionState
    metadata: _struct_pb2.Struct
    subject: AgentSubjectContext
    def __init__(self, session_id: _Optional[str] = ..., client_ref: _Optional[str] = ..., state: _Optional[_Union[AgentSessionState, str]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class AgentTurn(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TEXT_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    id: str
    session_id: str
    provider_name: str
    model: str
    status: AgentExecutionStatus
    messages: _containers.RepeatedCompositeFieldContainer[AgentMessage]
    output_text: str
    structured_output: _struct_pb2.Struct
    status_message: str
    created_by: AgentActor
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    execution_ref: str
    def __init__(self, id: _Optional[str] = ..., session_id: _Optional[str] = ..., provider_name: _Optional[str] = ..., model: _Optional[str] = ..., status: _Optional[_Union[AgentExecutionStatus, str]] = ..., messages: _Optional[_Iterable[_Union[AgentMessage, _Mapping]]] = ..., output_text: _Optional[str] = ..., structured_output: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., status_message: _Optional[str] = ..., created_by: _Optional[_Union[AgentActor, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., execution_ref: _Optional[str] = ...) -> None: ...

class AgentTurnDisplay(_message.Message):
    __slots__ = ()
    KIND_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    PARENT_REF_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    kind: str
    phase: str
    text: str
    label: str
    ref: str
    parent_ref: str
    input: _struct_pb2.Value
    output: _struct_pb2.Value
    error: _struct_pb2.Value
    action: str
    format: str
    language: str
    def __init__(self, kind: _Optional[str] = ..., phase: _Optional[str] = ..., text: _Optional[str] = ..., label: _Optional[str] = ..., ref: _Optional[str] = ..., parent_ref: _Optional[str] = ..., input: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., error: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., action: _Optional[str] = ..., format: _Optional[str] = ..., language: _Optional[str] = ...) -> None: ...

class CreateAgentProviderTurnRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    TOOL_SOURCE_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    RUN_GRANT_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    session_id: str
    idempotency_key: str
    model: str
    messages: _containers.RepeatedCompositeFieldContainer[AgentMessage]
    tools: _containers.RepeatedCompositeFieldContainer[ResolvedAgentTool]
    response_schema: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    created_by: AgentActor
    execution_ref: str
    tool_refs: _containers.RepeatedCompositeFieldContainer[AgentToolRef]
    tool_source: AgentToolSourceMode
    subject: AgentSubjectContext
    model_options: _struct_pb2.Struct
    run_grant: str
    def __init__(self, turn_id: _Optional[str] = ..., session_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., model: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[AgentMessage, _Mapping]]] = ..., tools: _Optional[_Iterable[_Union[ResolvedAgentTool, _Mapping]]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_by: _Optional[_Union[AgentActor, _Mapping]] = ..., execution_ref: _Optional[str] = ..., tool_refs: _Optional[_Iterable[_Union[AgentToolRef, _Mapping]]] = ..., tool_source: _Optional[_Union[AgentToolSourceMode, str]] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., run_grant: _Optional[str] = ...) -> None: ...

class GetAgentProviderTurnRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    subject: AgentSubjectContext
    def __init__(self, turn_id: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ListAgentProviderTurnsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    TURN_IDS_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_ONLY_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    subject: AgentSubjectContext
    turn_ids: _containers.RepeatedScalarFieldContainer[str]
    status: AgentExecutionStatus
    limit: int
    summary_only: bool
    def __init__(self, session_id: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ..., turn_ids: _Optional[_Iterable[str]] = ..., status: _Optional[_Union[AgentExecutionStatus, str]] = ..., limit: _Optional[int] = ..., summary_only: _Optional[bool] = ...) -> None: ...

class ListAgentProviderTurnsResponse(_message.Message):
    __slots__ = ()
    TURNS_FIELD_NUMBER: _ClassVar[int]
    turns: _containers.RepeatedCompositeFieldContainer[AgentTurn]
    def __init__(self, turns: _Optional[_Iterable[_Union[AgentTurn, _Mapping]]] = ...) -> None: ...

class CancelAgentProviderTurnRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    reason: str
    subject: AgentSubjectContext
    def __init__(self, turn_id: _Optional[str] = ..., reason: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class AgentTurnEvent(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    SEQ_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    VISIBILITY_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_FIELD_NUMBER: _ClassVar[int]
    id: str
    turn_id: str
    seq: int
    type: str
    source: str
    visibility: str
    data: _struct_pb2.Struct
    created_at: _timestamp_pb2.Timestamp
    display: AgentTurnDisplay
    def __init__(self, id: _Optional[str] = ..., turn_id: _Optional[str] = ..., seq: _Optional[int] = ..., type: _Optional[str] = ..., source: _Optional[str] = ..., visibility: _Optional[str] = ..., data: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., display: _Optional[_Union[AgentTurnDisplay, _Mapping]] = ...) -> None: ...

class ListAgentProviderTurnEventsRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_SEQ_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    after_seq: int
    limit: int
    subject: AgentSubjectContext
    def __init__(self, turn_id: _Optional[str] = ..., after_seq: _Optional[int] = ..., limit: _Optional[int] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ListAgentProviderTurnEventsResponse(_message.Message):
    __slots__ = ()
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[AgentTurnEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[AgentTurnEvent, _Mapping]]] = ...) -> None: ...

class GetAgentProviderInteractionRequest(_message.Message):
    __slots__ = ()
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    interaction_id: str
    subject: AgentSubjectContext
    def __init__(self, interaction_id: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ListAgentProviderInteractionsRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    subject: AgentSubjectContext
    def __init__(self, turn_id: _Optional[str] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ListAgentProviderInteractionsResponse(_message.Message):
    __slots__ = ()
    INTERACTIONS_FIELD_NUMBER: _ClassVar[int]
    interactions: _containers.RepeatedCompositeFieldContainer[AgentInteraction]
    def __init__(self, interactions: _Optional[_Iterable[_Union[AgentInteraction, _Mapping]]] = ...) -> None: ...

class ResolveAgentProviderInteractionRequest(_message.Message):
    __slots__ = ()
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    interaction_id: str
    resolution: _struct_pb2.Struct
    subject: AgentSubjectContext
    def __init__(self, interaction_id: _Optional[str] = ..., resolution: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., subject: _Optional[_Union[AgentSubjectContext, _Mapping]] = ...) -> None: ...

class ExecuteAgentToolRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_ID_FIELD_NUMBER: _ClassVar[int]
    ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    RUN_GRANT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    turn_id: str
    tool_call_id: str
    tool_id: str
    arguments: _struct_pb2.Struct
    idempotency_key: str
    run_grant: str
    def __init__(self, session_id: _Optional[str] = ..., turn_id: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., tool_id: _Optional[str] = ..., arguments: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., run_grant: _Optional[str] = ...) -> None: ...

class ExecuteAgentToolResponse(_message.Message):
    __slots__ = ()
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    status: int
    body: str
    def __init__(self, status: _Optional[int] = ..., body: _Optional[str] = ...) -> None: ...

class ListedAgentTool(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    MCP_NAME_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    ANNOTATIONS_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    SEARCH_TEXT_FIELD_NUMBER: _ClassVar[int]
    id: str
    mcp_name: str
    title: str
    description: str
    input_schema: str
    output_schema: str
    annotations: _plugin_pb2.OperationAnnotations
    ref: AgentToolRef
    tags: _containers.RepeatedScalarFieldContainer[str]
    search_text: str
    def __init__(self, id: _Optional[str] = ..., mcp_name: _Optional[str] = ..., title: _Optional[str] = ..., description: _Optional[str] = ..., input_schema: _Optional[str] = ..., output_schema: _Optional[str] = ..., annotations: _Optional[_Union[_plugin_pb2.OperationAnnotations, _Mapping]] = ..., ref: _Optional[_Union[AgentToolRef, _Mapping]] = ..., tags: _Optional[_Iterable[str]] = ..., search_text: _Optional[str] = ...) -> None: ...

class ListAgentToolsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    RUN_GRANT_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    turn_id: str
    page_size: int
    page_token: str
    run_grant: str
    query: str
    def __init__(self, session_id: _Optional[str] = ..., turn_id: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., run_grant: _Optional[str] = ..., query: _Optional[str] = ...) -> None: ...

class ListAgentToolsResponse(_message.Message):
    __slots__ = ()
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    tools: _containers.RepeatedCompositeFieldContainer[ListedAgentTool]
    next_page_token: str
    def __init__(self, tools: _Optional[_Iterable[_Union[ListedAgentTool, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class ResolveAgentConnectionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    RUN_GRANT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    turn_id: str
    connection: str
    instance: str
    run_grant: str
    def __init__(self, session_id: _Optional[str] = ..., turn_id: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., run_grant: _Optional[str] = ...) -> None: ...

class ResolvedAgentConnection(_message.Message):
    __slots__ = ()
    class HeadersEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    class ParamsEntry(_message.Message):
        __slots__ = ()
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    CONNECTION_ID_FIELD_NUMBER: _ClassVar[int]
    CONNECTION_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    connection_id: str
    connection: str
    instance: str
    mode: str
    headers: _containers.ScalarMap[str, str]
    params: _containers.ScalarMap[str, str]
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, connection_id: _Optional[str] = ..., connection: _Optional[str] = ..., instance: _Optional[str] = ..., mode: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., params: _Optional[_Mapping[str, str]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AgentManagerCreateSessionRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    CLIENT_REF_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    model: str
    client_ref: str
    metadata: _struct_pb2.Struct
    idempotency_key: str
    invocation_token: str
    workspace: AgentWorkspace
    def __init__(self, provider_name: _Optional[str] = ..., model: _Optional[str] = ..., client_ref: _Optional[str] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., invocation_token: _Optional[str] = ..., workspace: _Optional[_Union[AgentWorkspace, _Mapping]] = ...) -> None: ...

class AgentManagerGetSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    invocation_token: str
    def __init__(self, session_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerListSessionsRequest(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_ONLY_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    invocation_token: str
    state: AgentSessionState
    limit: int
    summary_only: bool
    def __init__(self, provider_name: _Optional[str] = ..., invocation_token: _Optional[str] = ..., state: _Optional[_Union[AgentSessionState, str]] = ..., limit: _Optional[int] = ..., summary_only: _Optional[bool] = ...) -> None: ...

class AgentManagerListSessionsResponse(_message.Message):
    __slots__ = ()
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[AgentSession]
    def __init__(self, sessions: _Optional[_Iterable[_Union[AgentSession, _Mapping]]] = ...) -> None: ...

class AgentManagerUpdateSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_REF_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    client_ref: str
    state: AgentSessionState
    metadata: _struct_pb2.Struct
    invocation_token: str
    def __init__(self, session_id: _Optional[str] = ..., client_ref: _Optional[str] = ..., state: _Optional[_Union[AgentSessionState, str]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerCreateTurnRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    TOOL_SOURCE_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    MODEL_OPTIONS_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    model: str
    messages: _containers.RepeatedCompositeFieldContainer[AgentMessage]
    tool_refs: _containers.RepeatedCompositeFieldContainer[AgentToolRef]
    tool_source: AgentToolSourceMode
    response_schema: _struct_pb2.Struct
    metadata: _struct_pb2.Struct
    idempotency_key: str
    invocation_token: str
    model_options: _struct_pb2.Struct
    def __init__(self, session_id: _Optional[str] = ..., model: _Optional[str] = ..., messages: _Optional[_Iterable[_Union[AgentMessage, _Mapping]]] = ..., tool_refs: _Optional[_Iterable[_Union[AgentToolRef, _Mapping]]] = ..., tool_source: _Optional[_Union[AgentToolSourceMode, str]] = ..., response_schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., invocation_token: _Optional[str] = ..., model_options: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentManagerGetTurnRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    invocation_token: str
    def __init__(self, turn_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerListTurnsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_ONLY_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    invocation_token: str
    status: AgentExecutionStatus
    limit: int
    summary_only: bool
    def __init__(self, session_id: _Optional[str] = ..., invocation_token: _Optional[str] = ..., status: _Optional[_Union[AgentExecutionStatus, str]] = ..., limit: _Optional[int] = ..., summary_only: _Optional[bool] = ...) -> None: ...

class AgentManagerListTurnsResponse(_message.Message):
    __slots__ = ()
    TURNS_FIELD_NUMBER: _ClassVar[int]
    turns: _containers.RepeatedCompositeFieldContainer[AgentTurn]
    def __init__(self, turns: _Optional[_Iterable[_Union[AgentTurn, _Mapping]]] = ...) -> None: ...

class AgentManagerCancelTurnRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    reason: str
    invocation_token: str
    def __init__(self, turn_id: _Optional[str] = ..., reason: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerListTurnEventsRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_SEQ_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    after_seq: int
    limit: int
    invocation_token: str
    def __init__(self, turn_id: _Optional[str] = ..., after_seq: _Optional[int] = ..., limit: _Optional[int] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerListTurnEventsResponse(_message.Message):
    __slots__ = ()
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[AgentTurnEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[AgentTurnEvent, _Mapping]]] = ...) -> None: ...

class AgentManagerListInteractionsRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    invocation_token: str
    def __init__(self, turn_id: _Optional[str] = ..., invocation_token: _Optional[str] = ...) -> None: ...

class AgentManagerListInteractionsResponse(_message.Message):
    __slots__ = ()
    INTERACTIONS_FIELD_NUMBER: _ClassVar[int]
    interactions: _containers.RepeatedCompositeFieldContainer[AgentInteraction]
    def __init__(self, interactions: _Optional[_Iterable[_Union[AgentInteraction, _Mapping]]] = ...) -> None: ...

class AgentManagerResolveInteractionRequest(_message.Message):
    __slots__ = ()
    TURN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_TOKEN_FIELD_NUMBER: _ClassVar[int]
    turn_id: str
    interaction_id: str
    resolution: _struct_pb2.Struct
    invocation_token: str
    def __init__(self, turn_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., resolution: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., invocation_token: _Optional[str] = ...) -> None: ...
