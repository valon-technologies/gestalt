import datetime

from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from . import agent_pb2 as _agent_pb2
from . import app_pb2 as _app_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentConfigCollectionUpdateMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_CONFIG_COLLECTION_UPDATE_MODE_UNSPECIFIED: _ClassVar[AgentConfigCollectionUpdateMode]
    AGENT_CONFIG_COLLECTION_UPDATE_MODE_REPLACE: _ClassVar[AgentConfigCollectionUpdateMode]
    AGENT_CONFIG_COLLECTION_UPDATE_MODE_PATCH: _ClassVar[AgentConfigCollectionUpdateMode]

class AgentRunEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_RUN_EVENT_TYPE_UNSPECIFIED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TURN_CREATED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TURN_STARTED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TEXT_DELTA: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TOOL_CALL_REQUESTED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TOOL_CALL_COMPLETED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_INTERACTION_REQUESTED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_INTERACTION_RESOLVED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_HISTORY_COMPACTED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TURN_COMPLETED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TURN_FAILED: _ClassVar[AgentRunEventType]
    AGENT_RUN_EVENT_TYPE_TURN_CANCELED: _ClassVar[AgentRunEventType]

class AgentInteractionKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_INTERACTION_KIND_UNSPECIFIED: _ClassVar[AgentInteractionKind]
    AGENT_INTERACTION_KIND_APPROVAL: _ClassVar[AgentInteractionKind]
    AGENT_INTERACTION_KIND_INPUT: _ClassVar[AgentInteractionKind]

class AgentInputType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_INPUT_TYPE_UNSPECIFIED: _ClassVar[AgentInputType]
    AGENT_INPUT_TYPE_TEXT: _ClassVar[AgentInputType]
    AGENT_INPUT_TYPE_CHOICE: _ClassVar[AgentInputType]
    AGENT_INPUT_TYPE_JSON: _ClassVar[AgentInputType]

class AgentApprovalDecision(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_APPROVAL_DECISION_UNSPECIFIED: _ClassVar[AgentApprovalDecision]
    AGENT_APPROVAL_DECISION_APPROVE: _ClassVar[AgentApprovalDecision]
    AGENT_APPROVAL_DECISION_DENY: _ClassVar[AgentApprovalDecision]

class AgentErrorCategory(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_ERROR_CATEGORY_UNSPECIFIED: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_INVALID_ARGUMENT: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_UNAUTHENTICATED: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_PERMISSION_DENIED: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_NOT_FOUND: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_CONFLICT: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_FAILED_PRECONDITION: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_UNAVAILABLE: _ClassVar[AgentErrorCategory]
    AGENT_ERROR_CATEGORY_DEADLINE_EXCEEDED: _ClassVar[AgentErrorCategory]
AGENT_CONFIG_COLLECTION_UPDATE_MODE_UNSPECIFIED: AgentConfigCollectionUpdateMode
AGENT_CONFIG_COLLECTION_UPDATE_MODE_REPLACE: AgentConfigCollectionUpdateMode
AGENT_CONFIG_COLLECTION_UPDATE_MODE_PATCH: AgentConfigCollectionUpdateMode
AGENT_RUN_EVENT_TYPE_UNSPECIFIED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TURN_CREATED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TURN_STARTED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TEXT_DELTA: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TOOL_CALL_REQUESTED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TOOL_CALL_COMPLETED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_INTERACTION_REQUESTED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_INTERACTION_RESOLVED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_HISTORY_COMPACTED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TURN_COMPLETED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TURN_FAILED: AgentRunEventType
AGENT_RUN_EVENT_TYPE_TURN_CANCELED: AgentRunEventType
AGENT_INTERACTION_KIND_UNSPECIFIED: AgentInteractionKind
AGENT_INTERACTION_KIND_APPROVAL: AgentInteractionKind
AGENT_INTERACTION_KIND_INPUT: AgentInteractionKind
AGENT_INPUT_TYPE_UNSPECIFIED: AgentInputType
AGENT_INPUT_TYPE_TEXT: AgentInputType
AGENT_INPUT_TYPE_CHOICE: AgentInputType
AGENT_INPUT_TYPE_JSON: AgentInputType
AGENT_APPROVAL_DECISION_UNSPECIFIED: AgentApprovalDecision
AGENT_APPROVAL_DECISION_APPROVE: AgentApprovalDecision
AGENT_APPROVAL_DECISION_DENY: AgentApprovalDecision
AGENT_ERROR_CATEGORY_UNSPECIFIED: AgentErrorCategory
AGENT_ERROR_CATEGORY_INVALID_ARGUMENT: AgentErrorCategory
AGENT_ERROR_CATEGORY_UNAUTHENTICATED: AgentErrorCategory
AGENT_ERROR_CATEGORY_PERMISSION_DENIED: AgentErrorCategory
AGENT_ERROR_CATEGORY_NOT_FOUND: AgentErrorCategory
AGENT_ERROR_CATEGORY_CONFLICT: AgentErrorCategory
AGENT_ERROR_CATEGORY_FAILED_PRECONDITION: AgentErrorCategory
AGENT_ERROR_CATEGORY_UNAVAILABLE: AgentErrorCategory
AGENT_ERROR_CATEGORY_DEADLINE_EXCEEDED: AgentErrorCategory

class AgentToolSelection(_message.Message):
    __slots__ = ()
    DISABLED_FIELD_NUMBER: _ClassVar[int]
    REFS_FIELD_NUMBER: _ClassVar[int]
    disabled: bool
    refs: _containers.RepeatedCompositeFieldContainer[_app_pb2.AgentToolRef]
    def __init__(self, disabled: _Optional[bool] = ..., refs: _Optional[_Iterable[_Union[_app_pb2.AgentToolRef, _Mapping]]] = ...) -> None: ...

class AgentSkillRef(_message.Message):
    __slots__ = ()
    MARKETPLACE_FIELD_NUMBER: _ClassVar[int]
    PACKAGE_FIELD_NUMBER: _ClassVar[int]
    SKILL_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    marketplace: str
    package: str
    skill: str
    version: str
    def __init__(self, marketplace: _Optional[str] = ..., package: _Optional[str] = ..., skill: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class AgentSkillSelection(_message.Message):
    __slots__ = ()
    REFS_FIELD_NUMBER: _ClassVar[int]
    refs: _containers.RepeatedCompositeFieldContainer[AgentSkillRef]
    def __init__(self, refs: _Optional[_Iterable[_Union[AgentSkillRef, _Mapping]]] = ...) -> None: ...

class AgentConfigInput(_message.Message):
    __slots__ = ()
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    SKILLS_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    model: str
    instructions: str
    tools: AgentToolSelection
    skills: AgentSkillSelection
    workspace: _agent_pb2.AgentWorkspace
    def __init__(self, provider_name: _Optional[str] = ..., model: _Optional[str] = ..., instructions: _Optional[str] = ..., tools: _Optional[_Union[AgentToolSelection, _Mapping]] = ..., skills: _Optional[_Union[AgentSkillSelection, _Mapping]] = ..., workspace: _Optional[_Union[_agent_pb2.AgentWorkspace, _Mapping]] = ...) -> None: ...

class AgentToolSelectionUpdate(_message.Message):
    __slots__ = ()
    MODE_FIELD_NUMBER: _ClassVar[int]
    REPLACE_FIELD_NUMBER: _ClassVar[int]
    ADD_FIELD_NUMBER: _ClassVar[int]
    REMOVE_FIELD_NUMBER: _ClassVar[int]
    mode: AgentConfigCollectionUpdateMode
    replace: AgentToolSelection
    add: _containers.RepeatedCompositeFieldContainer[_app_pb2.AgentToolRef]
    remove: _containers.RepeatedCompositeFieldContainer[_app_pb2.AgentToolRef]
    def __init__(self, mode: _Optional[_Union[AgentConfigCollectionUpdateMode, str]] = ..., replace: _Optional[_Union[AgentToolSelection, _Mapping]] = ..., add: _Optional[_Iterable[_Union[_app_pb2.AgentToolRef, _Mapping]]] = ..., remove: _Optional[_Iterable[_Union[_app_pb2.AgentToolRef, _Mapping]]] = ...) -> None: ...

class AgentSkillSelectionUpdate(_message.Message):
    __slots__ = ()
    MODE_FIELD_NUMBER: _ClassVar[int]
    REPLACE_FIELD_NUMBER: _ClassVar[int]
    ADD_FIELD_NUMBER: _ClassVar[int]
    REMOVE_FIELD_NUMBER: _ClassVar[int]
    mode: AgentConfigCollectionUpdateMode
    replace: AgentSkillSelection
    add: _containers.RepeatedCompositeFieldContainer[AgentSkillRef]
    remove: _containers.RepeatedCompositeFieldContainer[AgentSkillRef]
    def __init__(self, mode: _Optional[_Union[AgentConfigCollectionUpdateMode, str]] = ..., replace: _Optional[_Union[AgentSkillSelection, _Mapping]] = ..., add: _Optional[_Iterable[_Union[AgentSkillRef, _Mapping]]] = ..., remove: _Optional[_Iterable[_Union[AgentSkillRef, _Mapping]]] = ...) -> None: ...

class AgentConfigUpdateInput(_message.Message):
    __slots__ = ()
    MODEL_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    SKILLS_FIELD_NUMBER: _ClassVar[int]
    model: str
    instructions: str
    tools: AgentToolSelectionUpdate
    skills: AgentSkillSelectionUpdate
    def __init__(self, model: _Optional[str] = ..., instructions: _Optional[str] = ..., tools: _Optional[_Union[AgentToolSelectionUpdate, _Mapping]] = ..., skills: _Optional[_Union[AgentSkillSelectionUpdate, _Mapping]] = ...) -> None: ...

class AgentConfigRevision(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_REVISION_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    TOOL_REFS_FIELD_NUMBER: _ClassVar[int]
    SKILL_REFS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    parent_revision: str
    model: str
    instructions: str
    tool_refs: _containers.RepeatedCompositeFieldContainer[_app_pb2.AgentToolRef]
    skill_refs: _containers.RepeatedCompositeFieldContainer[AgentSkillRef]
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., parent_revision: _Optional[str] = ..., model: _Optional[str] = ..., instructions: _Optional[str] = ..., tool_refs: _Optional[_Iterable[_Union[_app_pb2.AgentToolRef, _Mapping]]] = ..., skill_refs: _Optional[_Iterable[_Union[AgentSkillRef, _Mapping]]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AgentResource(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    CONFIG_REVISION_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_RUN_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    provider_name: str
    state: _agent_pb2.AgentSessionState
    config_revision: str
    created_by_subject_id: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    last_run_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., provider_name: _Optional[str] = ..., state: _Optional[_Union[_agent_pb2.AgentSessionState, str]] = ..., config_revision: _Optional[str] = ..., created_by_subject_id: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_run_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class CreateAgentRequest(_message.Message):
    __slots__ = ()
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    config: AgentConfigInput
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, config: _Optional[_Union[AgentConfigInput, _Mapping]] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetAgentRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentsRequest(_message.Message):
    __slots__ = ()
    AGENT_IDS_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_ids: _containers.RepeatedScalarFieldContainer[str]
    state: _agent_pb2.AgentSessionState
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_ids: _Optional[_Iterable[str]] = ..., state: _Optional[_Union[_agent_pb2.AgentSessionState, str]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentsResponse(_message.Message):
    __slots__ = ()
    AGENTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    agents: _containers.RepeatedCompositeFieldContainer[AgentResource]
    next_page_token: str
    def __init__(self, agents: _Optional[_Iterable[_Union[AgentResource, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class ArchiveAgentRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class CreateAgentConfigRevisionRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_REVISION_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    UPDATE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    expected_revision: str
    idempotency_key: str
    update: AgentConfigUpdateInput
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., expected_revision: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., update: _Optional[_Union[AgentConfigUpdateInput, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentRunOutput(_message.Message):
    __slots__ = ()
    TEXT_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_FIELD_NUMBER: _ClassVar[int]
    text: str
    structured: _struct_pb2.Value
    def __init__(self, text: _Optional[str] = ..., structured: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class AgentRunResource(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    CONFIG_REVISION_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    STATUS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    agent_id: str
    provider_name: str
    config_revision: str
    status: _agent_pb2.AgentExecutionStatus
    status_message: str
    output: AgentRunOutput
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., agent_id: _Optional[str] = ..., provider_name: _Optional[str] = ..., config_revision: _Optional[str] = ..., status: _Optional[_Union[_agent_pb2.AgentExecutionStatus, str]] = ..., status_message: _Optional[str] = ..., output: _Optional[_Union[AgentRunOutput, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class CreateAgentRunRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    message: str
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., message: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class GetAgentRunRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentRunsRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentRunsResponse(_message.Message):
    __slots__ = ()
    RUNS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[AgentRunResource]
    next_page_token: str
    def __init__(self, runs: _Optional[_Iterable[_Union[AgentRunResource, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class CancelAgentRunRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    reason: str
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., reason: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentRunEventDisplay(_message.Message):
    __slots__ = ()
    TEXT_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    text: str
    label: str
    phase: str
    def __init__(self, text: _Optional[str] = ..., label: _Optional[str] = ..., phase: _Optional[str] = ...) -> None: ...

class AgentRunEvent(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_REF_FIELD_NUMBER: _ClassVar[int]
    id: str
    cursor: str
    sequence: int
    agent_id: str
    run_id: str
    type: AgentRunEventType
    occurred_at: _timestamp_pb2.Timestamp
    display: AgentRunEventDisplay
    payload_ref: str
    def __init__(self, id: _Optional[str] = ..., cursor: _Optional[str] = ..., sequence: _Optional[int] = ..., agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., type: _Optional[_Union[AgentRunEventType, str]] = ..., occurred_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., display: _Optional[_Union[AgentRunEventDisplay, _Mapping]] = ..., payload_ref: _Optional[str] = ...) -> None: ...

class ListAgentRunEventsRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    after_cursor: str
    page_size: int
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., after_cursor: _Optional[str] = ..., page_size: _Optional[int] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentRunEventsResponse(_message.Message):
    __slots__ = ()
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[AgentRunEvent]
    next_cursor: str
    def __init__(self, events: _Optional[_Iterable[_Union[AgentRunEvent, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class AgentApprovalInteraction(_message.Message):
    __slots__ = ()
    ACTION_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    ARGUMENTS_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    action: str
    description: str
    arguments_summary: _struct_pb2.Value
    def __init__(self, action: _Optional[str] = ..., description: _Optional[str] = ..., arguments_summary: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class AgentInputChoice(_message.Message):
    __slots__ = ()
    VALUE_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    value: str
    label: str
    def __init__(self, value: _Optional[str] = ..., label: _Optional[str] = ...) -> None: ...

class AgentInputDefinition(_message.Message):
    __slots__ = ()
    TYPE_FIELD_NUMBER: _ClassVar[int]
    MULTILINE_FIELD_NUMBER: _ClassVar[int]
    CHOICES_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_FIELD_NUMBER: _ClassVar[int]
    type: AgentInputType
    multiline: bool
    choices: _containers.RepeatedCompositeFieldContainer[AgentInputChoice]
    schema: _struct_pb2.Struct
    def __init__(self, type: _Optional[_Union[AgentInputType, str]] = ..., multiline: _Optional[bool] = ..., choices: _Optional[_Iterable[_Union[AgentInputChoice, _Mapping]]] = ..., schema: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class AgentInputInteraction(_message.Message):
    __slots__ = ()
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    prompt: str
    input: AgentInputDefinition
    def __init__(self, prompt: _Optional[str] = ..., input: _Optional[_Union[AgentInputDefinition, _Mapping]] = ...) -> None: ...

class AgentRunInteraction(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    APPROVAL_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    agent_id: str
    run_id: str
    kind: AgentInteractionKind
    state: _agent_pb2.AgentInteractionState
    title: str
    approval: AgentApprovalInteraction
    input: AgentInputInteraction
    created_at: _timestamp_pb2.Timestamp
    resolved_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., kind: _Optional[_Union[AgentInteractionKind, str]] = ..., state: _Optional[_Union[_agent_pb2.AgentInteractionState, str]] = ..., title: _Optional[str] = ..., approval: _Optional[_Union[AgentApprovalInteraction, _Mapping]] = ..., input: _Optional[_Union[AgentInputInteraction, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., resolved_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AgentApprovalResolution(_message.Message):
    __slots__ = ()
    DECISION_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    decision: AgentApprovalDecision
    reason: str
    def __init__(self, decision: _Optional[_Union[AgentApprovalDecision, str]] = ..., reason: _Optional[str] = ...) -> None: ...

class AgentInputResolution(_message.Message):
    __slots__ = ()
    VALUE_FIELD_NUMBER: _ClassVar[int]
    value: _struct_pb2.Value
    def __init__(self, value: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class AgentInteractionResolution(_message.Message):
    __slots__ = ()
    APPROVAL_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    approval: AgentApprovalResolution
    input: AgentInputResolution
    def __init__(self, approval: _Optional[_Union[AgentApprovalResolution, _Mapping]] = ..., input: _Optional[_Union[AgentInputResolution, _Mapping]] = ...) -> None: ...

class GetAgentRunInteractionRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    interaction_id: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentRunInteractionsRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    state: _agent_pb2.AgentInteractionState
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., state: _Optional[_Union[_agent_pb2.AgentInteractionState, str]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class ListAgentRunInteractionsResponse(_message.Message):
    __slots__ = ()
    INTERACTIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    interactions: _containers.RepeatedCompositeFieldContainer[AgentRunInteraction]
    next_page_token: str
    def __init__(self, interactions: _Optional[_Iterable[_Union[AgentRunInteraction, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class ResolveAgentRunInteractionRequest(_message.Message):
    __slots__ = ()
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    run_id: str
    interaction_id: str
    idempotency_key: str
    resolution: AgentInteractionResolution
    context: _app_pb2.RequestContext
    def __init__(self, agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., resolution: _Optional[_Union[AgentInteractionResolution, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentResolvedSkill(_message.Message):
    __slots__ = ()
    REF_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MATERIALIZATION_REF_FIELD_NUMBER: _ClassVar[int]
    ref: AgentSkillRef
    version: str
    digest: str
    materialization_ref: str
    def __init__(self, ref: _Optional[_Union[AgentSkillRef, _Mapping]] = ..., version: _Optional[str] = ..., digest: _Optional[str] = ..., materialization_ref: _Optional[str] = ...) -> None: ...

class AgentResolvedWorkspace(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    MATERIALIZATION_REF_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    id: str
    materialization_ref: str
    cwd: str
    def __init__(self, id: _Optional[str] = ..., materialization_ref: _Optional[str] = ..., cwd: _Optional[str] = ...) -> None: ...

class AgentHistoryPolicy(_message.Message):
    __slots__ = ()
    STRATEGY_FIELD_NUMBER: _ClassVar[int]
    MAX_CONTEXT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    strategy: str
    max_context_tokens: int
    def __init__(self, strategy: _Optional[str] = ..., max_context_tokens: _Optional[int] = ...) -> None: ...

class AgentResolvedConfigRevision(_message.Message):
    __slots__ = ()
    ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_REVISION_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_TOOLS_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_SKILLS_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    HISTORY_POLICY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    parent_revision: str
    model: str
    instructions: str
    resolved_tools: _containers.RepeatedCompositeFieldContainer[_agent_pb2.ListedAgentTool]
    resolved_skills: _containers.RepeatedCompositeFieldContainer[AgentResolvedSkill]
    resolved_workspace: AgentResolvedWorkspace
    history_policy: AgentHistoryPolicy
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., parent_revision: _Optional[str] = ..., model: _Optional[str] = ..., instructions: _Optional[str] = ..., resolved_tools: _Optional[_Iterable[_Union[_agent_pb2.ListedAgentTool, _Mapping]]] = ..., resolved_skills: _Optional[_Iterable[_Union[AgentResolvedSkill, _Mapping]]] = ..., resolved_workspace: _Optional[_Union[AgentResolvedWorkspace, _Mapping]] = ..., history_policy: _Optional[_Union[AgentHistoryPolicy, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AgentProviderCreateSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    INITIAL_CONFIG_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    idempotency_key: str
    initial_config: AgentResolvedConfigRevision
    created_by_subject_id: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., initial_config: _Optional[_Union[AgentResolvedConfigRevision, _Mapping]] = ..., created_by_subject_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderGetSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderListSessionsRequest(_message.Message):
    __slots__ = ()
    SESSION_IDS_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_ids: _containers.RepeatedScalarFieldContainer[str]
    state: _agent_pb2.AgentSessionState
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, session_ids: _Optional[_Iterable[str]] = ..., state: _Optional[_Union[_agent_pb2.AgentSessionState, str]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderListSessionsResponse(_message.Message):
    __slots__ = ()
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[AgentResource]
    next_page_token: str
    def __init__(self, sessions: _Optional[_Iterable[_Union[AgentResource, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class AgentProviderArchiveSessionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderCreateConfigRevisionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_REVISION_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    NEXT_CONFIG_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    expected_revision: str
    idempotency_key: str
    next_config: AgentResolvedConfigRevision
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., expected_revision: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., next_config: _Optional[_Union[AgentResolvedConfigRevision, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderCreateRunRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CONFIG_REVISION_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_REF_FIELD_NUMBER: _ClassVar[int]
    AUTHORITY_REF_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_SUBJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    idempotency_key: str
    message: str
    config_revision: str
    execution_ref: str
    authority_ref: str
    created_by_subject_id: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., message: _Optional[str] = ..., config_revision: _Optional[str] = ..., execution_ref: _Optional[str] = ..., authority_ref: _Optional[str] = ..., created_by_subject_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderGetRunRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderListRunsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderCancelRunRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    reason: str
    idempotency_key: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., reason: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderListRunEventsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    after_cursor: str
    page_size: int
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., after_cursor: _Optional[str] = ..., page_size: _Optional[int] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderGetInteractionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    interaction_id: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderListInteractionsRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    state: _agent_pb2.AgentInteractionState
    page_size: int
    page_token: str
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., state: _Optional[_Union[_agent_pb2.AgentInteractionState, str]] = ..., page_size: _Optional[int] = ..., page_token: _Optional[str] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderResolveInteractionRequest(_message.Message):
    __slots__ = ()
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    RESOLUTION_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    run_id: str
    interaction_id: str
    idempotency_key: str
    resolution: AgentInteractionResolution
    context: _app_pb2.RequestContext
    def __init__(self, session_id: _Optional[str] = ..., run_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ..., resolution: _Optional[_Union[AgentInteractionResolution, _Mapping]] = ..., context: _Optional[_Union[_app_pb2.RequestContext, _Mapping]] = ...) -> None: ...

class AgentProviderContractCapabilities(_message.Message):
    __slots__ = ()
    PROTOCOL_VERSION_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    SKILLS_FIELD_NUMBER: _ClassVar[int]
    INTERACTIONS_FIELD_NUMBER: _ClassVar[int]
    STRUCTURED_OUTPUT_FIELD_NUMBER: _ClassVar[int]
    WORKSPACES_FIELD_NUMBER: _ClassVar[int]
    PARALLEL_TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    REASONING_SUMMARIES_FIELD_NUMBER: _ClassVar[int]
    protocol_version: int
    tools: bool
    skills: bool
    interactions: bool
    structured_output: bool
    workspaces: bool
    parallel_tool_calls: bool
    reasoning_summaries: bool
    def __init__(self, protocol_version: _Optional[int] = ..., tools: _Optional[bool] = ..., skills: _Optional[bool] = ..., interactions: _Optional[bool] = ..., structured_output: _Optional[bool] = ..., workspaces: _Optional[bool] = ..., parallel_tool_calls: _Optional[bool] = ..., reasoning_summaries: _Optional[bool] = ...) -> None: ...

class GetAgentProviderContractCapabilitiesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AgentErrorDetail(_message.Message):
    __slots__ = ()
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    INTERACTION_ID_FIELD_NUMBER: _ClassVar[int]
    CURRENT_REVISION_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_CAPABILITY_FIELD_NUMBER: _ClassVar[int]
    category: AgentErrorCategory
    agent_id: str
    run_id: str
    interaction_id: str
    current_revision: str
    required_capability: str
    def __init__(self, category: _Optional[_Union[AgentErrorCategory, str]] = ..., agent_id: _Optional[str] = ..., run_id: _Optional[str] = ..., interaction_id: _Optional[str] = ..., current_revision: _Optional[str] = ..., required_capability: _Optional[str] = ...) -> None: ...
